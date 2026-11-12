//go:build linux && !android

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

package mister

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainArcadeRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"/media/fat/_Arcade/_Arcade Patches/_Restorations/x.mra", "/media/fat/_Arcade"},
		{"/media/usb0/_Arcade/_Arcade Patches/_Restorations/x.mra", "/media/usb0/_Arcade"},
		{"/media/usb0/games/_Arcade/x.mra", "/media/usb0/games/_Arcade"},
		{"/media/fat/_Arcade/x.mra", "/media/fat/_Arcade"},
		{"/media/fat/games/x.mra", "/media/fat"},
		{"/media/fat/_x.mra", "/media/fat"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, mainArcadeRoot(tt.path), tt.path)
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestMainFindArcadeRBF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, n := range []string{
		"jtcps2_20260101.rbf", "jtcps2_20260301.rbf", "Arcade-Other.rbf",
		"jtcps2x.rbf", "jtcps2_notrbf.txt",
	} {
		touch(t, filepath.Join(dir, n))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "jtcps2_dir.rbf"), 0o750))

	got, ok := mainFindArcadeRBF(dir, "jtcps2")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "jtcps2_20260301.rbf"), got)

	got, ok = mainFindArcadeRBF(dir, "other")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "Arcade-Other.rbf"), got)

	_, ok = mainFindArcadeRBF(dir, "jtcps")
	assert.False(t, ok)
	_, ok = mainFindArcadeRBF(filepath.Join(dir, "missing"), "jtcps2")
	assert.False(t, ok)
}

func TestResolveArcadeMRAPath(t *testing.T) {
	t.Parallel()

	const rel = "_Arcade/_Arcade Patches/_Restorations/sfa3.mra"

	t.Run("core beside MRA keeps path", func(t *testing.T) {
		t.Parallel()
		usb, fat := t.TempDir(), t.TempDir()
		mra := filepath.Join(usb, rel)
		touch(t, mra)
		touch(t, filepath.Join(usb, "_Arcade", "cores", "jtcps2_1.rbf"))
		got, err := resolveArcadeMRAPath(mra, "jtcps2", []string{usb, fat})
		require.NoError(t, err)
		assert.Equal(t, mra, got)
	})

	t.Run("missing core uses copy on other root", func(t *testing.T) {
		t.Parallel()
		usb, fat := t.TempDir(), t.TempDir()
		mra := filepath.Join(usb, rel)
		touch(t, mra)
		touch(t, filepath.Join(fat, rel))
		touch(t, filepath.Join(fat, "_Arcade", "cores", "jtcps2_1.rbf"))
		got, err := resolveArcadeMRAPath(mra, "jtcps2", []string{usb, fat})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(fat, rel), got)
	})

	t.Run("copy without core is skipped", func(t *testing.T) {
		t.Parallel()
		usb, fat := t.TempDir(), t.TempDir()
		mra := filepath.Join(usb, rel)
		touch(t, mra)
		touch(t, filepath.Join(fat, rel))
		_, err := resolveArcadeMRAPath(mra, "jtcps2", []string{usb, fat})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "jtcps2")
	})

	t.Run("root without a copy is skipped", func(t *testing.T) {
		t.Parallel()
		usb, empty, fat := t.TempDir(), t.TempDir(), t.TempDir()
		mra := filepath.Join(usb, rel)
		touch(t, mra)
		touch(t, filepath.Join(fat, rel))
		touch(t, filepath.Join(fat, "_Arcade", "cores", "jtcps2_1.rbf"))
		got, err := resolveArcadeMRAPath(mra, "jtcps2", []string{empty, fat})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(fat, rel), got)
	})

	t.Run("no rbf in MRA keeps path", func(t *testing.T) {
		t.Parallel()
		got, err := resolveArcadeMRAPath("/x/_Arcade/a.mra", "", nil)
		require.NoError(t, err)
		assert.Equal(t, "/x/_Arcade/a.mra", got)
	})
}

func TestLaunchArcade_MissingCoreFailsLaunch(t *testing.T) {
	t.Parallel()

	mra := filepath.Join(t.TempDir(), "_Arcade", "game.mra")
	require.NoError(t, os.MkdirAll(filepath.Dir(mra), 0o750))
	require.NoError(t, os.WriteFile(mra, []byte(
		`<misterromdescription><setname>game</setname><rbf>zaparoo_missing_core</rbf></misterromdescription>`,
	), 0o600))

	launcher := launchArcade(NewPlatform(), "Arcade")
	proc, err := launcher(&config.Instance{}, mra, nil)
	require.Error(t, err)
	assert.Nil(t, proc)
	assert.Contains(t, err.Error(), "zaparoo_missing_core")
	assert.Contains(t, err.Error(), filepath.Join(filepath.Dir(mra), "cores"))
}
