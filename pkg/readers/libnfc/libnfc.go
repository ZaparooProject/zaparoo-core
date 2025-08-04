//go:build linux

package libnfc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc/tags"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/rs/zerolog/log"
)

const (
	timeToForgetCard   = 500 * time.Millisecond
	connectMaxTries    = 10
	timesToPoll        = 1
	periodBetweenPolls = 250 * time.Millisecond
	periodBetweenLoop  = 250 * time.Millisecond
	autoConnStr        = "libnfc_auto:"
)

var ErrWriteCancelled = errors.New("write operation was cancelled")

type WriteRequestResult struct {
	Token     *tokens.Token
	Err       error
	Cancelled bool
}

type WriteRequest struct {
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
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:           cfg,
		write:         make(chan WriteRequest),
		activeWriteMu: sync.RWMutex{},
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
	return r.pnd.Close()
}

func (*Reader) IDs() []string {
	return []string{
		"pn532_uart",
		"pn532_i2c",
		"acr122_usb",
		"pcsc",
	}
}

func (r *Reader) Detect(connected []string) string {
	if !r.cfg.Readers().AutoDetect {
		return ""
	}

	device := detectSerialReaders(connected)
	if device != "" && !helpers.Contains(connected, device) {
		return device
	}

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
	if !r.Connected() {
		return nil, errors.New("not connected")
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

// keep track of serial devices that had failed opens
var (
	serialCacheMu   = &sync.RWMutex{}
	serialBlockList []string
)

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
			abs, err := filepath.Abs(filepath.Join(parent, symPath))
			if err == nil {
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
		if realPath != "" && strings.HasSuffix(realPath, ":"+realPath) {
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

			if err := pnd.InitiatorInit(); err != nil {
				log.Error().Msgf("could not init initiator: %s", err)
				continue
			}

			return pnd, err
		}

		if tries >= connectMaxTries {
			return pnd, err
		}

		tries++
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
		return nil, false, err
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
			log.Error().Msgf("error reading mifare: %s", err)
		}
	}

	log.Debug().Msgf("record bytes: %s", hex.EncodeToString(record.Bytes))
	tagText, err := tags.ParseRecordText(record.Bytes)
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
	tries := 4 * 30 // ~30 seconds

	for tries > 0 {
		select {
		case <-req.Cancel:
			log.Info().Msgf("write cancelled by user")
			req.Result <- WriteRequestResult{
				Cancelled: true,
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

		if err != nil && err.Error() != "timeout" {
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
			Err: err,
		}
		return
	}

	t, _, err := r.pollDevice(r.pnd, nil, timesToPoll, periodBetweenPolls)
	if err != nil || t == nil {
		log.Error().Msgf("error reading written tag: %s", err)
		req.Result <- WriteRequestResult{
			Err: err,
		}
		return
	}

	if t.UID != cardUID {
		log.Error().Msgf("ID mismatch after write: %s != %s", t.UID, cardUID)
		req.Result <- WriteRequestResult{
			Err: errors.New("ID mismatch after write"),
		}
		return
	}

	if t.Text != req.Text {
		log.Error().Msgf("text mismatch after write: %s != %s", t.Text, req.Text)
		req.Result <- WriteRequestResult{
			Err: errors.New("text mismatch after write"),
		}
		return
	}

	log.Info().Msgf("successfully wrote to card: %s", hex.EncodeToString(bytesWritten))
	req.Result <- WriteRequestResult{
		Token: t,
	}
}
