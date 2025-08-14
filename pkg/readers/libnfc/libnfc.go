//go:build linux

package libnfc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc/tags"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/shared/ndef"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/rs/zerolog/log"
)

const (
	timeToForgetCard          = 500 * time.Millisecond
	connectMaxTries           = 10
	timesToPoll               = 1
	periodBetweenPolls        = 250 * time.Millisecond
	periodBetweenLoop         = 250 * time.Millisecond
	autoConnStr               = "libnfc_auto:"
	maxMifareClassic1KSectors = 16
	defaultWriteTimeoutTries  = 4 * 30 // ~30 seconds
)

var ErrWriteCancelled = errors.New("write operation was cancelled")

type TransportTimeoutError struct {
	Err    error
	Device string
}

func (e *TransportTimeoutError) Error() string {
	return fmt.Sprintf("transport timeout on device %s: %v", e.Device, e.Err)
}

func (e *TransportTimeoutError) Unwrap() error {
	return e.Err
}

func (*TransportTimeoutError) IsRetryable() bool {
	return true
}

type TagNotFoundError struct {
	Err    error
	Device string
}

func (e *TagNotFoundError) Error() string {
	return fmt.Sprintf("tag not found on device %s: %v", e.Device, e.Err)
}

func (e *TagNotFoundError) Unwrap() error {
	return e.Err
}

func (*TagNotFoundError) IsRetryable() bool {
	return true
}

type DataCorruptedError struct {
	Err    error
	Device string
}

func (e *DataCorruptedError) Error() string {
	return fmt.Sprintf("data corrupted on device %s: %v", e.Device, e.Err)
}

func (e *DataCorruptedError) Unwrap() error {
	return e.Err
}

func (*DataCorruptedError) IsRetryable() bool {
	return false
}

func IsRetryableError(err error) bool {
	type retryable interface {
		IsRetryable() bool
	}
	if r, ok := err.(retryable); ok {
		return r.IsRetryable()
	}
	// Default timeout errors as retryable
	return errors.Is(err, nfc.Error(nfc.ETIMEOUT))
}

func validateWriteParameters(r *Reader, text string) error {
	if r == nil {
		return errors.New("reader cannot be nil")
	}
	if !r.Connected() {
		return errors.New("reader not connected")
	}
	if text == "" {
		return errors.New("text cannot be empty")
	}
	return nil
}

type WriteRequestResult struct {
	Token     *tokens.Token
	Err       error
	Cancelled bool
}

type WriteRequest struct {
	Ctx    context.Context
	Result chan WriteRequestResult
	Cancel chan bool
	Text   string
}

type Reader struct {
	cfg           *config.Instance
	pnd           *nfc.Device
	prevToken     *tokens.Token
	write         chan WriteRequest
	activeWrite   *WriteRequest
	conn          config.ReadersConnect
	activeWriteMu sync.RWMutex
	polling       bool
	acr122Only    bool // ACR122-only mode flag
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:           cfg,
		write:         make(chan WriteRequest),
		activeWriteMu: sync.RWMutex{},
		acr122Only:    false, // Default behavior - detect all devices
	}
}

// NewACR122Reader creates a reader that only works with ACR122 USB devices
// and ignores PN532 UART/I2C devices. This is useful when a separate PN532
// library handles UART/I2C devices and we want to prevent conflicts.
func NewACR122Reader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:           cfg,
		write:         make(chan WriteRequest),
		activeWriteMu: sync.RWMutex{},
		acr122Only:    true, // Skip UART/I2C detection
	}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	connStr := device.ConnectionString()
	if connStr == autoConnStr {
		connStr = ""
	} else {
		log.Debug().Msgf("opening device: %s", connStr)
	}

	pnd, err := openDeviceWithRetries(connStr)
	if err != nil {
		if device.ConnectionString() == autoConnStr {
			return nil
		}

		return err
	}

	r.conn = device
	r.pnd = &pnd
	r.polling = true
	r.prevToken = nil

	go func() {
		for r.polling {
			select {
			case req := <-r.write:
				r.writeTag(req)
			case <-time.After(periodBetweenLoop):
				// continue with reading
			}

			token, removed, err := r.pollDevice(r.pnd, r.prevToken, timesToPoll, periodBetweenPolls)
			if errors.Is(err, nfc.Error(nfc.EIO)) {
				log.Error().Msgf("error during poll: %s", err)
				log.Error().Msg("fatal IO error, device was possibly unplugged")

				err = r.Close()
				if err != nil {
					log.Warn().Msgf("error closing device: %s", err)
				}

				continue
			} else if err != nil {
				log.Error().Msgf("error polling device: %s", err)
				continue
			}

			if removed {
				log.Info().Msg("token removed, sending to input queue")
				iq <- readers.Scan{
					Source: r.conn.ConnectionString(),
					Token:  nil,
				}
				r.prevToken = nil
			} else if token != nil {
				if r.prevToken != nil && token.UID == r.prevToken.UID {
					continue
				}

				log.Info().Msg("new token detected, sending to input queue")
				iq <- readers.Scan{
					Source: r.conn.ConnectionString(),
					Token:  token,
				}
				r.prevToken = token
			}
		}
	}()

	return nil
}

