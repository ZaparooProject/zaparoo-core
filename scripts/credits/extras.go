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

	wuffsMIT, err := read("pkg/helpers/wuffspng/LICENSE-MIT")
	if err != nil {
		return nil, err
	}
	wuffsApache, err := read("pkg/helpers/wuffspng/LICENSE-APACHE")
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "Wuffs", Version: "0.4.0-alpha.12", URL: "https://github.com/google/wuffs",
			License: "Apache-2.0 OR MIT",
			Note: "Its PNG decoder is vendored in pkg/helpers/wuffspng from release/c/wuffs-v0.4.c at " +
				"https://github.com/google/wuffs/tree/7f70dda95c77b10c087c0908790837b5e9c15713.",
		},
		files: []extraFile{{name: "LICENSE-APACHE", text: wuffsApache}, {name: "LICENSE-MIT", text: wuffsMIT}},
	})

	drLibs, err := read("pkg/audio/nativedec/third_party/LICENSE-dr_libs")
	if err != nil {
		return nil, err
	}
	for _, lib := range []struct{ name, version, file string }{
		{name: "dr_mp3", version: "0.7.4", file: "dr_mp3.h"},
		{name: "dr_flac", version: "0.13.4", file: "dr_flac.h"},
	} {
		out = append(out, extraComponent{
			component: credits.Component{
				Name: lib.name, Version: lib.version, URL: "https://github.com/mackron/dr_libs",
				License: "Unlicense OR MIT-0",
				Note: "Vendored in pkg/audio/nativedec/third_party from " + lib.file + " at " +
					"https://github.com/mackron/dr_libs/tree/35b5c392cc15456736c440a68dbe073178bbffe0.",
			},
			files: []extraFile{{name: "LICENSE", text: drLibs}},
		})
	}
	stbVorbis, err := read("pkg/audio/nativedec/third_party/LICENSE-stb")
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "stb_vorbis", Version: "1.22", URL: "https://github.com/nothings/stb",
			License: "MIT OR Unlicense",
			Note: "Vendored in pkg/audio/nativedec/third_party from stb_vorbis.c at " +
				"https://github.com/nothings/stb/tree/d14d46f6b44456eb88beac58cba833a4135d4c89.",
		},
		files: []extraFile{{name: "LICENSE", text: stbVorbis}},
	})

	for i := range vendoredAudioLibraries {
		lib := &vendoredAudioLibraries[i]
		files := make([]extraFile, 0, len(lib.licenses))
		for _, lic := range lib.licenses {
			text, readErr := read(lic.path)
			if readErr != nil {
				return nil, readErr
			}
			files = append(files, extraFile{name: lic.name, text: text})
		}
		out = append(out, extraComponent{component: lib.component, files: files})
	}

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

	gamedatabase, err := read("pkg/database/gamedatabase/LICENSE")
	if err != nil {
		return nil, err
	}
	out = append(out, extraComponent{
		component: credits.Component{
			Name: "GameDataBase", URL: "https://github.com/PigSaint/GameDataBase", License: "CC-BY-4.0",
			Note: "Game metadata by PigSaint, embedded with modifications described in the attribution.",
		},
		files: []extraFile{{name: "Attribution", text: gamedatabase}},
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

type vendoredLicense struct{ name, path string }

// vendoredAudioLibraries are the C and C++ libraries compiled into the
// native audio decoders.
var vendoredAudioLibraries = []struct {
	component credits.Component
	licenses  []vendoredLicense
}{
	{
		component: credits.Component{
			Name: "FAAD2", Version: "2.11.4", URL: "https://github.com/knik0/faad2",
			License: "GPL-2.0-or-later",
			Note: "libfaad is vendored in pkg/audio/nativedec/third_party/faad2 from " +
				"https://github.com/knik0/faad2/tree/e8e76f0a44db45aed773a3f62fe35a63c867a738.",
		},
		licenses: []vendoredLicense{{name: "COPYING", path: "pkg/audio/nativedec/third_party/faad2/COPYING"}},
	},
	{
		component: credits.Component{
			Name: "libADLMIDI", Version: "1.6.2.1", URL: "https://github.com/Wohlstand/libADLMIDI",
			License: "LGPL-3.0-or-later AND GPL-2.0-or-later AND MIT AND BSL-1.0",
			Note: "Vendored in pkg/audio/midi/third_party/libadlmidi from " +
				"https://github.com/Wohlstand/libADLMIDI/tree/99c5e03a5cf9b4fdf35b53efddb853c96320d50f, " +
				"built with the DOSBox OPL3 emulator only. Changes are marked \"Zaparoo\": " +
				"dbopl.cpp computes its offset tables with offsetof, and the sequencer " +
				"bounds-checks note and controller numbers and rejects a zero division.",
		},
		licenses: []vendoredLicense{
			{name: "LICENSE", path: "pkg/audio/midi/third_party/libadlmidi/LICENSE"},
			{name: "LICENSE.GPL-3", path: "pkg/audio/midi/third_party/libadlmidi/LICENSE.GPL-3.txt"},
			{name: "LICENSE.DOSBox", path: "pkg/audio/midi/third_party/libadlmidi/src/chips/dosbox/LICENSE"},
			{name: "LICENSE.midiseq", path: "pkg/audio/midi/third_party/libadlmidi/src/midiseq/LICENSE.txt"},
			{name: "LICENSE.models", path: "pkg/audio/midi/third_party/libadlmidi/src/models/LICENSE.txt"},
			{name: "LICENSE.wopl", path: "pkg/audio/midi/third_party/libadlmidi/src/wopl/LICENSE.txt"},
			{name: "LICENSE.structures", path: "pkg/audio/midi/third_party/libadlmidi/src/structures/LICENSE"},
		},
	},
	{
		component: credits.Component{
			Name: "DMXOPL3", URL: "https://github.com/sneakernets/DMXOPL", License: "MIT",
			Note: "The GS OPL3 instrument bank embedded in the MIDI player, " +
				"from pkg/audio/midi/third_party/libadlmidi/banks.",
		},
		licenses: []vendoredLicense{
			{name: "LICENSE", path: "pkg/audio/midi/third_party/libadlmidi/banks/LICENSE-DMXOPL.txt"},
		},
	},
	{
		component: credits.Component{
			Name: "TinySoundFont", URL: "https://github.com/schellingb/TinySoundFont", License: "MIT",
			Note: "tsf.h and tml.h are vendored in pkg/audio/midi/third_party/tinysoundfont from " +
				"https://github.com/schellingb/TinySoundFont/tree/853a0a171759f1ddba0de1442133a75912bbeffa.",
		},
		licenses: []vendoredLicense{{name: "LICENSE", path: "pkg/audio/midi/third_party/tinysoundfont/LICENSE"}},
	},
	{
		component: credits.Component{
			Name: "libvgm sound cores", URL: "https://github.com/ValleyBell/libvgm",
			License: "BSD-3-Clause AND GPL-2.0-or-later AND LGPL-2.1-or-later AND MIT",
			Note: "Chip emulation cores vendored in pkg/audio/vgm/third_party/libvgm from " +
				"https://github.com/ValleyBell/libvgm/tree/c5c03a2c2514d38d2ab7b7e8dbd021af0b8292cb; " +
				"pkg/audio/vgm/third_party/README lists the license of each file.",
		},
		licenses: []vendoredLicense{
			{name: "LICENSE.BSD-3-Clause", path: "pkg/audio/vgm/third_party/LICENSE.BSD-3-Clause.txt"},
			{name: "LICENSE.GPL-2.0", path: "pkg/audio/vgm/third_party/LICENSE.GPL-2.0.txt"},
			{name: "LICENSE.LGPL-2.1", path: "pkg/audio/vgm/third_party/LICENSE.LGPL-2.1.txt"},
			{name: "LICENSE.MIT-mikey", path: "pkg/audio/vgm/third_party/LICENSE.MIT-mikey.txt"},
		},
	},
	{
		component: credits.Component{
			Name: "emu2413", Version: "1.6.4", URL: "https://github.com/digital-sound-antiques/emu2413",
			License: "MIT",
			Note: "Vendored in pkg/audio/vgm/third_party/emu2413 from " +
				"https://github.com/digital-sound-antiques/emu2413/tree/a1d5120903b75f93d3eb7690e34a29c0b4add903.",
		},
		licenses: []vendoredLicense{{name: "LICENSE", path: "pkg/audio/vgm/third_party/emu2413/LICENSE"}},
	},
	{
		component: credits.Component{
			Name: "Game_Music_Emu", Version: "0.6.5", URL: "https://github.com/libgme/game-music-emu",
			License: "LGPL-2.1-or-later AND MIT",
			Note: "Vendored and patched in pkg/audio/gme/third_party/gme from " +
				"https://github.com/libgme/game-music-emu/tree/8238afd9503a59ffcec0e2685584d5ce819ad7c6, " +
				"including its copy of emu2413 (MIT).",
		},
		licenses: []vendoredLicense{
			{name: "license.txt", path: "pkg/audio/gme/third_party/gme/license.txt"},
			{name: "LICENSE.emu2413", path: "pkg/audio/gme/third_party/gme/gme/ext/LICENSE"},
		},
	},
}
