package pn532_uart

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"

	"go.bug.st/serial"
)

type Pn532UartReader struct {
	cfg       *config.Instance
	device    string
	name      string
	polling   bool
	port      serial.Port
	lastToken *tokens.Token
}

func NewReader(cfg *config.Instance) *Pn532UartReader {
	return &Pn532UartReader{
		cfg: cfg,
	}
}

func (r *Pn532UartReader) Ids() []string {
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

	err = port.SetReadTimeout(100 * time.Millisecond)
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

func (r *Pn532UartReader) Open(device string, iq chan<- readers.Scan) error {
	ps := strings.SplitN(device, ":", 2)
	if len(ps) != 2 {
		return errors.New("invalid device string: " + device)
	}

	if !utils.Contains(r.Ids(), ps[0]) {
		return errors.New("invalid reader id: " + ps[0])
	}

	name := ps[1]

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
							Source: r.device,
							Token:  nil,
						}
						r.lastToken = nil
					}
				}

				continue
			}

			log.Debug().Msgf("target: %s", tgt.Uid)

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

			i := 3
			retryMax := 3
			retry := 0
			data := make([]byte, 0)
			for {
				// TODO: this is a random limit i picked, should detect blocks in card
				if i >= 256 {
					break
				}

				if retry >= retryMax {
					errCount++
					break
				}

				res, err := InDataExchange(r.port, []byte{0x30, byte(i)})
				if errors.Is(err, ErrNoFrameFound) {
					// sometimes the response just doesn't work, try again
					log.Warn().Msg("no frame found")
					retry++
					continue
				} else if err != nil {
					log.Error().Err(err).Msg("failed to run indataexchange")
					errCount++
					break
				} else if len(res) < 2 {
					log.Error().Msg("unexpected data response length")
					errCount++
					break
				} else if res[0] != 0x41 || res[1] != 0x00 {
					log.Warn().Msgf("unexpected data format: %x", res)
					// sometimes we receive the result of the last passive
					// target command, so just try request again a few times
					retry++
					continue
				} else if bytes.Equal(res[2:], make([]byte, 16)) {
					break
				}

				data = append(data, res[2:]...)
				i += 4

				retry = 0

				time.Sleep(1 * time.Millisecond)
			}

			log.Debug().Msgf("record bytes: %s", hex.EncodeToString(data))

			tagText, err := ParseRecordText(data)
			if err != nil {
				log.Error().Err(err).Msgf("error parsing NDEF record")
				// TODO: there should be some distinction between a data
				// transfer error and a legitimate empty/missing NDEF record
				tagText = ""
			}

			if tagText == "" {
				log.Warn().Msg("no text NDEF found")
			} else {
				log.Info().Msgf("decoded text NDEF: %s", tagText)
			}

			token := &tokens.Token{
				Type:     tgt.Type,
				UID:      tgt.Uid,
				Text:     tagText,
				Data:     hex.EncodeToString(data),
				ScanTime: time.Now(),
				Source:   r.device,
			}

			if !utils.TokensEqual(token, r.lastToken) {
				iq <- readers.Scan{
					Source: r.device,
					Token:  token,
				}
			}

			r.lastToken = token
		}
	}()

	return nil
}

func (r *Pn532UartReader) Close() error {
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
var serialCacheMu = &sync.RWMutex{}
var serialBlockList []string

func (r *Pn532UartReader) Detect(connected []string) string {
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

func (r *Pn532UartReader) Device() string {
	return r.device
}

func (r *Pn532UartReader) Connected() bool {
	return r.polling && r.port != nil
}

func (r *Pn532UartReader) Info() string {
	return "PN532 UART (" + r.name + ")"
}

func (r *Pn532UartReader) Write(text string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}
