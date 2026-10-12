//go:build linux && !android

package libnfc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/clausecker/nfc/v2"
	"github.com/rs/zerolog/log"
)

const (
	// removalMisses is how many polls in a row must find no tag before it is
	// reported removed, so one dropped poll is not a removal.
	removalMisses = 2
	// readAttempts is how many polls may fail to read a tag before it is
	// reported by UID alone. A tag still being placed often fails the first.
	readAttempts              = 3
	connectMaxTries           = 10
	timesToPoll               = 1
	periodBetweenPolls        = 250 * time.Millisecond
	periodBetweenLoop         = 250 * time.Millisecond
	autoConnStr               = "libnfcauto:"
	maxMifareClassic1KSectors = 16
	defaultWriteTimeoutTries  = 4 * 30 // ~30 seconds
)

// ErrWriteCancelled wraps context.Canceled, so a cancel the user asked for is
// not logged as a write failure.
var ErrWriteCancelled = fmt.Errorf("write operation was cancelled: %w", context.Canceled)

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
	Ctx     context.Context
	Result  chan WriteRequestResult
	Cancel  chan bool
	Text    string
	Options readers.WriteOptions
}

type readerMode int

const (
	modeAll readerMode = iota
	modeACR122Only
	modeLegacyUART
	modeLegacyI2C
)

// device is the part of a libnfc device the reader uses.
type device interface {
	tags.Transceiver
	InitiatorPollTarget(modulations []nfc.Modulation, times int, period time.Duration) (int, nfc.Target, error)
	String() string
	Connection() string
	Close() error
}

// readRetries counts failed reads of the tag in the field. A tag still being
// placed often fails its first read, so it is only reported by UID alone once
// it has failed readAttempts times in a row.
type readRetries struct {
	uid      string
	failures int
}

// failed records a failed read of the tag with uid and reports whether it has
// now failed often enough to stop retrying.
func (r *readRetries) failed(uid string) bool {
	if r.uid != uid {
		r.uid = uid
		r.failures = 0
	}
	r.failures++
	return r.failures >= readAttempts
}

// reset forgets the count: the tag was read, was given up on, or has left.
func (r *readRetries) reset() {
	r.uid = ""
	r.failures = 0
}

type Reader struct {
	cfg           *config.Instance
	pnd           device
	prevToken     *tokens.Token
	write         chan WriteRequest
	activeWrite   *WriteRequest
	conn          config.ReadersConnect
	reads         readRetries
	activeWriteMu syncutil.RWMutex
	mu            syncutil.RWMutex // protects polling, pnd
	// misses counts polls in a row that found no tag.
	misses  int
	polling bool
	// writtenTag is set while the tag last written is still on the reader.
	writtenTag bool
	mode       readerMode // Reader mode for different device types
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:   cfg,
		write: make(chan WriteRequest),
		mode:  modeAll,
	}
}

// NewACR122Reader creates a reader that only works with ACR122 USB devices
// and ignores PN532 UART/I2C devices. This is useful when a separate PN532
// library handles UART/I2C devices and we want to prevent conflicts.
func NewACR122Reader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:   cfg,
		write: make(chan WriteRequest),
		mode:  modeACR122Only,
	}
}

// NewLegacyUARTReader creates a reader for legacy PN532 UART devices.
// This provides a fallback option for users experiencing issues with the
// new go-pn532 driver. Auto-detect is disabled by default.
func NewLegacyUARTReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:   cfg,
		write: make(chan WriteRequest),
		mode:  modeLegacyUART,
	}
}

