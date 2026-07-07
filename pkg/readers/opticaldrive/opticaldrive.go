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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/gameid"
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

// FSChecker checks filesystem paths used by the optical drive reader.
type FSChecker interface {
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
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

func (DefaultFSChecker) ReadDir(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir failed: %w", err)
	}
	return entries, nil
}

// DefaultCommandRunner runs real blkid commands.
type DefaultCommandRunner struct{}

func (DefaultCommandRunner) RunBlkid(ctx context.Context, valueType, devicePath string) ([]byte, error) {
	//nolint:gosec // G204: valueType and devicePath from OS, blkid reader's purpose
	out, err := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", valueType, devicePath).Output()
	if err != nil {
		return nil, fmt.Errorf("blkid command failed: %w", err)
	}
	return out, nil
}

type FileReader struct {
	fsChecker         FSChecker
	commandRunner     CommandRunner
	gameIDProbe       func(path string) []readers.ScanProperty
	cfg               *config.Instance
	device            config.ReadersConnect
	path              string
	sysBlockPath      string
	devPath           string
	wg                sync.WaitGroup
	mu                syncutil.RWMutex
	defaultEnabled    bool
	defaultAutoDetect bool
	polling           bool
}

func NewReader(cfg *config.Instance) *FileReader {
	return NewReaderWithDefaults(cfg, true, true)
}

func NewReaderWithDefaults(cfg *config.Instance, defaultEnabled, defaultAutoDetect bool) *FileReader {
	return &FileReader{
		cfg:               cfg,
		defaultEnabled:    defaultEnabled,
		defaultAutoDetect: defaultAutoDetect,
		fsChecker:         DefaultFSChecker{},
		commandRunner:     DefaultCommandRunner{},
		gameIDProbe:       identifyGameIDProperties,
		sysBlockPath:      filepath.Join(string(filepath.Separator), "sys", "block"),
		devPath:           filepath.Join(string(filepath.Separator), "dev"),
	}
}

func (r *FileReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "opticaldrive",
		DefaultEnabled:    r.defaultEnabled,
		DefaultAutoDetect: r.defaultAutoDetect,
		Description:       "Optical drive CD/DVD reader",
	}
}

func (*FileReader) IDs() []string {
	return []string{"opticaldrive", "optical_drive"}
}

func (r *FileReader) Open(
	device config.ReadersConnect,
	iq chan<- readers.Scan,
	_ readers.OpenOpts,
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
		var lastUUID, lastLabel, lastUUIDErr, lastLabelErr string
		hasProbed := false

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
					hasProbed = false
					iq <- readers.Scan{
						Source:      tokens.SourceReader,
						Token:       nil,
						ReaderError: true,
					}
					continue
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			rawUUID, uuidErr := r.commandRunner.RunBlkid(ctx, "UUID", r.path)
			cancel()

			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			rawLabel, labelErr := r.commandRunner.RunBlkid(ctx, "LABEL", r.path)
			cancel()

			uuid := strings.TrimSpace(string(rawUUID))
			label := strings.TrimSpace(string(rawLabel))
			uuidErrStr, labelErrStr := errString(uuidErr), errString(labelErr)

			probeChanged := !hasProbed || uuid != lastUUID || label != lastLabel ||
				uuidErrStr != lastUUIDErr || labelErrStr != lastLabelErr
			lastUUID, lastLabel, lastUUIDErr, lastLabelErr = uuid, label, uuidErrStr, labelErrStr
			hasProbed = true
			if !probeChanged {
				continue
			}

			id := getID(uuid, label)
			scanProperties := r.gameIDProbe(r.path)
			log.Debug().
				Str("path", r.path).
				Str("uuid", uuid).
				Str("label", label).
				Str("uuidErr", uuidErrStr).
				Str("labelErr", labelErrStr).
				Int("properties", len(scanProperties)).
				Msg("optical media identification probe changed")
			if id == "" && len(scanProperties) > 0 {
				id = "gameid/" + scanProperties[0].System + "/" + scanProperties[0].Value
			}

			if id == "" {
				if token != nil {
					log.Debug().
						Err(errors.Join(uuidErr, labelErr)).
						Msg("error identifying optical media, removing token")
					token = nil
					iq <- readers.Scan{
						Source: tokens.SourceReader,
						Token:  nil,
					}
				}
				continue
			}

			if token != nil && token.UID == id {
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
				Source:     tokens.SourceReader,
				Token:      token,
				Properties: scanProperties,
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

func (r *FileReader) Detect(exclude []string) string {
	sysBlockPath := r.sysBlockPath
	if sysBlockPath == "" {
		sysBlockPath = filepath.Join(string(filepath.Separator), "sys", "block")
	}
	devPath := r.devPath
	if devPath == "" {
		devPath = filepath.Join(string(filepath.Separator), "dev")
	}

	fsChecker := r.fsChecker
	if fsChecker == nil {
		fsChecker = DefaultFSChecker{}
	}

	entries, err := fsChecker.ReadDir(sysBlockPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "sr") {
			continue
		}
		path := filepath.Join(devPath, name)
		if opticalPathExcluded(path, exclude) {
			continue
		}
		if _, statErr := fsChecker.Stat(path); statErr != nil {
			continue
		}
		return "opticaldrive:" + path
	}

	return ""
}

func opticalPathExcluded(path string, exclude []string) bool {
	for _, excluded := range exclude {
		_, excludedPath, found := strings.Cut(excluded, ":")
		if found && excludedPath == path {
			return true
		}
		if !found && excluded == path {
			return true
		}
	}
	return false
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func identifyGameIDProperties(path string) []readers.ScanProperty {
	started := time.Now()
	log.Debug().Str("path", path).Msg("live gameid identification started")
	candidates := gameid.IdentifyLiveDisc(path)
	duration := time.Since(started)
	if len(candidates) == 0 {
		log.Debug().Str("path", path).Dur("duration", duration).Msg("live gameid not found")
		return nil
	}

	log.Debug().Str("path", path).Int("candidates", len(candidates)).Dur("duration", duration).
		Msg("live gameid identified")

	out := make([]readers.ScanProperty, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, readers.ScanProperty{
			System: candidate.SystemID,
			Name:   string(tags.TagPropertyGameID),
			Value:  candidate.ID,
		})
	}
	return out
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
