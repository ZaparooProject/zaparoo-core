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

package rs232barcode

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

const maxBufferSize = 8192 // 8KB limit (QR Code v40 max: ~7KB numeric, ~4.3KB alphanumeric)

type Reader struct {
	port        testutils.SerialPort
	portFactory testutils.SerialPortFactory
	cfg         *config.Instance
	device      config.ReadersConnect
	path        string
	polling     bool
	mu          syncutil.RWMutex // protects polling
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:         cfg,
		portFactory: testutils.DefaultSerialPortFactory,
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "rs232barcode",
		DefaultEnabled:    true,
		DefaultAutoDetect: false,
		Description:       "RS232 barcode/QR code reader",
	}
}

func (*Reader) IDs() []string {
	return []string{"rs232barcode", "rs232_barcode"}
}

func (r *Reader) parseLine(line string) (*tokens.Token, error) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "\r")

	// Strip STX/ETX framing characters (used by some POS-configured scanners)
	line = strings.TrimPrefix(line, "\x02") // STX
	line = strings.TrimSuffix(line, "\x03") // ETX

	if line == "" {
		return nil, nil //nolint:nilnil // nil response means empty line, not an error
	}

	// For RS232 barcode readers, the entire line is the barcode data
	t := tokens.Token{
		Type:     tokens.TypeBarcode,
		UID:      line,
		Text:     line,
		Data:     line,
		ScanTime: time.Now(),
		Source:   r.device.ConnectionString(),
	}

	return &t, nil
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("failed to stat device path %s: %w", path, err)
		}
	}

	log.Debug().Msgf("opening RS232 barcode reader: %s", path)

	port, err := r.portFactory(path, &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return fmt.Errorf("failed to open serial port %s: %w", path, err)
	}

	err = port.SetReadTimeout(100 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to set read timeout on serial port: %w", err)
	}

	r.port = port
	r.device = device
	r.path = path
	r.mu.Lock()
	r.polling = true
	r.mu.Unlock()

	log.Info().Msgf("opened RS232 barcode reader: %s", path)

	go func() {
		buf := make([]byte, 1024)
		var lineBuf []byte
		overflowed := false

		for {
			r.mu.RLock()
			polling := r.polling
			r.mu.RUnlock()
			if !polling {
				break
			}

			n, err := r.port.Read(buf)

			// Process any bytes read, even if there's an error
			if n > 0 {
				for i := range n {
					b := buf[i]

					// Handle both \n and \r as line delimiters (some scanners use \r only)
					if b == '\n' || b == '\r' {
						if overflowed {
							overflowed = false
							lineBuf = lineBuf[:0]
							continue
						}

						if len(lineBuf) > 0 {
							line := string(lineBuf)
							lineBuf = lineBuf[:0]

							t, parseErr := r.parseLine(line)
							if parseErr != nil {
								log.Error().Err(parseErr).Msg("failed to parse barcode data")
								continue
							}

							if t != nil {
								log.Debug().Msgf("barcode scanned: %s", t.UID)
								iq <- readers.Scan{
									Source: r.device.ConnectionString(),
									Token:  t,
								}
							}
						}
						continue
					}

					if overflowed {
						continue
					}

					if len(lineBuf) >= maxBufferSize {
						log.Warn().Str("path", r.path).Msg("buffer overflow, discarding data until next delimiter")
						lineBuf = lineBuf[:0]
						overflowed = true
						continue
					}

					lineBuf = append(lineBuf, b)
				}
			}

			// Handle errors after processing any valid data
			if err != nil {
				log.Error().Err(err).Msg("failed to read from RS232 barcode reader")

				err = r.Close()
				if err != nil {
					log.Error().Err(err).Msg("failed to close RS232 barcode reader")
				}
				break
			}
		}
	}()

	return nil
}

func (r *Reader) Close() error {
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

func (*Reader) Detect(_ []string) string {
	return ""
}

func (r *Reader) Device() string {
	return r.device.ConnectionString()
}

func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.polling && r.port != nil
}

func (r *Reader) Info() string {
	return r.path
}

func (*Reader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*Reader) CancelWrite() {
	// no-op, writing not supported
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