// NewLegacyI2CReader creates a reader for legacy PN532 I2C devices.
// This provides a fallback option for users experiencing issues with the
// new go-pn532 driver. Auto-detect is disabled by default.
func NewLegacyI2CReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:   cfg,
		write: make(chan WriteRequest),
		mode:  modeLegacyI2C,
	}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan, _ readers.OpenOpts) error {
	connStr := device.ConnectionString()
	if connStr == autoConnStr {
		connStr = ""
	} else {
		log.Debug().Msgf("opening device: %s", connStr)
	}

	connStr = toLibnfcConnStr(r.mode, connStr)

	pnd, err := openDeviceWithRetries(connStr)
	if err != nil {
		if device.ConnectionString() == autoConnStr {
			return nil
		}

		return err
	}

	r.mu.Lock()
	r.conn = device
	r.pnd = pnd
	r.polling = true
	r.prevToken = nil
	r.mu.Unlock()

	go func() {
		for {
			r.mu.RLock()
			polling := r.polling
			pnd := r.pnd
			r.mu.RUnlock()
			if !polling || pnd == nil {
				break
			}
			select {
			case req := <-r.write:
				r.writeTag(&req)
			case <-time.After(periodBetweenLoop):
				// continue with reading
			}

			token, removed, err := r.pollDevice(pnd, r.prevToken, timesToPoll, periodBetweenPolls)
			if errors.Is(err, nfc.Error(nfc.EIO)) {
				log.Warn().Msgf("error during poll: %s", err)
				log.Warn().Msg("fatal IO error, device was possibly unplugged")

				log.Warn().Msg("reader error, sending error signal")
				iq <- readers.Scan{
					Source:      tokens.SourceReader,
					ReaderID:    r.ReaderID(),
					Token:       nil,
					ReaderError: true,
				}
				r.prevToken = nil

				err = r.Close()
				if err != nil {
					log.Warn().Msgf("error closing device: %s", err)
				}

				break
			} else if err != nil {
				log.Warn().Msgf("error polling device: %s", err)
				continue
			}

			if removed {
				log.Info().Msg("token removed, sending to input queue")
				iq <- readers.Scan{
					Source:            tokens.SourceReader,
					ReaderID:          r.ReaderID(),
					Token:             nil,
					WrittenTagRemoved: r.writtenTag,
				}
				r.prevToken = nil
				r.writtenTag = false
			} else if token != nil {
				if r.prevToken != nil && token.UID == r.prevToken.UID {
					continue
				}
				// A different tag replaced the written one without a gap.
				r.writtenTag = false

				log.Info().Msg("new token detected, sending to input queue")
				iq <- readers.Scan{
					Source:   tokens.SourceReader,
					ReaderID: r.ReaderID(),
					Token:    token,
				}
				r.prevToken = token
			}
		}
	}()

	return nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	r.polling = false
	if r.pnd == nil {
		r.mu.Unlock()
		return nil
	}
	pnd := r.pnd
	r.pnd = nil
	r.mu.Unlock()

	log.Debug().Msgf("closing device: %s", r.conn.ConnectionString())
	err := pnd.Close()
	if err != nil {
		return fmt.Errorf("failed to close NFC device: %w", err)
	}
	return nil
}

func (r *Reader) Metadata() readers.DriverMetadata {
	switch r.mode {
	case modeACR122Only:
		return readers.DriverMetadata{
			ID:             "libnfcacr122",
			DefaultEnabled: true,
			// Auto-detect is off by default because Detect calls
			// nfc.ListDevices, which enumerates the USB bus through libusb.
			// On the reader manager's 1 Hz tick that ran once a second on every
			// device, whether or not an ACR122 was attached, and enumerating
			// while the bus changes under it killed the service outright: on a
			// MiSTer, unplugging a USB reader produced
			//
			//   panic: member access within misaligned address 0x327ffc4 for
			//   type 'struct libusb_context', which requires 8 byte alignment
			//
			// which is a hard abort from the cgo side, with no Go panic and
			// nothing in the log. rs232barcode and mqtt already ship
			// enabled-but-not-auto-detected for the milder version of this
			// trade-off; an ACR122 owner opts in with a readers.connect entry
			// or readers.drivers.libnfcacr122.auto_detect = true.
			DefaultAutoDetect: false,
			Description:       "LibNFC ACR122 USB NFC reader",
		}
	case modeLegacyUART:
		return readers.DriverMetadata{
			ID:                "legacypn532uart",
			DefaultEnabled:    true,
			DefaultAutoDetect: false,
			Description:       "Legacy PN532 UART reader via LibNFC",
		}
	case modeLegacyI2C:
		return readers.DriverMetadata{
			ID:                "legacypn532i2c",
			DefaultEnabled:    true,
			DefaultAutoDetect: false,
			Description:       "Legacy PN532 I2C reader via LibNFC",
		}
	default:
		return readers.DriverMetadata{
			ID:                "libnfc",
			DefaultEnabled:    true,
			DefaultAutoDetect: true,
			Description:       "LibNFC NFC reader (PN532/ACR122)",
		}
	}
}

func (r *Reader) IDs() []string {
	switch r.mode {
	case modeACR122Only:
		return []string{
			"acr122usb",
			"acr122_usb",
		}
	case modeLegacyUART:
		return []string{
			"legacypn532uart",
			"legacy_pn532_uart",
		}
	case modeLegacyI2C:
		return []string{
			"legacypn532i2c",
			"legacy_pn532_i2c",
		}
	default:
		// Default behavior - all device types
		return []string{
			"pn532uart",
			"pn532_uart",
			"pn532i2c",
			"pn532_i2c",
			"acr122usb",
			"acr122_usb",
		}
	}
}

