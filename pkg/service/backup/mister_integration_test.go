//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package backup

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister"
	"github.com/stretchr/testify/assert"
)

func TestMiSTerBackupDefinitionsCollectorExcludesPrivateAndGeneratedFiles(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	writeTestFile(t, filepath.Join(rootDir, "MiSTer.ini"), "user ini\n")
	writeTestFile(t, filepath.Join(rootDir, "MiSTer_example.ini"), "example ini\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "core.cfg"), "core settings\n")
	writeTestFile(t, filepath.Join(rootDir, "config", "core_recent.cfg"), "recent\n")
	writeTestFile(t, filepath.Join(rootDir, "games", "MiSTer.ini"), "nested ini\n")
	writeTestFile(t, filepath.Join(rootDir, "games", "game.rom"), "rom\n")
	writeTestFile(t, filepath.Join(rootDir, "gamecontrollerdb_user.txt"), "root controller db\n")
	writeTestFile(t, filepath.Join(rootDir, "linux", "gamecontrollerdb_user.txt"), "linux controller db\n")

	definitions := mister.BackupDefinitions(platforms.Settings{DataDir: filepath.Join(rootDir, "zaparoo")})
	files := collectPlatformFiles(nil, definitions)
	byArchive := make(map[string]FileRef, len(files))
	for _, file := range files {
		byArchive[file.ArchivePath] = file
	}

	assert.Contains(t, byArchive, platformArchive("MiSTer.ini"))
	assert.Contains(t, byArchive, platformArchive(filepath.Join("config", "core.cfg")))
	assert.Contains(t, byArchive, platformArchive("gamecontrollerdb_user.txt"))
	assert.Contains(t, byArchive, platformArchive(filepath.Join("linux", "gamecontrollerdb_user.txt")))
	assert.NotContains(t, byArchive, platformArchive("MiSTer_example.ini"))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("config", "core_recent.cfg")))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("games", "MiSTer.ini")))
	assert.NotContains(t, byArchive, platformArchive(filepath.Join("games", "game.rom")))
}
