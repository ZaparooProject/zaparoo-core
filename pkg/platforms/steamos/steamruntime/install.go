//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/adrg/xdg"
	"github.com/spf13/afero"
)

const (
	runtimeExecutableName = "zaparoo-steam-runtime"
	runtimeDisplayName    = "Zaparoo Runtime"
	statusDuplicate       = "duplicate"
	statusMissing         = "missing"
	statusReady           = "ready"
	statusStale           = "stale"
)

//go:embed artwork/banner.png
var bannerArtwork []byte

//go:embed artwork/cover.png
var coverArtwork []byte

//go:embed artwork/hero.png
var heroArtwork []byte

//go:embed artwork/logo.png
var logoArtwork []byte

var defaultArtwork = []struct {
	suffix string
	data   []byte
}{
	{suffix: ".png", data: bannerArtwork},
	{suffix: "p.png", data: coverArtwork},
	{suffix: "_hero.png", data: heroArtwork},
	{suffix: "_logo.png", data: logoArtwork},
}

type InstallPaths struct {
	FS       afero.Fs
	Binary   string
	Runtime  string
	Desktop  string
	SteamDir string
}

func (p *InstallPaths) fileSystem() afero.Fs {
	if p.FS == nil {
		return afero.NewOsFs()
	}
	return p.FS
}

type InstallResult struct {
	ShortcutID         uint64
	ShortcutAdded      bool
	SteamRestartNeeded bool
}

type StatusResult struct {
	State       string
	RuntimePath string
	DesktopPath string
	ShortcutIDs []uint64
}

func DefaultInstallPaths() *InstallPaths {
	fs := afero.NewOsFs()
	home, _ := os.UserHomeDir()
	binary, err := os.Executable()
	if err != nil {
		binary = filepath.Join(home, ".local", "bin", "zaparoo")
	}
	steamDir := filepath.Join(home, ".steam", "steam")
	if _, err := fs.Stat(steamDir); err != nil {
		steamDir = filepath.Join(home, ".local", "share", "Steam")
	}
	return &InstallPaths{
		FS:       fs,
		Binary:   binary,
		Runtime:  filepath.Join(home, ".local", "bin", runtimeExecutableName),
		Desktop:  filepath.Join(xdg.DataHome, "applications", runtimeExecutableName+".desktop"),
		SteamDir: steamDir,
	}
}

func escapeDesktopExecPath(path string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"`", "\\`",
		`$`, `\$`,
		`%`, `%%`,
	)
	return `"` + replacer.Replace(path) + `"`
}

func desktopEntry(runtimePath string) []byte {
	return fmt.Appendf(nil, `[Desktop Entry]
Type=Application
Name=%s
Comment=Launch games through Zaparoo Core
Exec=%s
Icon=zaparoo
Terminal=false
Categories=Game;
`, runtimeDisplayName, escapeDesktopExecPath(runtimePath))
}

func writeDefaultArtwork(fs afero.Fs, path string, data []byte) error {
	// Preserve artwork users have replaced through Steam or another artwork manager.
	file, err := fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // User-owned Steam artwork.
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create default artwork: %w", err)
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		_ = fs.Remove(path)
		return fmt.Errorf("write default artwork: %w", err)
	}
	if err = file.Close(); err != nil {
		_ = fs.Remove(path)
		return fmt.Errorf("close default artwork: %w", err)
	}
	return nil
}

func installDefaultArtwork(fs afero.Fs, locations []shortcutLocation) error {
	for _, location := range locations {
		gridDir := filepath.Join(location.configDir, "grid")
		//nolint:gosec // Steam user artwork directory must remain traversable.
		if err := fs.MkdirAll(gridDir, 0o755); err != nil {
			return fmt.Errorf("create Steam artwork directory: %w", err)
		}
		prefix := filepath.Join(gridDir, strconv.FormatUint(uint64(location.appID), 10))
		for _, artwork := range defaultArtwork {
			if err := writeDefaultArtwork(fs, prefix+artwork.suffix, artwork.data); err != nil {
				return fmt.Errorf("install Steam artwork: %w", err)
			}
		}
	}
	return nil
}

func lstatFS(fs afero.Fs, path string) (os.FileInfo, error) {
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(path)
		if err != nil {
			return nil, fmt.Errorf("lstat %s: %w", path, err)
		}
		return info, nil
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return info, nil
}

func readlinkFS(fs afero.Fs, path string) (string, error) {
	reader, ok := fs.(afero.LinkReader)
	if !ok {
		return "", afero.ErrNoReadlink
	}
	target, err := reader.ReadlinkIfPossible(path)
	if err != nil {
		return "", fmt.Errorf("read link %s: %w", path, err)
	}
	return target, nil
}

func symlinkFS(fs afero.Fs, target, path string) error {
	linker, ok := fs.(afero.Linker)
	if !ok {
		return afero.ErrNoSymlink
	}
	if err := linker.SymlinkIfPossible(target, path); err != nil {
		return fmt.Errorf("link %s to %s: %w", path, target, err)
	}
	return nil
}

