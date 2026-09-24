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

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
)

// extraFile is a license text for an extra component.
type extraFile struct {
	name string
	text string
}

// extraComponent is third-party code or data that is not a Go module.
type extraComponent struct {
	component credits.Component
	files     []extraFile
}

// nativeLibrary is a C library statically linked into Linux builds. Its
// commit is read from the zigcc Dockerfile, which builds it.
type nativeLibrary struct {
	name      string
	repo      string
	commitVar string
	license   string
	textFile  string
	gplBase   bool
}

var nativeLibraries = []nativeLibrary{
	{
		name: "libnfc", repo: "https://github.com/nfc-tools/libnfc", commitVar: "LIBNFC_COMMIT",
		license: "LGPL-3.0", textFile: "libnfc-COPYING", gplBase: true,
	},
	{
		name: "libusb", repo: "https://github.com/libusb/libusb", commitVar: "LIBUSB_COMMIT",
		license: "LGPL-2.1", textFile: "libusb-COPYING",
	},
	{
		name: "libusb-compat-0.1", repo: "https://github.com/libusb/libusb-compat-0.1",
		commitVar: "LIBUSB_COMPAT_COMMIT", license: "LGPL-2.1", textFile: "libusb-compat-COPYING",
	},
}

const (
	dockerfilePath = "scripts/zigcc/Dockerfile"
	textsDir       = "scripts/credits/texts"
	sqliteModule   = "github.com/mattn/go-sqlite3"
	malgoModule    = "github.com/gen2brain/malgo"
)

// extras lists the components that do not come from Go modules: statically
// linked C libraries, C code bundled inside Go modules, vendored code, and
// embedded data.
func extras(root string, modules map[string]*linkedModule) ([]extraComponent, error) {
	read := func(rel string) (string, error) {
		data, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // fixed repository paths
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", rel, err)
		}
		return string(data), nil
	}
	gpl, err := read("LICENSE")
	if err != nil {
		return nil, err
	}
	dockerfile, err := read(dockerfilePath)
	if err != nil {
		return nil, err
	}

	var out []extraComponent
	for _, lib := range nativeLibraries {
		commit, commitErr := dockerfileCommit(dockerfile, lib.commitVar)
		if commitErr != nil {
			return nil, commitErr
		}
		text, readErr := read(filepath.Join(textsDir, lib.textFile))
		if readErr != nil {
			return nil, readErr
		}
		files := []extraFile{{name: "COPYING", text: text}}
		if lib.gplBase {
			// LGPL-3.0 is a set of additional permissions on top of GPL-3.0,
			// so the GPL text is part of its terms.
			files = append(files, extraFile{name: "COPYING.GPL", text: gpl})
		}
		out = append(out, extraComponent{
			component: credits.Component{
				Name:    lib.name,
				Version: commit[:12],
				URL:     lib.repo,
				License: lib.license,
				Note: "Statically linked into Linux builds. The source code for the exact version used is at " +
					lib.repo + "/tree/" + commit + ".",
			},
			files: files,
		})
	}

	sqlite, err := bundledCText(modules, sqliteModule, "sqlite3-binding.c", sqliteBlessing)
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "SQLite", URL: "https://sqlite.org", License: "Public domain",
			Note: "Bundled in " + sqliteModule + ".",
		},
		files: []extraFile{{name: "Public domain dedication", text: sqlite}},
	})

	miniaudio, err := bundledCText(modules, malgoModule, "miniaudio.h", miniaudioLicense)
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "miniaudio", URL: "https://miniaud.io", License: "Unlicense OR MIT-0",
			Note: "Bundled in " + malgoModule + ".",
		},
		files: []extraFile{{name: "LICENSE", text: miniaudio}},
	})

	vdf, err := read("internal/vdfbinary/LICENSE")
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "valve-vdf-binary", URL: "https://github.com/TimDeve/valve-vdf-binary", License: "MIT",
			Note: "Vendored and modified in internal/vdfbinary.",
		},
		files: []extraFile{{name: "LICENSE", text: vdf}},
	})

	eff, err := read(filepath.Join(textsDir, "eff-wordlist.txt"))
	if err != nil {
		return nil, err
	}
	out = append(out,
		extraComponent{
			component: credits.Component{
				Name: "EFF Short Wordlist #1", URL: "https://www.eff.org/dice", License: "CC-BY-3.0-US",
			},
			files: []extraFile{{name: "Attribution", text: eff}},
		},
		extraComponent{
			component: credits.Component{
				Name: "ArcadeDatabase_MiSTer", URL: "https://github.com/MiSTer-devel/ArcadeDatabase_MiSTer",
				License: "GPL-3.0", Note: "Arcade metadata embedded in MiSTer builds.",
			},
			files: []extraFile{{name: "LICENSE", text: gpl}},
		},
		extraComponent{
			component: credits.Component{
				Name: "Sound effects", URL: "https://timwilsie.com", License: "GPL-3.0",
				Note: "Sounds by Tim Wilsie (timwilsie.com), contributed to Zaparoo.",
			},
			files: []extraFile{{name: "LICENSE", text: gpl}},
		},
	)
	return out, nil
}

var dockerfileCommitPattern = regexp.MustCompile(`(?m)^ENV (\w+)="([0-9a-f]{40})"`)

func dockerfileCommit(dockerfile, name string) (string, error) {
	for _, match := range dockerfileCommitPattern.FindAllStringSubmatch(dockerfile, -1) {
		if match[1] == name {
			return match[2], nil
		}
	}
	return "", fmt.Errorf("%s not pinned in %s", name, dockerfilePath)
}

// bundledCText extracts a license statement from a C source file shipped
// inside a linked Go module, so the text follows the module's version.
func bundledCText(
	modules map[string]*linkedModule, modulePath, file string, extract func(string) (string, error),
) (string, error) {
	mod, ok := modules[modulePath]
	if !ok {
		return "", fmt.Errorf("module %s is not linked", modulePath)
	}
	path := filepath.Join(mod.dir, file)
	data, err := os.ReadFile(path) //nolint:gosec // module cache path from go list
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	text, err := extract(string(data))
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return text, nil
}

// sqliteBlessing returns SQLite's public domain dedication from the
// amalgamation's first file header.
func sqliteBlessing(source string) (string, error) {
	const first, last = "The author disclaims copyright to this source code.",
		"May you share freely, never taking more than you give."
	start := strings.Index(source, first)
	if start < 0 {
		return "", errors.New("SQLite dedication not found")
	}
	end := strings.Index(source[start:], last)
	if end < 0 {
		return "", errors.New("SQLite dedication end not found")
	}
	var lines []string
	for _, line := range strings.Split(source[start:start+end+len(last)], "\n") {
		lines = append(lines, strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*")))
	}
	return strings.Join(lines, "\n"), nil
}

// miniaudioLicense returns the license choice at the end of miniaudio.h.
func miniaudioLicense(source string) (string, error) {
	const first = "This software is available as a choice of the following licenses."
	start := strings.LastIndex(source, first)
	if start < 0 {
		return "", errors.New("miniaudio license not found")
	}
	end := strings.Index(source[start:], "*/")
	if end < 0 {
		return "", errors.New("miniaudio license end not found")
	}
	return source[start : start+end], nil
}
