//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appendVDFString(data []byte, key, value string) []byte {
	data = append(data, 0x01)
	data = append(data, key...)
	data = append(data, 0x00)
	data = append(data, value...)
	return append(data, 0x00)
}

func shortcutFixture(appID uint32, executable string) []byte {
	data := []byte{0x00}
	data = append(data, "shortcuts"...)
	data = append(data, 0x00, 0x00, '0', 0x00, 0x02)
	data = append(data, "appid"...)
	data = append(data, 0x00)
	appIDBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(appIDBytes, appID)
	data = append(data, appIDBytes...)
	data = appendVDFString(data, "appname", "Zaparoo")
	data = appendVDFString(data, "exe", `"`+executable+`"`)
	data = appendVDFString(data, "startdir", filepath.Dir(executable))
	return append(data, 0x08, 0x08, 0x08)
}

func TestFindShortcutIDs(t *testing.T) {
	t.Parallel()

	steamDir := t.TempDir()
	runtimePath := filepath.Join(t.TempDir(), "Zaparoo Runtime", runtimeExecutableName)
	path := filepath.Join(steamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, shortcutFixture(42, runtimePath), 0o600))

	ids, err := findShortcutIDs(steamDir, runtimePath)
	require.NoError(t, err)
	require.Equal(t, []uint64{shortcutBigPictureID(42)}, ids)
	assert.Equal(t, "steam://rungameid/180422180864", shortcutURL(ids[0]))
}

func TestFindShortcutIDsNotFound(t *testing.T) {
	t.Parallel()

	ids, err := findShortcutIDs(t.TempDir(), filepath.Join(t.TempDir(), runtimeExecutableName))
	require.ErrorIs(t, err, errShortcutNotFound)
	assert.Empty(t, ids)
}
