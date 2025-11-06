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

package simpleserial

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

// SerialPort defines the interface for serial port operations (for mocking in tests).
type SerialPort interface {
	Read(p []byte) (n int, err error)
	Close() error
	SetReadTimeout(t time.Duration) error
}

// SerialPortFactory creates a serial port connection.
type SerialPortFactory func(path string, mode *serial.Mode) (SerialPort, error)

// DefaultSerialPortFactory is the default factory that opens real serial ports.
func DefaultSerialPortFactory(path string, mode *serial.Mode) (SerialPort, error) {
	port, err := serial.Open(path, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return port, nil
}

type SimpleSerialReader struct {
	port        SerialPort
	portFactory SerialPortFactory
	cfg         *config.Instance
	lastToken   *tokens.Token
	device      config.ReadersConnect
	path        string
	polling     bool
	mu          sync.RWMutex // protects polling
}

func NewReader(cfg *config.Instance) *SimpleSerialReader {
	return &SimpleSerialReader{
		cfg:         cfg,
		portFactory: DefaultSerialPortFactory,
	}
}

func (*SimpleSerialReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "Simple serial protocol reader",
	}
}

func (*SimpleSerialReader) IDs() []string {
	return []string{"simpleserial", "simple_serial"}
}

func (r *SimpleSerialReader) parseLine(line string) (*tokens.Token, error) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "\r")

	if line == "" {
		return nil, nil //nolint:nilnil // nil response means empty line, not an error
	}

	if !strings.HasPrefix(line, "SCAN\t") {
		return nil, nil //nolint:nilnil // nil response means invalid format, not an error
	}

	args := line[5:]
	if args == "" {
		return nil, nil //nolint:nilnil // nil response means no args, not an error
	}

	t := tokens.Token{
		Data:     line,
		ScanTime: time.Now(),
		Source:   r.device.ConnectionString(),
	}

	ps := strings.Split(args, "\t")
	hasArg := false
	for i := 0; i < len(ps); i++ {
		ps[i] = strings.TrimSpace(ps[i])
		switch {
		case strings.HasPrefix(ps[i], "uid="):
			t.UID = ps[i][4:]
			hasArg = true
		case strings.HasPrefix(ps[i], "text="):
			t.Text = ps[i][5:]
			hasArg = true
		case strings.HasPrefix(ps[i], "removable="):
			// TODO: this isn't really what removable means, but it works
			//		 for now. it will block shell commands though
			t.FromAPI = ps[i][10:] == "no"
			hasArg = true
		}
	}

	// if there are no named arguments, whole args becomes text
	if !hasArg {
		t.Text = args
	}

	return &t, nil
}

func (r *SimpleSerialReader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("failed to stat device path %s: %w", path, err)
		}
	}

	port, err := r.portFactory(path, &serial.Mode{
		BaudRate: 115200,
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

	go func() {
		var lineBuf []byte

		for {
			r.mu.RLock()
			polling := r.polling
			r.mu.RUnlock()
			if !polling {
				break
			}
			buf := make([]byte, 1024)
			n, err := r.port.Read(buf)
			if err != nil {
				log.Error().Err(err).Msg("failed to read from serial port")

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

				err = r.Close()
				if err != nil {
					log.Error().Err(err).Msg("failed to close serial port")
				}
				break
			}

			for i := range n {
				if buf[i] == '\n' {
					line := string(lineBuf)
					lineBuf = nil

					t, err := r.parseLine(line)
					if err != nil {
						log.Error().Err(err).Msg("failed to parse line")
						continue
					}

					if t != nil && !helpers.TokensEqual(t, r.lastToken) {
						iq <- readers.Scan{
							Source: r.device.ConnectionString(),
							Token:  t,
						}
					}

					if t != nil {
						r.lastToken = t
					}
				} else {
					lineBuf = append(lineBuf, buf[i])
				}
			}

			// Token removal timeout: Consider a token "removed" if no new data is received
			// for 1 second. This timeout-based approach works for simple serial devices that
			// don't send explicit "REMOVED" messages. However, on slow or heavily loaded
			// systems, serial data processing delays could cause spurious token removal events.
			if r.lastToken != nil && time.Since(r.lastToken.ScanTime) > 1*time.Second {
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				r.lastToken = nil
			}
		}
	}()

	return nil
}

func (r *SimpleSerialReader) Close() error {
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

func (*SimpleSerialReader) Detect(_ []string) string {
	return ""
}

func (r *SimpleSerialReader) Device() string {
	return r.device.ConnectionString()
}

func (r *SimpleSerialReader) Connected() bool {
	return r.polling && r.port != nil
}

func (r *SimpleSerialReader) Info() string {
	return r.path
}

func (*SimpleSerialReader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*SimpleSerialReader) CancelWrite() {
	// no-op, writing not supported
}

func (*SimpleSerialReader) Capabilities() []readers.Capability {
	return []readers.Capability{}
}

func (*SimpleSerialReader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