func (r *Reader) Close() error {
	r.polling = false

	if r.pnd == nil {
		return nil
	}
	log.Debug().Msgf("closing device: %s", r.conn)
	err := r.pnd.Close()
	if err != nil {
		return fmt.Errorf("failed to close NFC device: %w", err)
	}
	return nil
}

func (r *Reader) IDs() []string {
	// When ACR122-only mode, exclude PN532 device types
	if r.acr122Only {
		return []string{
			"acr122_usb",
		}
	}

	// Default behavior - all device types
	return []string{
		"pn532_uart",
		"pn532_i2c",
		"acr122_usb",
	}
}

func (r *Reader) Detect(connected []string) string {
	if !r.cfg.Readers().AutoDetect {
		return ""
	}

	// Skip serial detection when ACR122-only mode enabled
	if !r.acr122Only {
		device := detectSerialReaders(connected)
		if device != "" && !helpers.Contains(connected, device) {
			return device
		}
	}

	// Auto-detect for ACR122 and other USB/PCSC devices
	if !helpers.Contains(connected, autoConnStr) {
		return autoConnStr
	}

	return ""
}

func (r *Reader) Device() string {
	return r.conn.ConnectionString()
}

func (r *Reader) Connected() bool {
	return r.pnd != nil && r.pnd.Connection() != ""
}

func (r *Reader) Info() string {
	if !r.Connected() {
		return ""
	}

	return r.pnd.String()
}

func (r *Reader) Write(text string) (*tokens.Token, error) {
	return r.WriteWithContext(context.Background(), text)
}

func (r *Reader) WriteWithContext(ctx context.Context, text string) (*tokens.Token, error) {
	if err := validateWriteParameters(r, text); err != nil {
		return nil, fmt.Errorf("invalid write parameters: %w", err)
	}

	r.activeWriteMu.RLock()
	if r.activeWrite != nil {
		r.activeWriteMu.RUnlock()
		return nil, errors.New("write already in progress")
	}
	r.activeWriteMu.RUnlock()

	req := WriteRequest{
		Text:   text,
		Result: make(chan WriteRequestResult),
		Cancel: make(chan bool),
		Ctx:    ctx,
	}

	r.write <- req

	res := <-req.Result
	if res.Cancelled {
		return nil, ErrWriteCancelled
	} else if res.Err != nil {
		log.Error().Msgf("error writing to tag: %s", res.Err)
		return nil, res.Err
	}

	return res.Token, nil
}

func (r *Reader) CancelWrite() {
	r.activeWriteMu.RLock()
	defer r.activeWriteMu.RUnlock()
	if r.activeWrite != nil {
		r.activeWrite.Cancel <- true
	}
}

func (r *Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityWrite}
}

func (r *Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

// keep track of serial devices that had failed opens
var (
	serialCacheMu   = &sync.RWMutex{}
	serialBlockList []string
)

func GetSerialBlockListCount() int {
	serialCacheMu.RLock()
	defer serialCacheMu.RUnlock()
	return len(serialBlockList)
}

func detectSerialReaders(connected []string) string {
	devices, err := helpers.GetSerialDeviceList()
	if err != nil {
		log.Error().Msgf("error getting serial devices: %s", err)
		return ""
	}

	for _, device := range devices {
		// the libnfc open is extremely disruptive to other devices, we want
		// to minimise the number of times we try to open a device
		connStr := "pn532_uart:" + device

		// ignore if device is in block list
		serialCacheMu.RLock()
		if helpers.Contains(serialBlockList, device) {
			serialCacheMu.RUnlock()
			continue
		}
		serialCacheMu.RUnlock()

		// ignore if exact same device and reader are connected
		if helpers.Contains(connected, connStr) {
			continue
		}

		// resolve device symlink if necessary
		realPath := ""
		symPath, err := os.Readlink(device)
		if err == nil {
			parent := filepath.Dir(device)
			abs, absErr := filepath.Abs(filepath.Join(parent, symPath))
			if absErr == nil {
				realPath = abs
			}
		}

		// ignore if same resolved device and reader connected
		if realPath != "" && helpers.Contains(connected, realPath) {
			continue
		}

		// ignore if different reader already connected
		match := false
		for _, c := range connected {
			if strings.HasSuffix(c, ":"+device) {
				match = true
				break
			}
		}
		if match {
			continue
		}

		// ignore if different resolved device and reader connected
		if realPath != "" && strings.HasSuffix(realPath, ":"+device) {
			continue
		}

		pnd, err := nfc.Open(connStr)
		if err != nil {
			serialCacheMu.Lock()
			serialBlockList = append(serialBlockList, device)
			serialCacheMu.Unlock()
			continue
		}
		err = pnd.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing device: %s", device)
		}
		return connStr
	}

	return ""
}

