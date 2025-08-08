/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package cores

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

func copySetnameBios(cfg *config.Instance, origCore, newCore *Core, name string) error {
	var biosPath string

	// TODO: this will not work on 100% of cores anymore because some core IDs
	// don't match to the name of their games folder. will need to be fixed later
	// with a way to reverse lookup a core back to a system (which has the folder)
	for _, folder := range misterconfig.RootDirs(cfg) {
		checkPath := filepath.Join(folder, origCore.ID, name)
		if _, err := os.Stat(checkPath); err == nil {
			biosPath = checkPath
			break
		}
	}

	if biosPath == "" || newCore.SetName == "" {
		return nil
	}

	newFolder, err := filepath.Abs(filepath.Join(filepath.Dir(biosPath), "..", newCore.SetName))
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, err := os.Stat(filepath.Join(newFolder, name)); err == nil {
		return nil
	}

	if err := os.MkdirAll(newFolder, 0o755); err != nil { //nolint:gosec // shared games folders
		return fmt.Errorf("failed to create directory %s: %w", newFolder, err)
	}

	if err := helpers.CopyFile(biosPath, filepath.Join(newFolder, name)); err != nil {
		return fmt.Errorf("failed to copy file %s to %s: %w", biosPath, filepath.Join(newFolder, name), err)
	}
	return nil
}

func hookFDS(cfg *config.Instance, system *Core, _ string) (string, error) {
	nesSystem, err := GetCore("NES")
	if err != nil {
		return "", err
	}

	return "", copySetnameBios(cfg, nesSystem, system, "boot0.rom")
}

func hookWSC(cfg *config.Instance, system *Core, _ string) (string, error) {
	wsSystem, err := GetCore("WonderSwan")
	if err != nil {
		return "", err
	}

	err = copySetnameBios(cfg, wsSystem, system, "boot.rom")
	if err != nil {
		return "", err
	}

	return "", copySetnameBios(cfg, wsSystem, system, "boot1.rom")
}

func hookAo486(_ *config.Instance, system *Core, path string) (string, error) {
	mglDef, err := PathToMGLDef(system, path)
	if err != nil {
		return "", err
	}

	var mgl string

	if !strings.HasSuffix(strings.ToLower(path), ".vhd") {
		return "", nil
	}

	dir := filepath.Dir(path)
	filename := filepath.Base(path)

	// exception for Top 300 pack which uses 2 disks
	if strings.HasSuffix(path, "IDE 0-1 Top 300 DOS Games.vhd") {
		mgl += fmt.Sprintf(
			"\t<file delay=\"%d\" type=%q index=\"%d\" path=%q/>\n",
			mglDef.Delay,
			mglDef.Method,
			mglDef.Index,
			"../../../../.."+filepath.Join(dir, "IDE 0-0 BOOT-DOS98.vhd"),
		)

		mgl += fmt.Sprintf(
			"\t<file delay=\"%d\" type=%q index=\"%d\" path=%q/>\n",
			mglDef.Delay,
			mglDef.Method,
			mglDef.Index+1,
			"../../../../.."+path,
		)

		mgl += "\t<reset delay=\"1\"/>\n"

		return mgl, nil
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	// if there's an iso in the same folder, mount it too
	for _, file := range files {
		fileName := strings.ToLower(file.Name())
		if (strings.HasSuffix(fileName, ".iso") || strings.HasSuffix(fileName, ".chd")) && file.Name() != filename {
			mgl += fmt.Sprintf(
				"\t<file delay=\"%d\" type=%q index=\"%d\" path=%q/>\n",
				mglDef.Delay,
				mglDef.Method,
				4,
				"../../../../.."+filepath.Join(dir, file.Name()),
			)
			break
		}
	}

	mgl += fmt.Sprintf(
		"\t<file delay=\"%d\" type=%q index=\"%d\" path=%q/>\n",
		mglDef.Delay,
		mglDef.Method,
		mglDef.Index,
		"../../../../.."+path,
	)

	mgl += "\t<reset delay=\"1\"/>\n"

	return mgl, nil
}

func hookAmiga(_ *config.Instance, _ *Core, path string) (string, error) {
	dirPath := strings.ToLower(filepath.Dir(path))
	if !strings.HasSuffix(dirPath, "listings/games.txt") && !strings.HasSuffix(dirPath, "listings/demos.txt") {
		return "", nil
	}

	gameName := filepath.Base(path)
	sharedPath, err := filepath.Abs(filepath.Join(filepath.Dir(path), "..", "..", "shared"))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	bootFile := filepath.Join(sharedPath, "ags_boot")
	if err := os.WriteFile(bootFile, []byte(gameName+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("failed to write boot file: %w", err)
	}

	return "\t<setname>Amiga</setname>\n", nil
}

func hookNeoGeo(_ *config.Instance, _ *Core, path string) (string, error) {
	// neogeo core allows launching zips and folders
	if strings.HasSuffix(strings.ToLower(path), ".zip") || filepath.Ext(path) == "" {
		return fmt.Sprintf(
			"\t<file delay=\"%d\" type=%q index=\"%d\" path=\"../../../../..%s\"/>\n",
			1,
			"f",
			1,
			path,
		), nil
	}

	return "", nil
}

var systemHooks = map[string]func(*config.Instance, *Core, string) (string, error){
	"FDS":             hookFDS,
	"WonderSwanColor": hookWSC,
	"ao486":           hookAo486,
	"Amiga":           hookAmiga,
	"NeoGeo":          hookNeoGeo,
}

func RunSystemHook(cfg *config.Instance, system *Core, path string) (string, error) {
	if hook, ok := systemHooks[system.ID]; ok {
		return hook(cfg, system, path)
	}

	return "", nil
}
