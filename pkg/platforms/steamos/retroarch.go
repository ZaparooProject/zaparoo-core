//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/gamescope"
)

const (
	RetroArchFlatpakID     = "org.libretro.RetroArch"
	retroArchNetworkAddr   = "127.0.0.1:55355"
	retroArchNetworkConfig = "retroarch-network.cfg"
)

func defaultRetroArchAppendConfigPath() string {
	return filepath.Join(linuxbase.Settings().ConfigDir, retroArchNetworkConfig)
}

func steamOSRetroArchOptions(appendConfigPath string) sharedretroarch.Options {
	return sharedretroarch.Options{
		Exec: []string{"flatpak", "run", RetroArchFlatpakID},
		CoresDir: filepath.Join(
			launchers.FlatpakAppPath(RetroArchFlatpakID),
			"config", "retroarch", "cores",
		),
		AppendConfigPath: appendConfigPath,
		NetworkCmdAddr:   retroArchNetworkAddr,
		Preflight: sharedretroarch.MemoizePreflight(func(_ string) error {
			if !launchers.IsFlatpakInstalled(RetroArchFlatpakID) {
				return errors.New("RetroArch Flatpak is not installed")
			}
			return nil
		}),
	}
}

func nativeRetroArchLaunchers(opts *sharedretroarch.Options) []platforms.Launcher {
	result := sharedretroarch.NewLaunchers(*opts, sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop))
	for i := range result {
		withGamescopeFocus(&result[i])
	}
	return result
}

func withGamescopeFocus(launcher *platforms.Launcher) {
	launch := launcher.Launch
	kill := launcher.Kill
	launcher.Launch = func(
		cfg *config.Instance,
		path string,
		opts *platforms.LaunchOptions,
	) (*os.Process, error) {
		proc, err := launch(cfg, path, opts)
		if err != nil {
			return nil, err
		}
		if proc != nil {
			go gamescope.ManageFocus(proc)
		}
		return proc, nil
	}
	launcher.Kill = func(cfg *config.Instance) error {
		gamescope.RevertFocus()
		if kill == nil {
			return nil
		}
		return kill(cfg)
	}
}
