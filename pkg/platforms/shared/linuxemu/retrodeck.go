//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/rs/zerolog/log"
)

const RetroDECKFlatpakID = "net.retrodeck.retrodeck"

func retroDECKLaunchers(_ *config.Instance, options *Options) []platforms.Launcher {
	if !options.IsFlatpakInstalled(RetroDECKFlatpakID) {
		return nil
	}
	paths := DefaultRetroDECKPaths(options.HomeDir)
	if !directoriesExist(paths.RomsPath, paths.GamelistPath) {
		return nil
	}
	folders, err := readProviderSystemFolders(paths.RomsPath)
	if err != nil {
		log.Warn().Err(err).Str("path", paths.RomsPath).Msg("failed to read RetroDECK ROM directory")
		return nil
	}
	result := make([]platforms.Launcher, 0, len(folders))
	for _, folder := range folders {
		info, ok := esde.LookupByFolderName(folder)
		if !ok {
			continue
		}
		result = append(result, createRetroDECKLauncher(options, folder, info, paths))
	}
	return result
}

func createRetroDECKLauncher(
	options *Options,
	folder string,
	info esde.SystemInfo,
	paths RetroDECKPaths,
) platforms.Launcher {
	launcher := platforms.Launcher{
		ID: "RetroDECK" + info.GetLauncherID(), SystemID: info.SystemID,
		Groups: []string{platformshared.LauncherGroupRetroDECK}, Lifecycle: platforms.LifecycleBlocking,
		SkipFilesystemScan: true, Test: providerPathTest(paths.RomsPath, folder),
		Availability: func(*config.Instance) error {
			if !options.IsFlatpakInstalled(RetroDECKFlatpakID) {
				return errors.New("RetroDECK Flatpak is not installed")
			}
			return nil
		},
		BuildLaunchCommand: func(
			_ *config.Instance,
			path string,
			_ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return retroDECKLaunchCommand(options, path), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command := retroDECKLaunchCommand(options, path)
			//nolint:gosec // Fixed Flatpak ID; path is validated against discovered provider root.
			cmd := exec.CommandContext(context.Background(), command.Executable, command.Args...)
			cmd.Env = helpers.MergeEnviron(os.Environ(), command.Env)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("launch RetroDECK: %w", err)
			}
			return cmd.Process, nil
		},
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return esde.ScanGamelist(esde.ScannerConfig{
				RomsBasePath: paths.RomsPath, GamelistBasePath: paths.GamelistPath, SystemFolder: folder,
			})
		},
	}
	guardProviderLauncher(&launcher)
	wrapGameMode(options, &launcher)
	return launcher
}

func retroDECKLaunchCommand(options *Options, path string) *platforms.LaunchCommand {
	var env []string
	if options.LaunchEnv != nil {
		env = options.LaunchEnv()
	}
	return &platforms.LaunchCommand{
		Executable: "flatpak",
		Args: []string{
			"run", "--die-with-parent", "--env=LOG_BUFFER=", RetroDECKFlatpakID, path,
		},
		Env: env,
	}
}
