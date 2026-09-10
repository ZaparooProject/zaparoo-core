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
	"bytes"
	"path/filepath"
	"testing"
	"text/template"
)

func TestRenderExecPathMatchesTemplate(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"service": systemdServiceFile,
		"desktop": desktopFile,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			legacy, err := template.New(name).Parse(content)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{
				filepath.Join("", "usr", "bin", "zaparoo"),
				filepath.Join("home", "Game User", "Zaparoo Core"),
				filepath.Join("games", `quotes"' $ % & < >`, "zaparoo"),
				filepath.Join("games", "日本語", "zaparoo"), //nolint:gosmopolitan // Verify non-ASCII executable paths.
				filepath.Join("games", "{{.ExecPath}}", "zaparoo"),
				"",
			} {
				var want bytes.Buffer
				if err := legacy.Execute(&want, struct{ ExecPath string }{path}); err != nil {
					t.Fatal(err)
				}
				if got := renderExecPath(content, path); !bytes.Equal(got, want.Bytes()) {
					t.Errorf("path %q: got %q, want %q", path, got, want.Bytes())
				}
			}
		})
	}
}

func BenchmarkRenderExecPath(b *testing.B) {
	path := filepath.Join("home", "Game User", "zaparoo")
	b.ReportAllocs()
	for b.Loop() {
		_ = renderExecPath(systemdServiceFile, path)
	}
}
