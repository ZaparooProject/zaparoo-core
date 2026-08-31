//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"fmt"
	"path/filepath"

	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/spf13/afero"
)

const retroArchNativeConfigPrefix = "retroarch-native-"

func nativeRetroArchSystemConfigPath(configDir, systemID string) string {
	return filepath.Join(configDir, retroArchNativeConfigPrefix+systemID+".cfg")
}

func ensureNativeRetroArchSystemConfigs(
	fs afero.Fs,
	zaparooConfigDir string,
	cores []sharedretroarch.CoreLaunch,
) error {
	contents, err := sharedretroarch.ConfigForProfile(sharedretroarch.ConfigProfileLowLatency)
	if err != nil {
		return fmt.Errorf("build low-latency RetroArch profile: %w", err)
	}
	if err := fs.MkdirAll(zaparooConfigDir, 0o750); err != nil {
		return fmt.Errorf("create Zaparoo config directory: %w", err)
	}

	written := make(map[string]struct{}, len(cores))
	for i := range cores {
		systemID := cores[i].SystemID
		if systemID == "" {
			continue
		}
		if _, exists := written[systemID]; exists {
			continue
		}
		path := nativeRetroArchSystemConfigPath(zaparooConfigDir, systemID)
		if err := afero.WriteFile(fs, path, []byte(contents), 0o600); err != nil {
			return fmt.Errorf("write native RetroArch config for %s: %w", systemID, err)
		}
		written[systemID] = struct{}{}
	}
	return nil
}
