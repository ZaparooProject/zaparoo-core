// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package pn532uart

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/ndef"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

// SerialPort defines the interface for serial port operations (for mocking in tests).
type SerialPort interface {
	Close() error
}

// ConnectFunc creates and initializes a PN532 UART connection.
type ConnectFunc func(name string) (SerialPort, error)

// PN532Commander handles PN532 protocol commands.
type PN532Commander interface {
	InListPassiveTarget(port SerialPort) (*Target, error)
	InDataExchange(port SerialPort, data []byte) ([]byte, error)
}

// DefaultPN532Commander uses the real PN532 command functions.
type DefaultPN532Commander struct{}

func (DefaultPN532Commander) InListPassiveTarget(port SerialPort) (*Target, error) {
	// Cast to serial.Port for the actual command
	if sp, ok := port.(serial.Port); ok {
		return InListPassiveTarget(sp)
	}
	return nil, errors.New("invalid port type")
}

func (DefaultPN532Commander) InDataExchange(port SerialPort, data []byte) ([]byte, error) {
	// Cast to serial.Port for the actual command
	if sp, ok := port.(serial.Port); ok {
		return InDataExchange(sp, data)
	}
	return nil, errors.New("invalid port type")
}

// DefaultConnectFunc wraps the package-level connect function.
func DefaultConnectFunc(name string) (SerialPort, error) {
	return connect(name)
}

type PN532UARTReader struct {
	port      SerialPort
	commander PN532Commander
	cfg       *config.Instance
	lastToken *tokens.Token
	connectFn ConnectFunc
	device    config.ReadersConnect
	name      string
	polling   bool
	mu        sync.RWMutex // protects polling
}

func NewReader(cfg *config.Instance) *PN532UARTReader {
	return &PN532UARTReader{
		cfg:       cfg,
		connectFn: DefaultConnectFunc,
		commander: DefaultPN532Commander{},
	}
}

func (*PN532UARTReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "legacy_pn532_uart",
		DefaultEnabled:    true,
		DefaultAutoDetect: false,
		Description:       "Legacy PN532 UART reader",
	}
}

func (*PN532UARTReader) IDs() []string {
	return []string{"legacy_pn532_uart"}
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
		return port, fmt.Errorf("failed to open serial port: %w", err)
	}

	err = port.SetReadTimeout(50 * time.Millisecond)
	if err != nil {
		return port, fmt.Errorf("failed to set read timeout: %w", err)
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
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	name := device.Path

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(name); err != nil {
			return fmt.Errorf("device path does not exist: %w", err)
		}
	}

	port, err := r.connectFn(name)
	if err != nil {
		if port != nil {
			_ = port.Close()
		}
		return err
	}

	r.port = port
	r.device = device
	r.name = name
	r.mu.Lock()
	r.polling = true
	r.mu.Unlock()

	go func() {
		errCount := 0
		maxErrors := 5
		zeroScans := 0
		maxZeroScans := 3

		for {
			r.mu.RLock()
			polling := r.polling
			r.mu.RUnlock()
			if !polling {
				break
			}
			if errCount >= maxErrors {
				log.Error().Msg("too many errors, exiting")

				// Send reader error notification to prevent triggering on_remove/exit
				if r.lastToken != nil {
					log.Warn().Msg("reader error with active token - sending error signal to keep media running")
					iq <- readers.Scan{
						Source:      r.device.ConnectionString(),
						Token:       nil,
						ReaderError: true,
					}
					r.lastToken = nil
				}

				err := r.Close()
				if err != nil {
					log.Warn().Err(err).Msg("failed to close serial port")
				}
				break
			}

			time.Sleep(250 * time.Millisecond)

			tgt, err := r.commander.InListPassiveTarget(r.port)
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

			errCount = 0
			zeroScans = 0

			if r.lastToken != nil && r.lastToken.UID == tgt.UID {
				// same token
				continue
			}

			if tgt.Type == tokens.TypeMifare {
				log.Debug().Msg("mifare not supported, skipping")
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

				res, exchangeErr := r.commander.InDataExchange(r.port, []byte{0x30, byte(i)})
				switch {
				case errors.Is(exchangeErr, ErrNoFrameFound):
					// sometimes the response just doesn't work, try again
					log.Warn().Msg("no frame found")
					blockRetry++
					continue readLoop
				case exchangeErr != nil:
					log.Error().Err(exchangeErr).Msg("failed to run indataexchange")
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

			tagText, err := ndef.ParseToText(data)
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
				UID:      tgt.UID,
				Text:     tagText,
				Data:     hex.EncodeToString(data),
				ScanTime: time.Now(),
				Source:   r.device.ConnectionString(),
			}

			if !helpers.TokensEqual(token, r.lastToken) {
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
	r.mu.Lock()
	r.polling = false
	r.mu.Unlock()
	if r.port != nil {
		err := r.port.Close()
		if err != nil {
			return fmt.Errorf("failed to close serial port: %w", err)
		}
	}
	return nil
}

// keep track of serial devices that had failed opens
var (
	serialCacheMu   = &sync.RWMutex{}
	serialBlockList []string
)

func (*PN532UARTReader) Detect(connected []string) string {
	ports, err := helpers.GetSerialDeviceList()
	if err != nil {
		log.Error().Err(err).Msg("failed to get serial ports")
	}

	for _, name := range ports {
		device := "legacy_pn532_uart:" + name

		// ignore if device is in block list
		serialCacheMu.RLock()
		if helpers.Contains(serialBlockList, name) {
			serialCacheMu.RUnlock()
			continue
		}
		serialCacheMu.RUnlock()

		// ignore if exact same device and reader are connected
		if helpers.Contains(connected, device) {
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
			if realPath != "" && helpers.Contains(connected, realPath) {
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
			closeErr := port.Close()
			if closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close serial port")
			}
		}

		if err != nil {
			log.Trace().Err(err).Msgf("failed to open detected serial port, blocklisting: %s", name)
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.polling && r.port != nil
}

func (r *PN532UARTReader) Info() string {
	return "PN532 UART (" + r.name + ")"
}

func (*PN532UARTReader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*PN532UARTReader) CancelWrite() {
	// no-op, writing not supported
}

func (*PN532UARTReader) Capabilities() []readers.Capability {
	return []readers.Capability{}
}

func (*PN532UARTReader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
