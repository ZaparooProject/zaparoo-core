//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const (
	retroArchFlatpakID     = "org.libretro.RetroArch"
	retroArchNetworkAddr   = "127.0.0.1:55355"
	retroArchNetworkConfig = "retroarch-network.cfg"
)

func defaultRetroArchAppendConfigPath() string {
	return filepath.Join(linuxbase.Settings().ConfigDir, retroArchNetworkConfig)
}

func linuxRetroArchOptions() sharedretroarch.Options {
	return sharedretroarch.Options{
		Exec: []string{"flatpak", "run", retroArchFlatpakID},
		CoresDir: filepath.Join(
			launchers.FlatpakAppPath(retroArchFlatpakID),
			"config", "retroarch", "cores",
		),
		AppendConfigPath: defaultRetroArchAppendConfigPath(),
		NetworkCmdAddr:   retroArchNetworkAddr,
		Preflight: func(_ string) error {
			if !launchers.IsFlatpakInstalled(retroArchFlatpakID) {
				return errors.New("RetroArch Flatpak is not installed")
			}
			return nil
		},
	}
}

func ensureRetroArchNetworkConfig() error {
	if err := sharedretroarch.EnsureNetworkCommandConfig(nil, defaultRetroArchAppendConfigPath()); err != nil {
		return fmt.Errorf("write RetroArch network config: %w", err)
	}
	return nil
}
