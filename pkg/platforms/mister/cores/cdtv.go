//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package cores

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// Main_MiSTer's support/minimig/minimig_config.h stores four 1026-byte
// hardfile records, then CPU/autofire/info, then separate CD32 and CDTV drives.
// Older CD32 launch configurations use a different layout and must not be patched
// at these offsets. Current Main saves 7268 bytes, or 7270 with a2065_mode.
const (
	minimigCD32DriveOffset = 5216
	minimigCDTVDriveOffset = 6242
	minimigCDTVPathOffset  = 6244
	minimigCDPathSize      = 1024
	minimigCDTVConfigSize  = 7268
)

func hookCDTV(_ *config.Instance, core *Core, path string) (string, error) {
	return hookCDTVWithFS(afero.NewOsFs(), core, path)
}

func hookCDTVWithFS(fs afero.Fs, core *Core, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if core.SetNameSameDir || core.SetName == "" || strings.ContainsAny(core.SetName, "/\\\x00") {
		return "", errors.New("CDTV requires a dedicated configuration setname without same_dir")
	}
	configPath := filepath.Join(misterconfig.SDRootDir, "config", core.SetName+".cfg")
	if err := prepareCDTVConfig(fs, configPath, path); err != nil {
		return "", err
	}
	// A nonempty override suppresses the file tag: Minimig's MGL interface only
	// addresses floppy slots. Main mounts this disc while loading the config.
	return "\n", nil
}

func prepareCDTVConfig(fs afero.Fs, configPath, mediaPath string) error {
	ext := strings.ToLower(filepath.Ext(mediaPath))
	if ext != ".chd" && ext != ".cue" && ext != ".iso" {
		return fmt.Errorf("unsupported CDTV file format: %s (supported: .chd, .cue, .iso)", ext)
	}
	if !filepath.IsAbs(mediaPath) || strings.ContainsRune(mediaPath, 0) {
		return errors.New("CDTV image path must be absolute and contain no NUL bytes")
	}
	// Main accepts absolute paths. Preserve SD, USB, and network mount roots
	// instead of interpreting them relative to the currently selected storage.
	mediaPath = filepath.Clean(mediaPath)
	if len(mediaPath) >= minimigCDPathSize {
		return fmt.Errorf("CDTV image path too long: %d bytes (max: %d)", len(mediaPath), minimigCDPathSize-1)
	}
	mediaInfo, err := fs.Stat(mediaPath)
	if err != nil {
		return fmt.Errorf("CDTV image not accessible: %w", err)
	}
	if !mediaInfo.Mode().IsRegular() {
		return fmt.Errorf("CDTV image is not a regular file: %s", mediaPath)
	}

	file, err := fs.OpenFile(configPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open CDTV configuration %s: %w; save a CDTV preset as this configuration first",
			configPath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close CDTV configuration")
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat CDTV configuration: %w", err)
	}
	if !info.Mode().IsRegular() || (info.Size() != minimigCDTVConfigSize && info.Size() != minimigCDTVConfigSize+2) {
		return fmt.Errorf("unsupported CDTV configuration size: %d; save a CDTV preset with current MiSTer Main",
			info.Size())
	}
	data := make([]byte, info.Size())
	if _, readErr := io.ReadFull(file, data); readErr != nil {
		return fmt.Errorf("read CDTV configuration: %w", readErr)
	}
	if !bytes.Equal(data[:8], []byte("MNMGCFG0")) || data[minimigCD32DriveOffset] != 0 ||
		data[minimigCDTVDriveOffset] != 1 {
		return errors.New("invalid CDTV configuration: requires Minimig CDTV preset with CD32 disabled")
	}

	filename := make([]byte, minimigCDPathSize)
	copy(filename, mediaPath)
	// Change only the CDTV filename, retaining the user's BIOS, chipset, and
	// display configuration. Zero padding clears any previous longer filename.
	n, err := file.WriteAt(filename, minimigCDTVPathOffset)
	if err != nil {
		return fmt.Errorf("write CDTV image path: %w", err)
	}
	if n != len(filename) {
		return fmt.Errorf("write CDTV image path: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync CDTV configuration: %w", err)
	}
	return nil
}
