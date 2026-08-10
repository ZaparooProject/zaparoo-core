//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runtimeShortcutFixture(appID uint32, executable string) []byte {
	return fixtures.BuildShortcutsVDF([]fixtures.TestShortcut{{
		AppID: appID, AppName: "Zaparoo", Exe: `"` + executable + `"`, StartDir: filepath.Dir(executable),
	}})
}

func TestFindShortcutIDs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	steamDir := filepath.Join(string(filepath.Separator), "steam")
	runtimePath := filepath.Join(string(filepath.Separator), "runtime", "Zaparoo Runtime", runtimeExecutableName)
	path := filepath.Join(steamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, runtimeShortcutFixture(42, runtimePath), 0o600))

	ids, err := findShortcutIDs(fs, steamDir, runtimePath)
	require.NoError(t, err)
	require.Equal(t, []uint64{shortcutBigPictureID(42)}, ids)
	assert.Equal(t, "steam://rungameid/180422180864", shortcutURL(ids[0]))
}

func TestFindShortcutIDsNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	ids, err := findShortcutIDs(
		fs,
		filepath.Join(string(filepath.Separator), "steam"),
		filepath.Join(string(filepath.Separator), "runtime", runtimeExecutableName),
	)
	require.ErrorIs(t, err, errShortcutNotFound)
	assert.Empty(t, ids)
}
