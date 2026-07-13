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
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/gamescope"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const (
	RetroArchFlatpakID    = "org.libretro.RetroArch"
	retroArchNetworkAddr  = "127.0.0.1:55355"
	retroArchNativeConfig = "retroarch-native.cfg"
)

func defaultRetroArchAppendConfigPath() string {
	return filepath.Join(linuxbase.Settings().ConfigDir, retroArchNativeConfig)
}

func defaultRetroArchConfigDir() string {
	return filepath.Join(
		launchers.FlatpakAppPath(RetroArchFlatpakID),
		"config", "retroarch",
	)
}

func steamOSRetroArchOptions(appendConfigPath string) sharedretroarch.Options {
	opts := sharedretroarch.Options{
		Exec:             []string{"flatpak", "run", RetroArchFlatpakID},
		CoresDir:         filepath.Join(defaultRetroArchConfigDir(), "cores"),
		AppendConfigPath: appendConfigPath,
		NetworkCmdAddr:   retroArchNetworkAddr,
		Preflight: sharedretroarch.MemoizePreflight(func(_ string) error {
			if !launchers.IsFlatpakInstalled(RetroArchFlatpakID) {
				return errors.New("RetroArch Flatpak is not installed")
			}
			return nil
		}),
	}
	opts.LaunchEnv = steamOSLaunchEnvOverrides
	return opts
}

func nativeRetroArchLaunchers(opts *sharedretroarch.Options) []platforms.Launcher {
	cores := sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop)
	result := make([]platforms.Launcher, 0, len(cores))
	configDir := filepath.Dir(opts.AppendConfigPath)
	for i := range cores {
		coreOpts := *opts
		coreOpts.AppendConfigPath = nativeRetroArchSystemConfigPath(configDir, cores[i].SystemID)
		launcher := sharedretroarch.NewLauncher(coreOpts, cores[i])
		launcher.Groups = append(launcher.Groups, platformshared.LauncherGroupNative)
		withGamescopeFocus(&launcher)
		result = append(result, launcher)
	}
	return result
}

var steamOSGameMode = gamescope.NewManager(gamescope.SessionOptions{Enabled: true})

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
			go steamOSGameMode.ManageFocus(proc)
		}
		return proc, nil
	}
	launcher.Kill = func(cfg *config.Instance) error {
		steamOSGameMode.RevertFocus()
		if kill == nil {
			return nil
		}
		return kill(cfg)
	}
}
