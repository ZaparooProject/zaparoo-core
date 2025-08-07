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

package mister

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

// TODO: is there a zaparoo findfile somewhere?
func FindFile(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)

	files, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		target := file.Name()

		if len(target) != len(name) {
			continue
		} else if strings.EqualFold(target, name) {
			return filepath.Join(parent, target), nil
		}
	}

	return "", fmt.Errorf("file match not found: %s", path)
}

type PathResult struct {
	Path   string
	System Core
}

func GetActiveSystemPaths(cfg *config.Instance, systems []Core) []PathResult {
	var matches []PathResult

	gamesFolders := misterconfig.RootDirs(cfg)
	for _, system := range systems {
		for _, gamesFolder := range gamesFolders {
			gf, err := FindFile(gamesFolder)
			if err != nil {
				continue
			}

			found := false

			for _, folder := range []string{} { // TODO: this should use the systems defs on zaparoo itself
				systemFolder := filepath.Join(gf, folder)
				path, err := FindFile(systemFolder)
				if err != nil {
					continue
				}

				matches = append(matches, PathResult{path, system})
				found = true
				break
			}

			if found {
				break
			}
		}

		if len(matches) == len(systems) {
			break
		}
	}

	return matches
}

func copySetnameBios(cfg *config.Instance, origSystem, newSystem *Core, name string) error {
	var biosPath string

	for _, folder := range GetActiveSystemPaths(cfg, []Core{*origSystem}) {
		checkPath := filepath.Join(folder.Path, name)
		if _, err := os.Stat(checkPath); err == nil {
			biosPath = checkPath
			break
		}
	}

	if biosPath == "" || newSystem.SetName == "" {
		return nil
	}

	newFolder, err := filepath.Abs(filepath.Join(filepath.Dir(biosPath), "..", newSystem.SetName))
	if err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(newFolder, name)); err == nil {
		return nil
	}

	if err := os.MkdirAll(newFolder, 0o755); err != nil { //nolint:gosec // shared games folders
		return err
	}

	return helpers.CopyFile(biosPath, filepath.Join(newFolder, name))
}

func hookFDS(cfg *config.Instance, system *Core, _ string) (string, error) {
	nesSystem, err := GetSystem("NES")
	if err != nil {
		return "", err
	}

	return "", copySetnameBios(cfg, nesSystem, system, "boot0.rom")
}

func hookWSC(cfg *config.Instance, system *Core, _ string) (string, error) {
	wsSystem, err := GetSystem("WonderSwan")
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
			"\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=%q/>\n",
			mglDef.Delay,
			mglDef.Method,
			mglDef.Index,
			"../../../../.."+filepath.Join(dir, "IDE 0-0 BOOT-DOS98.vhd"),
		)

		mgl += fmt.Sprintf(
			"\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=%q/>\n",
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
		return "", err
	}

	// if there's an iso in the same folder, mount it too
	for _, file := range files {
		if (strings.HasSuffix(strings.ToLower(file.Name()), ".iso") || strings.HasSuffix(strings.ToLower(file.Name()), ".chd")) && file.Name() != filename {
			mgl += fmt.Sprintf(
				"\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=%q/>\n",
				mglDef.Delay,
				mglDef.Method,
				4,
				"../../../../.."+filepath.Join(dir, file.Name()),
			)
			break
		}
	}

	mgl += fmt.Sprintf(
		"\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=%q/>\n",
		mglDef.Delay,
		mglDef.Method,
		mglDef.Index,
		"../../../../.."+path,
	)

	mgl += "\t<reset delay=\"1\"/>\n"

	return mgl, nil
}

func hookAmiga(_ *config.Instance, _ *Core, path string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(filepath.Dir(path)), "listings/games.txt") && !strings.HasSuffix(strings.ToLower(filepath.Dir(path)), "listings/demos.txt") {
		return "", nil
	}

	gameName := filepath.Base(path)
	sharedPath, err := filepath.Abs(filepath.Join(filepath.Dir(path), "..", "..", "shared"))
	if err != nil {
		return "", err
	}

	bootFile := filepath.Join(sharedPath, "ags_boot")
	if err := os.WriteFile(bootFile, []byte(gameName+"\n"), 0o600); err != nil {
		return "", err
	}

	return "\t<setname>Amiga</setname>\n", nil
}

func hookNeoGeo(_ *config.Instance, _ *Core, path string) (string, error) {
	// neogeo core allows launching zips and folders
	if strings.HasSuffix(strings.ToLower(path), ".zip") || filepath.Ext(path) == "" {
		return fmt.Sprintf(
			"\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"../../../../..%s\"/>\n",
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