func (r *Reader) Detect(connected []string) string {
	metadata := r.Metadata()
	driver := config.DriverInfo{
		ID:                metadata.ID,
		DefaultEnabled:    metadata.DefaultEnabled,
		DefaultAutoDetect: metadata.DefaultAutoDetect,
	}
	if !r.cfg.IsReaderEnabled(driver, config.ReaderEnableContextAutoDetect) {
		return ""
	}

	switch r.mode {
	case modeACR122Only:
		return detectACR122Readers(connected)
	case modeLegacyUART:
		// Only detect UART devices, return with legacy prefix
		device := detectSerialReaders(connected)
		if device != "" && !helpers.Contains(connected, device) {
			return strings.Replace(device, "pn532_uart:", "legacypn532uart:", 1)
		}
	case modeLegacyI2C:
		// Only detect I2C devices, return with legacy prefix
		device := detectI2CReaders(connected)
		if device != "" && !helpers.Contains(connected, device) {
			return strings.Replace(device, "pn532_i2c:", "legacypn532i2c:", 1)
		}
	default:
		// Default mode - detect UART/I2C serial devices
		device := detectSerialReaders(connected)
		if device != "" && !helpers.Contains(connected, device) {
			return device
		}

		return detectACR122Readers(connected)
	}

	return ""
}

func (r *Reader) Path() string {
	return r.conn.Path
}

func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.polling && r.pnd != nil
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.polling || r.pnd == nil {
		return ""
	}
	return r.pnd.String()
}

func (r *Reader) Write(text string) (*tokens.Token, error) {
	return r.WriteTarget(context.Background(), text, readers.WriteOptions{})
}

func (r *Reader) WriteWithContext(ctx context.Context, text string) (*tokens.Token, error) {
	return r.WriteTarget(ctx, text, readers.WriteOptions{})
}

func (r *Reader) WriteTarget(ctx context.Context, text string, opts readers.WriteOptions) (*tokens.Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}

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
		// Buffered so a cancel never blocks: once the tag is being written
		// nothing is listening, and the write is left to finish.
		Cancel:  make(chan bool, 1),
		Ctx:     ctx,
		Options: opts,
	}

	r.write <- req

	res := <-req.Result
	if res.Cancelled {
		return nil, ErrWriteCancelled
	} else if res.Err != nil {
		// Expected user conditions (no tag presented, unsupported tag) log at
		// Warn; genuine hardware write failures stay at Error for Sentry.
		if errors.Is(res.Err, readers.ErrTagNotDetected) || errors.Is(res.Err, readers.ErrUnsupportedTagType) {
			log.Warn().Msgf("error writing to tag: %s", res.Err)
		} else {
			log.Error().Msgf("error writing to tag: %s", res.Err)
		}
		return nil, res.Err
	}

	return res.Token, nil
}

func (r *Reader) CancelWrite() {
	r.activeWriteMu.RLock()
	defer r.activeWriteMu.RUnlock()
	if r.activeWrite != nil {
		select {
		case r.activeWrite.Cancel <- true:
		default:
		}
	}
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityWrite, readers.CapabilityRemovable}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

func (r *Reader) ReaderID() string {
	r.mu.RLock()
	connStr := r.conn.ConnectionString()
	if connStr == "" || connStr == autoConnStr {
		if r.pnd != nil && r.pnd.Connection() != "" {
			connStr = r.pnd.Connection()
		}
	}
	r.mu.RUnlock()
	return readers.GenerateReaderID(r.Metadata().ID, connStr)
}

