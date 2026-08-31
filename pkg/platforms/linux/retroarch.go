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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const (
	retroArchFlatpakID   = "org.libretro.RetroArch"
	retroArchNetworkAddr = "127.0.0.1:55355"
)

func linuxRetroArchOptions() sharedretroarch.Options {
	return sharedretroarch.Options{
		Exec: []string{"flatpak", "run", retroArchFlatpakID},
		CoresDir: filepath.Join(
			launchers.FlatpakAppPath(retroArchFlatpakID),
			"config", "retroarch", "cores",
		),
		AppendConfigPath: linuxemu.DesktopRetroArchConfigPath(),
		NetworkCmdAddr:   retroArchNetworkAddr,
		Preflight: sharedretroarch.MemoizePreflight(func(_ string) error {
			if !launchers.IsFlatpakInstalled(retroArchFlatpakID) {
				return errors.New("RetroArch Flatpak is not installed")
			}
			if err := sharedretroarch.EnsureNetworkCommandConfig(
				nil, linuxemu.DesktopRetroArchConfigPath(),
			); err != nil {
				return fmt.Errorf("write RetroArch network config: %w", err)
			}
			return nil
		}),
	}
}
