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

package opticaldrive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

const (
	TokenType         = "disc"
	IDSourceUUID      = "uuid"
	IDSourceLabel     = "label"
	IDSourceMerged    = "merged"
	MergedIDSeparator = "/"
)

type FileReader struct {
	cfg     *config.Instance
	device  config.ReadersConnect
	path    string
	polling bool
}

func NewReader(cfg *config.Instance) *FileReader {
	return &FileReader{
		cfg: cfg,
	}
}

func (*FileReader) IDs() []string {
	return []string{"optical_drive"}
}

func (r *FileReader) Open(
	device config.ReadersConnect,
	iq chan<- readers.Scan,
) error {
	log.Info().Msgf("opening optical drive reader: %s", device.ConnectionString())
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
		return fmt.Errorf("failed to stat device parent directory: %w", err)
	}

	r.device = device
	r.path = path
	r.polling = true

	getID := func(uuid string, label string) string {
		if uuid == "" {
			return label
		} else if label == "" {
			return uuid
		}

		switch r.device.IDSource {
		case IDSourceUUID:
			return uuid
		case IDSourceLabel:
			return label
		case IDSourceMerged:
			return uuid + MergedIDSeparator + label
		default:
			return uuid + MergedIDSeparator + label
		}
	}

	go func() {
		var token *tokens.Token

		for r.polling {
			time.Sleep(1 * time.Second)

			// Validate device path to prevent command injection
			if !strings.HasPrefix(r.path, "/dev/") {
				log.Error().Str("path", r.path).Msg("invalid optical drive device path")
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			//nolint:gosec // Safe: r.path validated as device path starting with /dev/
			rawUUID, err := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", "UUID", r.path).Output()
			cancel()
			if err != nil {
				if token == nil {
					continue
				}
				log.Debug().Err(err).Msg("error identifying optical media, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
			}

			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			//nolint:gosec // Safe: r.path validated as device path starting with /dev/
			rawLabel, err := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", "LABEL", r.path).Output()
			cancel()
			if err != nil {
				if token == nil {
					continue
				}
				log.Debug().Err(err).Msg("error identifying optical media, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
			}

			uuid := strings.TrimSpace(string(rawUUID))
			label := strings.TrimSpace(string(rawLabel))

			if uuid == "" && label == "" && token != nil {
				log.Debug().Msg("id is empty, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				continue
			}

			id := getID(uuid, label)
			if token != nil && token.UID == id {
				continue
			} else if id == "" {
				continue
			}

			token = &tokens.Token{
				Type:     TokenType,
				ScanTime: time.Now(),
				UID:      id,
			}

			log.Debug().Msgf("new token: %s", token.UID)
			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token:  token,
			}
		}
	}()

	return nil
}

func (r *FileReader) Close() error {
	r.polling = false
	return nil
}

func (*FileReader) Detect(_ []string) string {
	return ""
}

func (r *FileReader) Device() string {
	return r.device.ConnectionString()
}

func (r *FileReader) Connected() bool {
	return r.polling
}

func (r *FileReader) Info() string {
	return r.path
}

func (*FileReader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*FileReader) CancelWrite() {
	// no-op, writing not supported
}

func (*FileReader) Capabilities() []readers.Capability {
	return []readers.Capability{}
}

func (*FileReader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