func openDeviceWithRetries(device string) (nfc.Device, error) {
	tries := 0
	for {
		pnd, err := nfc.Open(device)
		if err == nil {
			log.Info().Msgf("successful connect, after %d tries", tries)

			connProto := strings.SplitN(strings.ToLower(device), ":", 2)[0]
			log.Info().Msgf("connection protocol: %s", connProto)
			deviceName := pnd.String()
			log.Info().Msgf("device name: %s", deviceName)

			if initErr := pnd.InitiatorInit(); initErr != nil {
				log.Error().Msgf("could not init initiator: %s", initErr)
				continue
			}

			return pnd, nil
		}

		if tries >= connectMaxTries {
			connProto := "unknown"
			if device != "" {
				connProto = strings.SplitN(strings.ToLower(device), ":", 2)[0]
			}
			return pnd, fmt.Errorf(
				"failed to open NFC device '%s' (protocol: %s) after %d tries: %w",
				device, connProto, connectMaxTries, err,
			)
		}

		tries++

		// Exponential backoff: 50ms, 200ms, 450ms, etc.
		backoffMs := 50 * tries * tries
		if backoffMs > 1000 { // Cap at 1 second
			backoffMs = 1000
		}
		log.Trace().Msgf("retry %d/%d after %dms backoff", tries, connectMaxTries, backoffMs)
		time.Sleep(time.Duration(backoffMs) * time.Millisecond)
	}
}

func (r *Reader) pollDevice(
	pnd *nfc.Device,
	activeToken *tokens.Token,
	ttp int,
	pbp time.Duration,
) (*tokens.Token, bool, error) {
	removed := false

	count, target, err := pnd.InitiatorPollTarget(tags.SupportedCardTypes, ttp, pbp)
	if err != nil && !errors.Is(err, nfc.Error(nfc.ETIMEOUT)) {
		deviceInfo := "unknown"
		if pnd != nil {
			deviceInfo = pnd.String()
		}
		return nil, false, fmt.Errorf("failed to poll NFC target on device '%s': %w", deviceInfo, err)
	}

	if count > 1 {
		log.Info().Msg("more than one card on the reader")
	}

	if count <= 0 {
		if activeToken != nil && time.Since(activeToken.ScanTime) > timeToForgetCard {
			log.Info().Msg("card removed")
			activeToken = nil
			removed = true
		}

		return activeToken, removed, nil
	}

	tagUID := tags.GetTagUID(target)
	if tagUID == "" {
		log.Warn().Msgf("unable to detect token ID: %s", target.String())
	}

	// no change in tag
	if activeToken != nil && tagUID == activeToken.UID {
		return activeToken, removed, nil
	}

	log.Info().Msgf("found token ID: %s", tagUID)

	var record tags.TagData
	cardType := tags.GetTagType(target)

	switch cardType {
	case tokens.TypeNTAG:
		log.Info().Msg("NTAG detected")
		record, err = tags.ReadNtag(*pnd)
		if err != nil {
			return activeToken, removed, fmt.Errorf("error reading ntag: %w", err)
		}
	case tokens.TypeMifare:
		log.Info().Msg("MIFARE detected")
		record, err = tags.ReadMifare(*pnd, tagUID)
		if err != nil {
			return activeToken, removed, fmt.Errorf("error reading mifare: %w", err)
		}
	}

	log.Debug().Msgf("record bytes: %s", hex.EncodeToString(record.Bytes))
	tagText, err := ndef.ParseToText(record.Bytes)
	if err != nil {
		log.Error().Err(err).Msgf("error parsing NDEF record")
		tagText = ""
	}

	if tagText == "" {
		log.Warn().Msg("no text NDEF found")
	} else {
		log.Info().Msgf("decoded text NDEF: %s", tagText)
	}

	card := &tokens.Token{
		Type:     record.Type,
		UID:      tagUID,
		Text:     tagText,
		Data:     hex.EncodeToString(record.Bytes),
		ScanTime: time.Now(),
		Source:   r.conn.ConnectionString(),
	}

	return card, removed, nil
}

