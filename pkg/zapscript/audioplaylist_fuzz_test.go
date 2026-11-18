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

package zapscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/ZaparooProject/go-zapscript"
)

// FuzzReadPlaylistFile reads arbitrary .m3u and .pls content. Every item must
// be bounded, free of control characters, and an m3u entry must parse as a
// launch of exactly one location.
func FuzzReadPlaylistFile(f *testing.F) {
	f.Add("#EXTM3U\n#EXTINF:-1,Radio\nhttp://radio.example/live?sid=1\n", false)
	f.Add("\xef\xbb\xbfsong.mp3\r\n../x.mp3\r\nself.m3u\r\n", false)
	f.Add("#EXT-X-VERSION:3\nseg.ts\n", false)
	f.Add("[playlist]\nFile1=a.mp3?launcher=x\nTitle1=A\nFile2=self.pls\n", true)
	f.Add("[playlist]\nFile1=\x00\nFile2=://\n", true)

	f.Fuzz(func(t *testing.T, content string, pls bool) {
		dir := t.TempDir()
		name := "self.m3u"
		if pls {
			name = "self.pls"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		items, err := readPlaylistFile(nil, nil, path)
		if err != nil {
			return
		}
		if len(items) == 0 || len(items) > maxPlaylistItems {
			t.Fatalf("got %d items", len(items))
		}
		for _, item := range items {
			if strings.ContainsFunc(item.ZapScript, isDisallowedControl) {
				t.Fatalf("item has control characters: %q", item.ZapScript)
			}
			if pls || strings.ContainsFunc(item.Name, unicode.IsControl) {
				continue
			}
			script, err := zapscript.NewParser(item.ZapScript).ParseScript()
			if err != nil || len(script.Cmds) != 1 || script.Cmds[0].Name != zapscript.ZapScriptCmdLaunch {
				t.Fatalf("m3u item %q does not parse as one launch: %v", item.ZapScript, err)
			}
		}
	})
}
