// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

// Package standalone holds the operating-system independent parts of the
// built-in standalone emulator launchers: argument builders, launcher target
// files and the Windows emulator table and install discovery.
package standalone

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxLauncherTargetSize = 4096

// Context is what an argument builder knows about one launch.
type Context struct {
	// MAMEConfig returns MAME's -showconfig output, for launchers that read
	// MAME's own settings.
	MAMEConfig func() ([]byte, error)
	// Path is the absolute media path being launched.
	Path string
	// InstallDir is the folder holding the emulator executable, when known.
	InstallDir string
	// Exe is the emulator executable, when known.
	Exe string
}

// ArgsFunc builds the emulator arguments for one launch.
type ArgsFunc func(ctx *Context) ([]string, error)

// Batch passes prefix followed by the media path.
func Batch(prefix ...string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		args := make([]string, 0, len(prefix)+1)
		args = append(args, prefix...)
		return append(args, ctx.Path), nil
	}
}

// Around passes prefix, the media path, then suffix.
func Around(prefix []string, suffix ...string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		args := make([]string, 0, len(prefix)+len(suffix)+1)
		args = append(args, prefix...)
		args = append(args, ctx.Path)
		return append(args, suffix...), nil
	}
}

// Joined passes prefix followed by flag and the media path as one argument,
// for emulators that only accept -flag=value.
func Joined(flag string, prefix ...string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		args := make([]string, 0, len(prefix)+1)
		args = append(args, prefix...)
		return append(args, flag+ctx.Path), nil
	}
}

// ShadPS4Args launches the game named by a .ps4 target file.
func ShadPS4Args(ctx *Context) ([]string, error) {
	target, err := ReadLauncherTarget(ctx.Path, ".ps4")
	if err != nil {
		return nil, err
	}
	return []string{"-g", target, "--fullscreen", "true"}, nil
}

// CemuArgs launches a Wii U title.
func CemuArgs(ctx *Context) ([]string, error) { return []string{"-g", ctx.Path, "-f"}, nil }

// Vita3KArgs launches the title ID named by a .psvita target file.
func Vita3KArgs(ctx *Context) ([]string, error) {
	titleID, err := ReadLauncherTarget(ctx.Path, ".psvita")
	if err != nil {
		return nil, err
	}
	return []string{"-F", "-r", titleID}, nil
}

// RPCS3Args launches a PS3 game, resolving a .ps3 target file first.
func RPCS3Args(ctx *Context) ([]string, error) {
	path := ctx.Path
	if strings.EqualFold(filepath.Ext(path), ".ps3") {
		target, err := ReadLauncherTarget(path, ".ps3")
		if err != nil {
			return nil, err
		}
		path = target
	}
	return []string{"--no-gui", "--fullscreen", path}, nil
}

// ScummVMArgs launches the game ID named by a .scummvm target file from the
// target file's folder.
func ScummVMArgs(ctx *Context) ([]string, error) {
	target, err := ScummVMTarget(ctx.Path)
	if err != nil {
		return nil, err
	}
	return []string{"--fullscreen", "--path=" + filepath.Dir(ctx.Path), target}, nil
}

// ScummVMTarget reads the game ID from a .scummvm target file. An empty file
// names the game by its file name, as Batocera and RetroBat lay them out.
func ScummVMTarget(path string) (string, error) {
	if !strings.EqualFold(filepath.Ext(path), ".scummvm") {
		return "", errors.New("ScummVM launch requires a .scummvm target file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read ScummVM target: %w", err)
	}
	if info.Size() == 0 {
		target := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if target == "" || strings.HasPrefix(target, "-") {
			return "", errors.New("read ScummVM target: invalid launcher target")
		}
		return target, nil
	}
	target, err := ReadLauncherTarget(path, ".scummvm")
	if err != nil {
		return "", fmt.Errorf("read ScummVM target: %w", err)
	}
	return target, nil
}

// ReadLauncherTarget reads the one-line target stored in a small launcher
// file such as .ps3, .psvita or .scummvm.
func ReadLauncherTarget(path, extension string) (string, error) {
	if !strings.EqualFold(filepath.Ext(path), extension) {
		return "", fmt.Errorf("launcher target requires a %s file", extension)
	}
	//nolint:gosec // Path was resolved through the media database and launch allow-list.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open launcher target: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file; close errors do not affect parsed data.
	data, err := io.ReadAll(io.LimitReader(file, maxLauncherTargetSize+1))
	if err != nil {
		return "", fmt.Errorf("read launcher target: %w", err)
	}
	target := strings.TrimSpace(string(data))
	if target == "" || len(data) > maxLauncherTargetSize || strings.HasPrefix(target, "-") ||
		strings.ContainsAny(target, "\r\n\x00") {
		return "", errors.New("invalid launcher target")
	}
	return target, nil
}

// ValidateMediaFile requires an absolute path to an existing regular file.
func ValidateMediaFile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("media path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat media path: %w", err)
	}
	if info.IsDir() {
		return errors.New("media path must be a file")
	}
	return nil
}

// SetName returns a ROM set name taken from an archive file name, for
// emulators that load sets by name from a ROM path.
func SetName(path string, extensions ...string) (string, error) {
	extension := filepath.Ext(path)
	allowed := false
	for _, candidate := range extensions {
		if strings.EqualFold(extension, candidate) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("launch requires a %s ROM set", strings.Join(extensions, " or "))
	}
	name := strings.TrimSuffix(filepath.Base(path), extension)
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n;\"") {
		return "", errors.New("invalid ROM set name")
	}
	return name, nil
}

// AltirraArgs starts Altirra fullscreen in the given machine's default
// profile (5200 or xegs). Headerless cartridge images carry no mapper, so
// one is picked from the image size; otherwise Altirra stops to ask.
func AltirraArgs(machine string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		args := []string{"/f", "/defprofile:" + machine}
		switch strings.ToLower(filepath.Ext(ctx.Path)) {
		case ".a52", ".bin", ".rom":
			info, err := os.Stat(ctx.Path)
			if err != nil {
				return nil, fmt.Errorf("stat cartridge: %w", err)
			}
			if mapper := altirraCartMapper(machine, info.Size()); mapper != "" {
				// Altirra only applies the mapper to an image given with /cart.
				return append(args, "/cartmapper", mapper, "/cart", ctx.Path), nil
			}
		}
		return append(args, ctx.Path), nil
	}
}

// altirraCartMapper returns Altirra's mapper number for a headerless
// cartridge of the given size, or "" when the size is ambiguous.
func altirraCartMapper(machine string, size int64) string {
	if machine == "5200" {
		return map[int64]string{4096: "20", 8192: "19", 16384: "6", 32768: "4", 40960: "7"}[size]
	}
	if size <= 8192 {
		return "1"
	}
	return map[int64]string{
		16384: "2", 32768: "12", 65536: "13", 131072: "14", 262144: "23", 524288: "24", 1048576: "25",
	}[size]
}

// DOSBoxArgs runs a DOS program, or loads a .conf file whose autoexec starts
// the game.
func DOSBoxArgs(flags ...string) ArgsFunc {
	return func(ctx *Context) ([]string, error) {
		args := append([]string(nil), flags...)
		if strings.EqualFold(filepath.Ext(ctx.Path), ".conf") {
			return append(args, "-conf", ctx.Path), nil
		}
		return append(args, ctx.Path), nil
	}
}