// keep track of serial devices that had failed opens
var (
	serialCacheMu   = &syncutil.RWMutex{}
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
		log.Warn().Msgf("error getting serial devices: %s", err)
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

// keep track of I2C devices that had failed opens
var (
	i2cCacheMu   = &syncutil.RWMutex{}
	i2cBlockList []string
)

func detectI2CReaders(connected []string) string {
	// Look for I2C devices on Linux (typically /dev/i2c-*)
	i2cDevices := []string{
		"/dev/i2c-0",
		"/dev/i2c-1",
		"/dev/i2c-2",
		"/dev/i2c-3",
	}

	for _, device := range i2cDevices {
		// Check if device exists
		if _, err := os.Stat(device); os.IsNotExist(err) {
			continue
		}

		connStr := "pn532_i2c:" + device

		// ignore if device is in block list
		i2cCacheMu.RLock()
		if helpers.Contains(i2cBlockList, device) {
			i2cCacheMu.RUnlock()
			continue
		}
		i2cCacheMu.RUnlock()

		// ignore if exact same device and reader are connected
		if helpers.Contains(connected, connStr) {
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

		// Try to open the device to see if it's a valid PN532
		pnd, err := nfc.Open(connStr)
		if err != nil {
			i2cCacheMu.Lock()
			i2cBlockList = append(i2cBlockList, device)
			i2cCacheMu.Unlock()
			continue
		}
		err = pnd.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing I2C device: %s", device)
		}
		return connStr
	}

	return ""
}

func detectACR122Readers(connected []string) string {
	devices, err := nfc.ListDevices()
	if err != nil {
		log.Trace().Err(err).Msg("error listing libnfc devices")
		return fallbackAutoACR122(connected)
	}

	return detectACR122Device(connected, devices)
}

func detectACR122Device(connected, devices []string) string {
	foundACR122 := false
	for _, device := range devices {
		connStr := strings.TrimRight(device, "\x00")
		if !isACR122ConnStr(connStr) {
			continue
		}
		foundACR122 = true

		path := connectionPath(connStr)
		if path == "" || isConnectedPath(connected, path) {
			continue
		}

		log.Trace().Msgf("acr122 libnfc reader found: %s", connStr)
		return connStr
	}

	if foundACR122 {
		return ""
	}

	return fallbackAutoACR122(connected)
}

func fallbackAutoACR122(connected []string) string {
	if helpers.Contains(connected, autoConnStr) || isConnectedPath(connected, "") {
		return ""
	}
	return autoConnStr
}

func isACR122ConnStr(connStr string) bool {
	return strings.HasPrefix(connStr, "acr122_usb:") || strings.HasPrefix(connStr, "acr122_pcsc:")
}

func connectionPath(connStr string) string {
	parts := strings.SplitN(connStr, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func isConnectedPath(connected []string, path string) bool {
	for _, connStr := range connected {
		if connectionPath(connStr) == path {
			return true
		}
	}
	return false
}

// toLibnfcConnStr translates internal normalized connection strings to the
// format expected by libnfc. libnfc driver names use underscores
// (e.g. "pn532_i2c", "pn532_uart") but ConnectionString() strips them for
// internal driver matching.
func toLibnfcConnStr(mode readerMode, connStr string) string {
	switch mode {
	case modeLegacyUART:
		return strings.Replace(connStr, "legacypn532uart:", "pn532_uart:", 1)
	case modeLegacyI2C:
		return strings.Replace(connStr, "legacypn532i2c:", "pn532_i2c:", 1)
	default:
		connStr = strings.Replace(connStr, "pn532uart:", "pn532_uart:", 1)
		connStr = strings.Replace(connStr, "pn532i2c:", "pn532_i2c:", 1)
		connStr = strings.Replace(connStr, "acr122usb:", "acr122_usb:", 1)
		connStr = strings.Replace(connStr, "acr122pcsc:", "acr122_pcsc:", 1)
		return connStr
	}
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
				log.Warn().Msgf("could not init initiator: %s", initErr)
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
	pnd device,
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
		r.misses++
		// A tag that left takes its failed reads with it: the same tag coming
		// back gets the full number of attempts again.
		r.reads.reset()
		if activeToken != nil && r.misses >= removalMisses {
			log.Info().Msg("card removed")
			activeToken = nil
			removed = true
		}

		return activeToken, removed, nil
	}
	r.misses = 0

	uid := tags.UID(target)
	tagUID := hex.EncodeToString(uid)
	if tagUID == "" {
		log.Warn().Msgf("unable to detect token ID: %s", target.String())
	}

	// no change in tag
	if activeToken != nil && tagUID == activeToken.UID {
		return activeToken, removed, nil
	}

	log.Info().Msgf("found token ID: %s", tagUID)

	res, err := nfctag.Read(context.Background(), &nfctag.Tag{
		IO:   tags.NewIO(pnd, uid),
		UID:  uid,
		Kind: tags.Kind(target),
	})
	if err != nil {
		if !r.reads.failed(tagUID) {
			return activeToken, removed, fmt.Errorf("error reading %s tag: %w", res.Type, err)
		}
		// A tag that cannot be read is still a tag: it is reported by UID so
		// it can be used through a mapping.
		log.Warn().Err(err).Msgf("could not read %s tag, reporting it by UID", res.Type)
	}
	r.reads.reset()

	log.Info().Msgf("%s tag detected", res.Type)
	if res.Text == "" {
		log.Debug().Msg("no text NDEF found")
	} else if e := log.Debug(); e.Enabled() {
		e.Msgf("decoded text NDEF: %s", zapscript.ForLog(res.Text))
	}

	card := &tokens.Token{
		Type:     res.Type,
		UID:      tagUID,
		Text:     res.Text,
		Data:     hex.EncodeToString(res.Data),
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
	}

	return card, removed, nil
}

func (r *Reader) writeTag(req *WriteRequest) {
	log.Info().Msg("libnfc write request received")
	if e := log.Debug(); e.Enabled() {
		e.Msgf("libnfc write text: %s", zapscript.ForLog(req.Text))
	}

	r.mu.RLock()
	pnd := r.pnd
	r.mu.RUnlock()
	if pnd == nil {
		req.Result <- WriteRequestResult{
			Err: errors.New("device closed"),
		}
		return
	}

	r.activeWriteMu.Lock()
	if r.activeWrite != nil {
		log.Warn().Msgf("write already in progress")
		req.Result <- WriteRequestResult{
			Err: errors.New("write already in progress"),
		}
		r.activeWriteMu.Unlock()
		return
	}
	r.activeWrite = req
	r.activeWriteMu.Unlock()
	defer func() {
		r.activeWriteMu.Lock()
		r.activeWrite = nil
		r.activeWriteMu.Unlock()
	}()

	var count int
	var target nfc.Target
	var err error
	var lastPollErr error
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

		count, target, err = pnd.InitiatorPollTarget(
			tags.SupportedCardTypes,
			timesToPoll,
			periodBetweenPolls,
		)

		if err != nil && !errors.Is(err, nfc.Error(nfc.ETIMEOUT)) {
			log.Warn().Msgf("could not poll: %s", err)
			lastPollErr = err
		}

		if count > 0 {
			break
		}

		tries--
	}

	if count == 0 {
		// A timeout (or clean poll) with no tag is the expected "nothing
		// presented" case. A non-timeout poll error means the reader actually
		// failed, so surface it as a genuine fault rather than masking it as
		// ErrTagNotDetected (which downstream treats as an expected condition).
		if lastPollErr != nil {
			req.Result <- WriteRequestResult{
				Err: fmt.Errorf("failed to poll for tag: %w", lastPollErr),
			}
			return
		}
		log.Warn().Msgf("could not detect a tag")
		req.Result <- WriteRequestResult{
			Err: readers.ErrTagNotDetected,
		}
		return
	}

	uid := tags.UID(target)
	cardUID := hex.EncodeToString(uid)
	log.Info().Msgf("found tag with ID: %s", cardUID)
	if req.Options.ExcludeUID != "" && cardUID == req.Options.ExcludeUID {
		req.Result <- WriteRequestResult{
			Err: fmt.Errorf("refusing to write excluded tag: %s", cardUID),
		}
		return
	}
	if req.Options.TargetUID != "" && cardUID != req.Options.TargetUID {
		req.Result <- WriteRequestResult{
			Err: fmt.Errorf("refusing to write wrong tag: got %s want %s", cardUID, req.Options.TargetUID),
		}
		return
	}

	res, err := nfctag.Write(context.Background(), &nfctag.Tag{
		IO:   tags.NewIO(pnd, uid),
		UID:  uid,
		Kind: tags.Kind(target),
	}, req.Text)
	if err != nil {
		if errors.Is(err, nfctag.ErrVerify) {
			err = &DataCorruptedError{Device: r.conn.ConnectionString(), Err: err}
		}
		req.Result <- WriteRequestResult{
			Err: fmt.Errorf("failed to write to tag: %w", err),
		}
		return
	}

	t := &tokens.Token{
		Type:     res.Type,
		UID:      cardUID,
		Text:     res.Text,
		Data:     hex.EncodeToString(res.Data),
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
	}

	log.Info().Msgf("successfully wrote to card: %s", cardUID)
	// The written tag stays quiet until it leaves the reader, and its removal
	// is flagged so the next tap of it is a normal scan.
	r.prevToken = t
	r.writtenTag = true
	r.misses = 0
	req.Result <- WriteRequestResult{
		Token: t,
	}
}
