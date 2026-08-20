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
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
)

func TestRoots(t *testing.T) {
	t.Parallel()

	roots := Roots(platformids.Batocera)
	assert.Equal(t, []Root{{SourceDir: "cmd/batocera/scripts", ArchiveRoot: "scripts"}}, roots)
	assert.Empty(t, Roots(platformids.Linux))

	roots[0].ArchiveRoot = "changed"
	assert.Equal(t, "scripts", Roots(platformids.Batocera)[0].ArchiveRoot)
}

func TestRelativeMember(t *testing.T) {
	t.Parallel()

	root := Root{ArchiveRoot: "scripts"}
	for _, tt := range []struct {
		name   string
		member string
		want   string
		ok     bool
	}{
		{name: "nested", member: "scripts/services/zaparoo_service", want: "services/zaparoo_service", ok: true},
		{name: "root file", member: "scripts/zaparoo_write_game.sh", want: "zaparoo_write_game.sh", ok: true},
		{name: "root directory", member: "scripts", ok: false},
		{name: "other root", member: "other/file", ok: false},
		{name: "absolute", member: "/scripts/file", ok: false},
		{name: "parent", member: "scripts/../file", ok: false},
		{name: "nested parent", member: "scripts/a/../../file", ok: false},
		{name: "dot", member: "scripts/./file", ok: false},
		{name: "empty component", member: "scripts//file", ok: false},
		{name: "backslash", member: `scripts\..\file`, ok: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := RelativeMember(root, tt.member)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIncludeSource(t *testing.T) {
	t.Parallel()

	assert.True(t, IncludeSource("services/zaparoo_service"))
	assert.False(t, IncludeSource(".DS_Store"))
	assert.False(t, IncludeSource("configs/.DS_Store"))
	assert.False(t, IncludeSource(`configs\.DS_Store`))
}

func FuzzPayloadMemberPath(f *testing.F) {
	root := Root{ArchiveRoot: "scripts"}
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
		rel, ok := RelativeMember(root, member)
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
