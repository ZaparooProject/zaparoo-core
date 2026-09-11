//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

func launcherDefinitions(home string) []platforms.BackupDefinition {
	definitions := make([]platforms.BackupDefinition, 0, 10)
	for _, rel := range []string{
		filepath.Join(".kodi", "userdata"),
		filepath.Join(".var", "app", "tv.kodi.Kodi", "data", "userdata"),
		filepath.Join(".var", "app", "tv.kodi.Kodi", "data", "kodi", "userdata"),
		filepath.Join(".var", "app", "tv.kodi.Kodi", ".kodi", "userdata"),
	} {
		definitions = append(definitions, definition(
			filepath.Join(home, rel), rel, backupCategorySettings,
			[]platforms.BackupPattern{{All: true}},
			[]platforms.BackupPattern{
				{Contains: "thumbnails/"},
				{Glob: filepath.Join("Database", "Textures*.db")},
				{Contains: "cache/"},
				{Contains: "temp/"},
				{Contains: "logs/"},
				{Contains: "themes/"},
			},
		))
	}
	for _, rel := range []string{
		filepath.Join(".config", "Moonlight Game Streaming Project"),
		filepath.Join(
			".var", "app", "com.moonlight_stream.Moonlight", "config",
			"Moonlight Game Streaming Project",
		),
	} {
		definitions = append(definitions, nonRecursiveDefinition(
			filepath.Join(home, rel), rel, backupCategorySettings,
			[]platforms.BackupPattern{{Glob: "Moonlight.conf"}},
		))
	}
	for _, rel := range []string{
		filepath.Join(".config", "bottles"),
		filepath.Join(".var", "app", "com.usebottles.bottles", "config", "bottles"),
	} {
		definitions = append(definitions, definition(
			filepath.Join(home, rel), rel, backupCategorySettings,
			[]platforms.BackupPattern{{All: true}},
			[]platforms.BackupPattern{{Contains: "cache/"}, {Contains: "logs/"}, {Contains: "downloads/"}},
		))
	}
	return definitions
}

func faugusMetadataDefinitions(home string) []platforms.BackupDefinition {
	paths := []string{
		filepath.Join(".config", "faugus-launcher"),
		filepath.Join(".local", "share", "faugus-launcher"),
		filepath.Join(".var", "app", "io.github.Faugus.faugus-launcher", "config", "faugus-launcher"),
		filepath.Join(".var", "app", "io.github.Faugus.faugus-launcher", "data", "faugus-launcher"),
	}
	definitions := make([]platforms.BackupDefinition, 0, len(paths))
	for _, rel := range paths {
		definitions = append(definitions, definition(
			filepath.Join(home, rel), rel, backupCategorySettings,
			[]platforms.BackupPattern{{All: true}},
			[]platforms.BackupPattern{
				{Contains: "cache/"},
				{Contains: "logs/"},
				{Contains: "downloads/"},
				{Contains: "screenshots/"},
			},
		))
	}
	return definitions
}