func (r *Reader) writeTag(req WriteRequest) {
	log.Info().Msgf("libnfc write request: %s", req.Text)

	r.activeWriteMu.Lock()
	if r.activeWrite != nil {
		log.Error().Msgf("write already in progress")
		req.Result <- WriteRequestResult{
			Err: errors.New("write already in progress"),
		}
		r.activeWriteMu.Unlock()
		return
	}
	r.activeWrite = &req
	r.activeWriteMu.Unlock()
	defer func() {
		r.activeWriteMu.Lock()
		r.activeWrite = nil
		r.activeWriteMu.Unlock()
	}()

	var count int
	var target nfc.Target
	var err error
	tries := defaultWriteTimeoutTries

	for tries > 0 {
		select {
		case <-req.Cancel:
			log.Info().Msgf("write cancelled by user")
			req.Result <- WriteRequestResult{
				Cancelled: true,
			}
			return
		case <-req.Ctx.Done():
			log.Info().Msgf("write cancelled by context: %v", req.Ctx.Err())
			req.Result <- WriteRequestResult{
				Err: fmt.Errorf("write cancelled by context: %w", req.Ctx.Err()),
			}
			return
		case <-time.After(periodBetweenLoop):
			// continue with reading
		}

		count, target, err = r.pnd.InitiatorPollTarget(
			tags.SupportedCardTypes,
			timesToPoll,
			periodBetweenPolls,
		)

		if err != nil && !errors.Is(err, nfc.Error(nfc.ETIMEOUT)) {
			log.Error().Msgf("could not poll: %s", err)
		}

		if count > 0 {
			break
		}

		tries--
	}

	if count == 0 {
		log.Error().Msgf("could not detect a tag")
		req.Result <- WriteRequestResult{
			Err: errors.New("could not detect a tag"),
		}
		return
	}

	cardUID := tags.GetTagUID(target)
	log.Info().Msgf("found tag with ID: %s", cardUID)

	cardType := tags.GetTagType(target)
	var bytesWritten []byte

	switch cardType {
	case tokens.TypeMifare:
		bytesWritten, err = tags.WriteMifare(*r.pnd, req.Text, cardUID)
		if err != nil {
			log.Error().Msgf("error writing to mifare: %s", err)
			req.Result <- WriteRequestResult{
				Err: err,
			}
			return
		}
	case tokens.TypeNTAG:
		bytesWritten, err = tags.WriteNtag(*r.pnd, req.Text)
		if err != nil {
			log.Error().Msgf("error writing to ntag: %s", err)
			req.Result <- WriteRequestResult{
				Err: err,
			}
			return
		}
	default:
		log.Error().Msgf("unsupported tag type: %s", cardType)
		req.Result <- WriteRequestResult{
			Err: fmt.Errorf("unsupported tag type: %s", cardType),
		}
		return
	}

	verificationTries := 3
	var t *tokens.Token
	for i := 0; i < verificationTries; i++ {
		var verifyErr error
		t, _, verifyErr = r.pollDevice(r.pnd, nil, timesToPoll, periodBetweenPolls)
		if verifyErr == nil && t != nil {
			break
		}
		if i >= verificationTries-1 {
			log.Error().Msgf("write verification failed after %d attempts: %v", verificationTries, verifyErr)
			req.Result <- WriteRequestResult{
				Err: &DataCorruptedError{
					Device: r.conn.ConnectionString(),
					Err:    fmt.Errorf("write verification failed: %w", verifyErr),
				},
			}
			return
		}
		log.Warn().Msgf("write verification attempt %d failed, retrying: %v", i+1, verifyErr)
		time.Sleep(50 * time.Millisecond)
	}

	if t.UID != cardUID {
		log.Error().Msgf("ID mismatch after write: %s != %s", t.UID, cardUID)
		req.Result <- WriteRequestResult{
			Err: &DataCorruptedError{
				Device: r.conn.ConnectionString(),
				Err:    fmt.Errorf("ID mismatch after write: expected %s, got %s", cardUID, t.UID),
			},
		}
		return
	}

	if t.Text != req.Text {
		log.Error().Msgf("text mismatch after write: %s != %s", t.Text, req.Text)
		req.Result <- WriteRequestResult{
			Err: &DataCorruptedError{
				Device: r.conn.ConnectionString(),
				Err:    fmt.Errorf("text mismatch after write: expected %s, got %s", req.Text, t.Text),
			},
		}
		return
	}

	log.Info().Msgf("successfully wrote to card: %s", hex.EncodeToString(bytesWritten))
	r.prevToken = t
	req.Result <- WriteRequestResult{
		Token: t,
	}
}
