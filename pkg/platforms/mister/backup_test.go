//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package mister

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupDefinitions(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	definitions := BackupDefinitions(platforms.Settings{DataDir: filepath.Join(rootDir, "zaparoo")})
	require.Len(t, definitions, 9)

	assert.Equal(t, rootDir, definitions[0].SourceRoot)
	assert.Equal(t, "settings", definitions[0].Category)
	assert.True(t, definitions[0].NonRecursive)
	assert.Contains(t, definitions[0].Include, platforms.BackupPattern{Glob: "MiSTer.ini"})
	assert.Contains(t, definitions[0].Exclude, platforms.BackupPattern{Glob: "MiSTer_example.ini"})

	assert.Equal(t, filepath.Join(rootDir, "config"), definitions[1].SourceRoot)
	assert.Equal(t, "config", definitions[1].RestoreRoot)
	assert.Contains(t, definitions[1].Include, platforms.BackupPattern{Glob: "*.cfg"})
	assert.Contains(t, definitions[1].Exclude, platforms.BackupPattern{Contains: "_recent"})

	assert.Equal(t, rootDir, definitions[2].SourceRoot)
	assert.True(t, definitions[2].NonRecursive)
	assert.Equal(t, filepath.Join(rootDir, "linux"), definitions[3].SourceRoot)
	assert.True(t, definitions[3].NonRecursive)

	assert.Equal(t, filepath.Join(rootDir, "config", "inputs"), definitions[4].SourceRoot)
	assert.Equal(t, filepath.Join("config", "inputs"), definitions[4].RestoreRoot)
	assert.Equal(t, "inputs", definitions[4].Category)

	profileRoot := filepath.Join(rootDir, "zaparoo", "profiles")
	assert.Equal(t, profileRoot, definitions[5].SourceRoot)
	assert.Equal(t, "saves", definitions[5].Category)
	assert.Contains(t, definitions[5].Include, platforms.BackupPattern{Contains: "/saves/"})
	assert.Equal(t, profileRoot, definitions[6].SourceRoot)
	assert.Equal(t, "savestates", definitions[6].Category)
	assert.Contains(t, definitions[6].Include, platforms.BackupPattern{Contains: "/savestates/"})
	assert.Equal(t, filepath.Join(rootDir, "saves"), definitions[7].SourceRoot)
	assert.Equal(t, "saves", definitions[7].Category)
	assert.Equal(t, filepath.Join(rootDir, "savestates"), definitions[8].SourceRoot)
	assert.Equal(t, "savestates", definitions[8].Category)
	assert.Equal(t, []platforms.BackupPattern{{All: true}}, definitions[8].Include)
}
