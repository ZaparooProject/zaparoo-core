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
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	sharedlaunchers "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/gamescope"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const (
	RetroArchFlatpakID     = "org.libretro.RetroArch"
	RetroArchNetworkAddr   = "127.0.0.1:55355"
	retroArchNetworkConfig = "retroarch-network.cfg"
)

// DesktopRetroArchConfigPath returns the shared Linux desktop network-command overlay path.
func DesktopRetroArchConfigPath() string {
	return filepath.Join(linuxbase.Settings().ConfigDir, retroArchNetworkConfig)
}

// DesktopRetroArchOptions builds standard Flatpak RetroArch options for Linux desktops.
func DesktopRetroArchOptions(appendConfigPath string) sharedretroarch.Options {
	options := sharedretroarch.Options{
		Exec: []string{"flatpak", "run", RetroArchFlatpakID},
		CoresDir: filepath.Join(
			sharedlaunchers.FlatpakAppPath(RetroArchFlatpakID), "config", "retroarch", "cores",
		),
		AppendConfigPath: appendConfigPath,
		NetworkCmdAddr:   RetroArchNetworkAddr,
		Preflight: sharedretroarch.MemoizePreflight(func(_ string) error {
			if !sharedlaunchers.IsFlatpakInstalled(RetroArchFlatpakID) {
				return errors.New("RetroArch Flatpak is not installed")
			}
			return EnsureRetroArchNetworkConfig(appendConfigPath)
		}),
	}
	return options
}

// DesktopEmulationOptions applies shared desktop launch-environment wiring.
//
//nolint:gocritic // Value API owns a copy so caller-provided RetroArch options remain unchanged.
func DesktopEmulationOptions(
	gameMode *gamescope.Manager,
	retroArch sharedretroarch.Options,
) Options {
	launchEnv := func() []string { return linuxbase.DesktopSessionEnvOverrides(gameMode) }
	retroArch.LaunchEnv = launchEnv
	options := NewOptions("", retroArch)
	options.LaunchEnv = launchEnv
	return options
}

// EnsureRetroArchNetworkConfig writes the shared network-command overlay.
func EnsureRetroArchNetworkConfig(path string) error {
	if err := sharedretroarch.EnsureNetworkCommandConfig(nil, path); err != nil {
		return fmt.Errorf("write RetroArch network config: %w", err)
	}
	return nil
}

func retroArchAvailable(options *Options) bool {
	if options == nil || options.RetroArch == nil || len(options.RetroArch.Exec) == 0 {
		return false
	}
	executable := options.RetroArch.Exec[0]
	if executable == "flatpak" {
		return options.IsFlatpakInstalled(RetroArchFlatpakID)
	}
	if filepath.IsAbs(executable) {
		info, err := os.Stat(executable)
		return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
	}
	_, err := options.LookPath(executable)
	return err == nil
}

func defaultRetroArchLaunchers(options *Options) []platforms.Launcher {
	cores := sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop)
	result := make([]platforms.Launcher, 0, len(cores))
	for i := range cores {
		launcher := sharedretroarch.NewLauncher(*options.RetroArch, cores[i])
		launcher.Groups = append(launcher.Groups, platformshared.LauncherGroupNative)
		wrapGameMode(options, &launcher)
		result = append(result, launcher)
	}
	return result
}

// AttachPlainESDEScanners enriches filesystem results from explicit index roots
// using standard ~/ES-DE/gamelists metadata. It never adds or scans roots.
func AttachPlainESDEScanners(cfg *config.Instance, options Options, catalog []platforms.Launcher) {
	options.normalize()
	configurePlainESDEScanners(cfg, &options, catalog)
}

func configurePlainESDEScanners(cfg *config.Instance, options *Options, catalog []platforms.Launcher) {
	if cfg == nil || len(cfg.IndexRoots()) == 0 {
		return
	}
	gamelistsPath := filepath.Join(options.HomeDir, "ES-DE", "gamelists")
	if !directoryExists(gamelistsPath) {
		return
	}
	attached := make(map[string]struct{})
	for i := range catalog {
		launcher := &catalog[i]
		if launcher.SystemID == "" || launcher.Scanner != nil || launcher.SkipFilesystemScan ||
			len(launcher.Folders) == 0 {
			continue
		}
		if _, exists := attached[launcher.SystemID]; exists {
			continue
		}
		folder := firstRelativeFolder(launcher.Folders)
		if folder == "" {
			continue
		}
		launcher.Scanner = plainESDEScanner(cfg.IndexRoots(), gamelistsPath, folder)
		attached[launcher.SystemID] = struct{}{}
	}
}

func plainESDEScanner(
	roots []string,
	gamelistsPath, folder string,
) func(context.Context, *config.Instance, string, []platforms.ScanResult) ([]platforms.ScanResult, error) {
	rootCopy := append([]string(nil), roots...)
	return func(
		ctx context.Context,
		_ *config.Instance,
		_ string,
		results []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		byPath := make(map[string]platforms.ScanResult, len(results))
		for i := range results {
			byPath[results[i].Path] = results[i]
		}
		for _, root := range rootCopy {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := esde.EnhanceResultsFromGamelist(byPath, esde.ScannerConfig{
				RomsBasePath: root, GamelistBasePath: gamelistsPath, SystemFolder: folder,
			}); err != nil {
				return nil, fmt.Errorf("enhance ES-DE results: %w", err)
			}
		}
		output := make([]platforms.ScanResult, len(results))
		for i := range results {
			output[i] = byPath[results[i].Path]
		}
		return output, nil
	}
}

func firstRelativeFolder(folders []string) string {
	for _, folder := range folders {
		if folder != "" && !filepath.IsAbs(folder) {
			return folder
		}
	}
	return ""
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
