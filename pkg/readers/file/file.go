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

package file

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const TokenType = "file"

type Reader struct {
	cfg     *config.Instance
	device  config.ReadersConnect
	path    string
	polling bool
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg: cfg,
	}
}

func (*Reader) IDs() []string {
	return []string{"file"}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if !filepath.IsAbs(path) {
		return errors.New("invalid device path, must be absolute")
	}

	parent := filepath.Dir(path)
	if parent == "" {
		return errors.New("invalid device path")
	}

	if _, err := os.Stat(parent); err != nil {
		return fmt.Errorf("failed to stat parent directory: %w", err)
	}

	if _, err := os.Stat(path); err != nil {
		// attempt to create empty file
		//nolint:gosec // Safe: creates file reader token files in controlled directories
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		_ = f.Close()
	}

	r.device = device
	r.path = path
	r.polling = true

	go func() {
		var token *tokens.Token

		for r.polling {
			time.Sleep(100 * time.Millisecond)

			contents, err := os.ReadFile(r.path)
			if err != nil {
				// TODO: have a max retries?
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Error:  err,
				}
				continue
			}

			text := strings.TrimSpace(string(contents))

			// "remove" the token if the file is now empty
			if text == "" && token != nil {
				log.Debug().Msg("file is empty, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				continue
			}

			if token != nil && token.Text == text {
				continue
			}

			if text == "" {
				continue
			}

			token = &tokens.Token{
				Type:     TokenType,
				Text:     text,
				Data:     hex.EncodeToString(contents),
				ScanTime: time.Now(),
				Source:   r.device.ConnectionString(),
			}

			log.Debug().Msgf("new token: %s", token.Text)
			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token:  token,
			}
		}
	}()

	return nil
}

func (r *Reader) Close() error {
	r.polling = false
	return nil
}

func (*Reader) Detect(_ []string) string {
	return ""
}

func (r *Reader) Device() string {
	return r.device.ConnectionString()
}

func (r *Reader) Connected() bool {
	return r.polling
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
