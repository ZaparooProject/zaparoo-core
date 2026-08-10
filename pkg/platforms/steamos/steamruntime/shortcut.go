//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/vdfbinary"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
)

var errShortcutNotFound = errors.New("runtime shortcut not found")

type shortcutLocation struct {
	configDir    string
	appID        uint32
	bigPictureID uint64
}

func shortcutBigPictureID(appID uint32) uint64 {
	return (uint64(appID) << 32) | 0x02000000
}

func findShortcutLocations(steamDir string, targets ...string) ([]shortcutLocation, error) {
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target != "" {
			targetSet[filepath.Clean(target)] = struct{}{}
		}
	}

	userdataDir := filepath.Join(steamDir, "userdata")
	users, err := os.ReadDir(userdataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errShortcutNotFound
		}
		return nil, fmt.Errorf("read Steam userdata: %w", err)
	}
	var locations []shortcutLocation
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		configDir := filepath.Join(userdataDir, user.Name(), "config")
		path := filepath.Join(configDir, "shortcuts.vdf")
		data, readErr := os.ReadFile(path) //nolint:gosec // Steam user config path.
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read Steam shortcuts for user %s: %w", user.Name(), readErr)
		}
		shortcuts, parseErr := vdfbinary.ParseShortcuts(bytes.NewReader(data))
		if parseErr != nil {
			return nil, fmt.Errorf("parse Steam shortcuts for user %s: %w", user.Name(), parseErr)
		}
		for _, shortcut := range shortcuts {
			if _, ok := targetSet[steam.NormalizeShortcutExecutable(shortcut.Exe)]; ok {
				locations = append(locations, shortcutLocation{
					configDir: configDir, appID: shortcut.AppID, bigPictureID: shortcutBigPictureID(shortcut.AppID),
				})
			}
		}
	}
	if len(locations) == 0 {
		return nil, errShortcutNotFound
	}
	return locations, nil
}

func findShortcutIDs(steamDir string, targets ...string) ([]uint64, error) {
	locations, err := findShortcutLocations(steamDir, targets...)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(locations))
	for _, location := range locations {
		ids = append(ids, location.bigPictureID)
	}
	return ids, nil
}

func shortcutURL(id uint64) string {
	return "steam://rungameid/" + strconv.FormatUint(id, 10)
}
