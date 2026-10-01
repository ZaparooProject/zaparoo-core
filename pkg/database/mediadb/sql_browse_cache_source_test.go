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

package mediadb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceCacheAncestorDirs(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"source://", "source://abc/", "source://abc/NES/", "source://abc/NES/sub/"},
		sourceCacheAncestorDirs("source://abc/NES/sub/game.nes"),
		"a deeply nested source file walks every real ancestor, rooted at the scheme bucket")
	assert.Equal(t,
		[]string{"source://", "source://abc/"},
		sourceCacheAncestorDirs("source://abc/game.nes"),
		"a root-level source file's only real ancestor is the id root itself")
}

func TestBrowseCacheDirParentAndNameForSourcePaths(t *testing.T) {
	t.Parallel()

	parent, name, isVirtual := browseCacheDirParentAndName("source://")
	assert.Equal(t, "/", parent)
	assert.Equal(t, "source://", name)
	assert.True(t, isVirtual, "the bare scheme bucket is still the one virtual node, same as before")

	parent, name, isVirtual = browseCacheDirParentAndName("source://abc/")
	assert.Equal(t, "source://", parent)
	assert.Equal(t, "abc", name)
	assert.False(t, isVirtual, "a real id root is a browsable directory, not a virtual marker")

	parent, name, isVirtual = browseCacheDirParentAndName("source://abc/NES/")
	assert.Equal(t, "source://abc/", parent)
	assert.Equal(t, "NES", name)
	assert.False(t, isVirtual)

	parent, name, isVirtual = browseCacheDirParentAndName("source://abc/NES/sub/")
	assert.Equal(t, "source://abc/NES/", parent)
	assert.Equal(t, "sub", name)
	assert.False(t, isVirtual)
}

// A non-source virtual scheme has no real hierarchy and must keep behaving
// exactly as before: one node under "/", nothing deeper.
func TestBrowseCacheDirParentAndNameOtherVirtualSchemesStayFlat(t *testing.T) {
	t.Parallel()

	parent, name, isVirtual := browseCacheDirParentAndName("android://")
	assert.Equal(t, "/", parent)
	assert.Equal(t, "android://", name)
	assert.True(t, isVirtual)

	parent, name, isVirtual = browseCacheDirParentAndName("scummvm://")
	assert.Equal(t, "/", parent)
	assert.Equal(t, "scummvm://", name)
	assert.True(t, isVirtual)
}

func TestCountPairsForNestedSourcePath(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	pairs := builder.countPairsForPath("source://abc/NES/sub/game.nes")

	got := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		got = append(got, pair.parent.path+" -> "+pair.child.path)
	}
	assert.ElementsMatch(t, []string{
		"/ -> source://",
		"source:// -> source://abc/",
		"source://abc/ -> source://abc/NES/",
		"source://abc/NES/ -> source://abc/NES/sub/",
		"source://abc/NES/sub/ -> source://abc/NES/sub/",
	}, got, "every real ancestor link plus the leaf's own direct-file self-pair, and no scheme-bucket self-pair")
}

func TestCountPairsForFlatVirtualSchemeKeepsItsSelfPair(t *testing.T) {
	t.Parallel()

	builder := newBrowseCacheBuilder()
	builder.ensureDir("/")
	pairs := builder.countPairsForPath("android://com.example.game:variant/Name")

	require.Len(t, pairs, 2)
	assert.Equal(t, "/", pairs[0].parent.path)
	assert.Equal(t, "android://", pairs[0].child.path)
	assert.Equal(t, "android://", pairs[1].parent.path)
	assert.Equal(t, "android://", pairs[1].child.path,
		"a flat scheme keeps its self-pair: every app's direct parent really is the bare scheme")
}
