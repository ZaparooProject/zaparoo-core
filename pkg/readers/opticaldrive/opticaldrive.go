// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const (
	TokenType         = "disc"
	IDSourceUUID      = "uuid"
	IDSourceLabel     = "label"
	IDSourceMerged    = "merged"
	MergedIDSeparator = "/"
)

// FSChecker checks if files/devices exist.
type FSChecker interface {
	Stat(path string) (os.FileInfo, error)
}

// CommandRunner runs external commands.
type CommandRunner interface {
	RunBlkid(ctx context.Context, valueType, devicePath string) ([]byte, error)
}

// DefaultFSChecker uses os.Stat for filesystem checks.
type DefaultFSChecker struct{}

func (DefaultFSChecker) Stat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat failed: %w", err)
	}
	return info, nil
}

// DefaultCommandRunner runs real blkid commands.
type DefaultCommandRunner struct{}

func (DefaultCommandRunner) RunBlkid(ctx context.Context, valueType, devicePath string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", valueType, devicePath).Output()
	if err != nil {
		return nil, fmt.Errorf("blkid command failed: %w", err)
	}
	return out, nil
}

type FileReader struct {
	fsChecker     FSChecker
	commandRunner CommandRunner
	cfg           *config.Instance
	device        config.ReadersConnect
	path          string
	polling       bool
	mu            syncutil.RWMutex // protects polling
	wg            sync.WaitGroup   // tracks polling goroutine
}

func NewReader(cfg *config.Instance) *FileReader {
	return &FileReader{
		cfg:           cfg,
		fsChecker:     DefaultFSChecker{},
		commandRunner: DefaultCommandRunner{},
	}
}

func (*FileReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "opticaldrive",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "Optical drive CD/DVD reader",
	}
}

func (*FileReader) IDs() []string {
	return []string{"opticaldrive", "optical_drive"}
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

	if _, err := r.fsChecker.Stat(parent); err != nil {
		return fmt.Errorf("failed to stat device parent directory: %w", err)
	}

	r.device = device
	r.path = path
	r.mu.Lock()
	r.polling = true
	r.mu.Unlock()

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

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		var token *tokens.Token

		for {
			r.mu.RLock()
			polling := r.polling
			r.mu.RUnlock()
			if !polling {
				break
			}
			time.Sleep(1 * time.Second)

			// Validate device path to prevent command injection
			if !strings.HasPrefix(r.path, "/dev/") {
				log.Warn().Str("path", r.path).Msg("invalid optical drive device path")
				continue
			}

			// Check if device still exists (distinguish hardware error from disc removal)
			if _, err := r.fsChecker.Stat(r.path); err != nil {
				if token != nil {
					log.Warn().Err(err).Msg("optical drive device no longer exists - " +
						"sending error signal to keep media running")
					token = nil
					iq <- readers.Scan{
						Source:      tokens.SourceReader,
						Token:       nil,
						ReaderError: true,
					}
					continue
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			rawUUID, err := r.commandRunner.RunBlkid(ctx, "UUID", r.path)
			cancel()
			if err != nil {
				if token == nil {
					continue
				}
				// Device exists but blkid failed - this is normal disc removal
				log.Debug().Err(err).Msg("error identifying optical media, removing token")
				token = nil
				iq <- readers.Scan{
					Source: tokens.SourceReader,
					Token:  nil,
				}
			}

			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			rawLabel, err := r.commandRunner.RunBlkid(ctx, "LABEL", r.path)
			cancel()
			if err != nil {
				if token == nil {
					continue
				}
				log.Debug().Err(err).Msg("error identifying optical media, removing token")
				token = nil
				iq <- readers.Scan{
					Source: tokens.SourceReader,
					Token:  nil,
				}
			}

			uuid := strings.TrimSpace(string(rawUUID))
			label := strings.TrimSpace(string(rawLabel))

			if uuid == "" && label == "" && token != nil {
				log.Debug().Msg("id is empty, removing token")
				token = nil
				iq <- readers.Scan{
					Source: tokens.SourceReader,
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
				Source:   tokens.SourceReader,
				ReaderID: r.ReaderID(),
			}

			log.Debug().Msgf("new token: %s", token.UID)
			iq <- readers.Scan{
				Source: tokens.SourceReader,
				Token:  token,
			}
		}
	}()

	return nil
}

func (r *FileReader) Close() error {
	r.mu.Lock()
	r.polling = false
	r.mu.Unlock()
	r.wg.Wait()
	return nil
}

func (*FileReader) Detect(_ []string) string {
	return ""
}

func (r *FileReader) Path() string {
	return r.path
}

func (r *FileReader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	return []readers.Capability{readers.CapabilityRemovable}
}

func (*FileReader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

func (r *FileReader) ReaderID() string {
	return readers.GenerateReaderID(r.Metadata().ID, r.path)
}
