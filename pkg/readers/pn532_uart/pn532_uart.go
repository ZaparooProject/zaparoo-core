package pn532_uart

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

type PN532UARTReader struct {
	cfg       *config.Instance
	device    config.ReadersConnect
	name      string
	polling   bool
	port      serial.Port
	lastToken *tokens.Token
}

func NewReader(cfg *config.Instance) *PN532UARTReader {
	return &PN532UARTReader{
		cfg: cfg,
	}
}

func (r *PN532UARTReader) Ids() []string {
	return []string{"pn532_uart"}
}

func connect(name string) (serial.Port, error) {
	log.Debug().Msgf("connecting to %s", name)
	port, err := serial.Open(name, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return port, err
	}

	err = port.SetReadTimeout(50 * time.Millisecond)
	if err != nil {
		return port, err
	}

	err = SamConfiguration(port)
	if err != nil {
		return port, err
	}

	fv, err := GetFirmwareVersion(port)
	if err != nil {
		return port, err
	}
	log.Debug().Msgf("firmware version: %v", fv)

	return port, nil
}

func (r *PN532UARTReader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !utils.Contains(r.Ids(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	name := device.Path

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(name); err != nil {
			return err
		}
	}

	port, err := connect(name)
	if err != nil {
		if port != nil {
			_ = port.Close()
		}
		return err
	}

	r.port = port
	r.device = device
	r.name = name
	r.polling = true

	go func() {
		errCount := 0
		maxErrors := 5
		zeroScans := 0
		maxZeroScans := 3

		for r.polling {
			if errCount >= maxErrors {
				log.Error().Msg("too many errors, exiting")
				err := r.Close()
				if err != nil {
					log.Warn().Err(err).Msg("failed to close serial port")
				}
				r.polling = false
				break
			}

			time.Sleep(250 * time.Millisecond)

			tgt, err := InListPassiveTarget(r.port)
			if err != nil {
				log.Error().Err(err).Msg("failed to read passive target")
				errCount++
				continue
			} else if tgt == nil {
				zeroScans++

				// token was removed
				if zeroScans == maxZeroScans && r.lastToken != nil {
					if r.lastToken != nil {
						iq <- readers.Scan{
							Source: r.device.ConnectionString(),
							Token:  nil,
						}
						r.lastToken = nil
					}
				}

				continue
			}

			// log.Debug().Msgf("target: %s", tgt.Uid)

			errCount = 0
			zeroScans = 0

			if r.lastToken != nil && r.lastToken.UID == tgt.Uid {
				// same token
				continue
			}

			if tgt.Type == tokens.TypeMifare {
				log.Error().Err(err).Msg("mifare not supported")
				continue
			}

			ndefRetryMax := 3
			ndefRetry := 0
		ndefRetry:

			i := 3
			blockRetryMax := 3
			blockRetry := 0
			data := make([]byte, 0)
		readLoop:
			for i < 256 {
				// TODO: this is a random limit i picked, should detect blocks in card

				if blockRetry >= blockRetryMax {
					errCount++
					break readLoop
				}

				res, err := InDataExchange(r.port, []byte{0x30, byte(i)})
				switch {
				case errors.Is(err, ErrNoFrameFound):
					// sometimes the response just doesn't work, try again
					log.Warn().Msg("no frame found")
					blockRetry++
					continue readLoop
				case err != nil:
					log.Error().Err(err).Msg("failed to run indataexchange")
					errCount++
					break readLoop
				case len(res) < 2:
					log.Error().Msg("unexpected data response length")
					errCount++
					break readLoop
				case res[0] != 0x41 || res[1] != 0x00:
					log.Warn().Msgf("unexpected data format: %x", res)
					// sometimes we receive the result of the last passive
					// target command, so just try request again a few times
					blockRetry++
					continue readLoop
				case bytes.Equal(res[2:], make([]byte, 16)):
					break readLoop
				}

				data = append(data, res[2:]...)
				i += 4

				blockRetry = 0
			}

			log.Debug().Msgf("record bytes: %s", hex.EncodeToString(data))

			tagText, err := ParseRecordText(data)
			if err != nil && ndefRetry < ndefRetryMax {
				log.Error().Err(err).Msgf("no NDEF found, retrying data exchange")
				ndefRetry++
				goto ndefRetry
			} else if err != nil {
				log.Error().Err(err).Msgf("no NDEF records")
				tagText = ""
			}

			if tagText != "" {
				log.Info().Msgf("decoded text NDEF: %s", tagText)
			}

			token := &tokens.Token{
				Type:     tgt.Type,
				UID:      tgt.Uid,
				Text:     tagText,
				Data:     hex.EncodeToString(data),
				ScanTime: time.Now(),
				Source:   r.device.ConnectionString(),
			}

			if !utils.TokensEqual(token, r.lastToken) {
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  token,
				}
			}

			r.lastToken = token
		}
	}()

	return nil
}

func (r *PN532UARTReader) Close() error {
	r.polling = false
	if r.port != nil {
		err := r.port.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// keep track of serial devices that had failed opens
var (
	serialCacheMu   = &sync.RWMutex{}
	serialBlockList []string
)

func (r *PN532UARTReader) Detect(connected []string) string {
	ports, err := utils.GetSerialDeviceList()
	if err != nil {
		log.Error().Err(err).Msg("failed to get serial ports")
	}

	for _, name := range ports {
		device := "pn532_uart:" + name

		// ignore if device is in block list
		serialCacheMu.RLock()
		if utils.Contains(serialBlockList, name) {
			serialCacheMu.RUnlock()
			continue
		}
		serialCacheMu.RUnlock()

		// ignore if exact same device and reader are connected
		if utils.Contains(connected, device) {
			continue
		}

		if runtime.GOOS != "windows" {
			// resolve device symlink if necessary
			realPath := ""
			symPath, err := os.Readlink(name)
			if err == nil {
				parent := filepath.Dir(name)
				abs, err := filepath.Abs(filepath.Join(parent, symPath))
				if err == nil {
					realPath = abs
				}
			}

			// ignore if same resolved device and reader connected
			if realPath != "" && utils.Contains(connected, realPath) {
				continue
			}

			// ignore if different resolved device and reader connected
			if realPath != "" && strings.HasSuffix(realPath, ":"+realPath) {
				continue
			}
		}

		// ignore if different reader already connected
		match := false
		for _, connDev := range connected {
			if strings.HasSuffix(connDev, ":"+name) {
				match = true
				break
			}
		}
		if match {
			continue
		}

		// try to open the device
		port, err := connect(name)
		if port != nil {
			err := port.Close()
			if err != nil {
				log.Warn().Err(err).Msg("failed to close serial port")
			}
		}

		if err != nil {
			log.Debug().Err(err).Msgf("failed to open detected serial port, blocklisting: %s", name)
			serialCacheMu.Lock()
			serialBlockList = append(serialBlockList, name)
			serialCacheMu.Unlock()
			continue
		}

		return device
	}

	return ""
}

func (r *PN532UARTReader) Device() string {
	return r.device.ConnectionString()
}

func (r *PN532UARTReader) Connected() bool {
	return r.polling && r.port != nil
}

func (r *PN532UARTReader) Info() string {
	return "PN532 UART (" + r.name + ")"
}

func (r *PN532UARTReader) Write(text string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (r *PN532UARTReader) CancelWrite() {
	// no-op, writing not supported
}
