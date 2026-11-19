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

package installer

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

// FuzzFindInstallDir checks the one invariant that matters here: whatever
// download URL arrives, the file lands inside the system's own directory or
// the call is refused. The URL comes from a card or from a ZapLink body, so
// the last path segment is entirely chosen by whoever served it, and it is the
// only thing deciding where the device writes.
func FuzzFindInstallDir(f *testing.F) {
	seeds := []string{
		"https://cdn.example.com/media/games/jaguar/Downfall.rom",
		"https://cdn.example.com/media/Full%20Circle%20-%20Rocketeer%20(Promo).rom",
		"http://example.com/roms/%252e%252e%252f%252e%252e%252fScripts%252fpwn.sh",
		"http://example.com/roms/%2e%2e%2f%2e%2e%2fpwn.sh",
		`http://example.com/roms/..%5C..%5Cpwn.exe`,
		"http://example.com/roms/..",
		"http://example.com/roms/.",
		"http://example.com/roms/",
		"http://example.com/",
		"smb://nas/share/game.sfc",
		"not-a-url/game.rom",
		"http://example.com/%00.rom",
		"http://example.com/" + string(rune(0x202e)) + "exe.mor",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, rawURL string) {
		dataDir := t.TempDir()
		pl := setupMockPlatformWithTempDir(t, dataDir)
		systemDir := filepath.Join(dataDir, config.MediaDir, "NES")

		got, err := findInstallDir(&config.Instance{}, pl, "nes", namesFromURL(rawURL, ""))
		if err != nil {
			return
		}
		if !isWithinDir(systemDir, got) {
			t.Fatalf("install path escaped %s: url=%q path=%q", systemDir, rawURL, got)
		}
	})
}

// FuzzExtractZip holds extraction to its folder whatever the archive holds:
// the zip comes from a media server, so every entry name and header is
// untrusted.
func FuzzExtractZip(f *testing.F) {
	f.Add(buildZip(f, []zipEntry{{name: "Game.cue", body: "cue"}, {name: "Disc 1/Track.bin", body: "bin"}}))
	f.Add(buildZip(f, []zipEntry{{name: "../escape", body: "x"}}))
	f.Add(buildZip(f, []zipEntry{{name: `..\escape`, body: "x"}}))
	f.Add(buildZip(f, []zipEntry{{name: "/abs", body: "x"}}))
	f.Add(buildZip(f, []zipEntry{{name: "link", body: "/etc", mode: fs.ModeSymlink | 0o777}}))
	f.Add([]byte("PK\x03\x04"))

	f.Fuzz(func(t *testing.T, data []byte) {
		base := t.TempDir()
		zipPath := filepath.Join(base, "in.zip")
		if err := os.WriteFile(zipPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(base, "out")
		_ = extractZip(context.Background(), zipPath, dest, 1<<20)

		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == base || p == zipPath || p == dest {
				return nil
			}
			if !isWithinDir(dest, p) {
				t.Fatalf("extraction wrote outside its folder: %s", p)
			}
			if d.Type()&fs.ModeSymlink != 0 {
				t.Fatalf("extraction created a link: %s", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
