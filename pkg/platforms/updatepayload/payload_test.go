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

package updatepayload

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelativeArchivePath(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		member string
		want   string
		ok     bool
	}{
		{name: "nested", member: "scripts/services/zaparoo_service", want: "services/zaparoo_service", ok: true},
		{name: "root file", member: "scripts/zaparoo_write_game.sh", want: "zaparoo_write_game.sh", ok: true},
		{name: "root directory", member: "scripts"},
		{name: "other root", member: "other/file"},
		{name: "absolute", member: "/scripts/file"},
		{name: "parent", member: "scripts/../file"},
		{name: "nested parent", member: "scripts/a/../../file"},
		{name: "dot", member: "scripts/./file"},
		{name: "empty component", member: "scripts//file"},
		{name: "backslash", member: `scripts\..\file`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := RelativeArchivePath("scripts", tt.member)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchArchiveFile(t *testing.T) {
	t.Parallel()

	files := []File{{
		ArchivePath: "scripts/ports/Zaparoo.sh",
		InstallPath: "../roms/ports/Zaparoo.sh",
		Mode:        0o755,
	}}
	file, ok := MatchArchiveFile(files, "scripts/ports/Zaparoo.sh")
	require.True(t, ok)
	assert.Equal(t, "../roms/ports/Zaparoo.sh", file.InstallPath)
	assert.Equal(t, 0o755, int(file.Mode))

	_, ok = MatchArchiveFile(files, "scripts/unconfigured.sh")
	assert.False(t, ok)
	assert.True(t, InvalidArchiveMember(files, "scripts/../outside"))
	assert.False(t, InvalidArchiveMember(files, "scripts/unconfigured.sh"))
}

func TestResolveInstallPath(t *testing.T) {
	t.Parallel()

	binary := filepath.Join("userdata", "system", "zaparoo")
	got, ok := ResolveInstallPath(binary, "../roms/ports/Zaparoo.sh")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join("userdata", "roms", "ports", "Zaparoo.sh"), got)

	_, ok = ResolveInstallPath(binary, "/outside")
	assert.False(t, ok)
}

func FuzzPayloadMemberPath(f *testing.F) {
	for _, seed := range []string{
		"scripts/file",
		"scripts/a/b",
		"scripts/../file",
		`scripts\..\file`,
		"/scripts/file",
		"scripts//file",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, member string) {
		rel, ok := RelativeArchivePath("scripts", member)
		if !ok {
			return
		}
		assert.NotEmpty(t, rel)
		assert.NotContains(t, rel, `\`)
		assert.NotContains(t, rel, "//")
		assert.NotEqual(t, ".", rel)
		assert.NotEqual(t, "..", rel)
	})
}