func ensureRuntimeSymlink(fs afero.Fs, paths *InstallPaths) error {
	if !filepath.IsAbs(paths.Binary) || !filepath.IsAbs(paths.Runtime) {
		return errors.New("runtime paths must be absolute")
	}
	if _, err := fs.Stat(paths.Binary); err != nil {
		return fmt.Errorf("installed Zaparoo binary is unavailable: %w", err)
	}
	//nolint:gosec // User binary directory must remain traversable for desktop launchers.
	if err := fs.MkdirAll(filepath.Dir(paths.Runtime), 0o755); err != nil {
		return fmt.Errorf("create runtime binary directory: %w", err)
	}
	info, err := lstatFS(fs, paths.Runtime)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink == 0:
		return fmt.Errorf("refusing to replace non-symlink runtime path: %s", paths.Runtime)
	case err == nil:
		target, readErr := readlinkFS(fs, paths.Runtime)
		if readErr == nil && target == paths.Binary {
			return nil
		}
		if removeErr := fs.Remove(paths.Runtime); removeErr != nil {
			return fmt.Errorf("remove stale runtime symlink: %w", removeErr)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("inspect runtime symlink: %w", err)
	}
	if err := symlinkFS(fs, paths.Binary, paths.Runtime); err != nil {
		return fmt.Errorf("create runtime symlink: %w", err)
	}
	return nil
}

func installWithExecutor(ctx context.Context, paths *InstallPaths, executor command.Executor) (InstallResult, error) {
	fs := paths.fileSystem()
	if err := ensureRuntimeSymlink(fs, paths); err != nil {
		return InstallResult{}, err
	}
	//nolint:gosec // XDG application directory must be traversable by desktop launchers.
	if err := fs.MkdirAll(filepath.Dir(paths.Desktop), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create runtime application directory: %w", err)
	}
	//nolint:gosec // Desktop entry is intentionally public metadata.
	if err := afero.WriteFile(fs, paths.Desktop, desktopEntry(paths.Runtime), 0o644); err != nil {
		return InstallResult{}, fmt.Errorf("write runtime desktop entry: %w", err)
	}

	locations, err := findShortcutLocations(fs, paths.SteamDir, paths.Runtime, paths.Desktop)
	if err == nil {
		if artworkErr := installDefaultArtwork(fs, locations); artworkErr != nil {
			return InstallResult{}, artworkErr
		}
		return InstallResult{ShortcutID: locations[0].bigPictureID}, nil
	}
	if !errors.Is(err, errShortcutNotFound) {
		return InstallResult{}, err
	}

	addCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if runErr := executor.Run(addCtx, "steamos-add-to-steam", paths.Desktop); runErr != nil {
		return InstallResult{}, fmt.Errorf("add Zaparoo shortcut to Steam: %w", runErr)
	}
	locations, err = findShortcutLocations(fs, paths.SteamDir, paths.Runtime, paths.Desktop)
	if err == nil {
		if artworkErr := installDefaultArtwork(fs, locations); artworkErr != nil {
			return InstallResult{}, artworkErr
		}
	} else if !errors.Is(err, errShortcutNotFound) {
		return InstallResult{}, err
	}
	return InstallResult{ShortcutAdded: true, SteamRestartNeeded: true}, nil
}

func statusWithPaths(paths *InstallPaths) (StatusResult, error) {
	fs := paths.fileSystem()
	result := StatusResult{RuntimePath: paths.Runtime, DesktopPath: paths.Desktop}
	ids, err := findShortcutIDs(fs, paths.SteamDir, paths.Runtime, paths.Desktop)
	if err != nil && !errors.Is(err, errShortcutNotFound) {
		return result, err
	}
	result.ShortcutIDs = ids

	runtimeReady := false
	if info, statErr := lstatFS(fs, paths.Runtime); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		target, readErr := readlinkFS(fs, paths.Runtime)
		runtimeReady = readErr == nil && target == paths.Binary
	}
	_, desktopErr := fs.Stat(paths.Desktop)
	desktopReady := desktopErr == nil
	switch {
	case len(ids) > 1:
		result.State = statusDuplicate
	case len(ids) == 1 && runtimeReady && desktopReady:
		result.State = statusReady
	case len(ids) == 0 && !runtimeReady && !desktopReady:
		result.State = statusMissing
	default:
		result.State = statusStale
	}
	return result, nil
}

func Status() (StatusResult, error) {
	return statusWithPaths(DefaultInstallPaths())
}

func Install(ctx context.Context) (InstallResult, error) {
	return installWithExecutor(ctx, DefaultInstallPaths(), &command.RealExecutor{})
}

func uninstallWithPaths(paths *InstallPaths) error {
	fs := paths.fileSystem()
	info, err := lstatFS(fs, paths.Runtime)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink == 0:
		return fmt.Errorf("refusing to remove non-symlink runtime path: %s", paths.Runtime)
	case err == nil:
		if removeErr := fs.Remove(paths.Runtime); removeErr != nil {
			return fmt.Errorf("remove Steam Runtime symlink: %w", removeErr)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("inspect Steam Runtime symlink: %w", err)
	}
	if err := fs.Remove(paths.Desktop); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Steam Runtime desktop entry: %w", err)
	}
	return nil
}

func Uninstall() error {
	return uninstallWithPaths(DefaultInstallPaths())
}
