//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatform_Identity(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	assert.Equal(t, platformids.Batocera, p.ID())
	settings := p.Settings()
	assert.Equal(t, "/userdata/system/.local/share/zaparoo", settings.DataDir)
	assert.Equal(t, "/userdata/system/.config/zaparoo", settings.ConfigDir)
	assert.Equal(t, "/userdata/system/.local/share/zaparoo/logs", settings.LogDir)
	assert.True(t, settings.DisableZapScriptInTUI)

	opts := Options()
	assert.Equal(t, []string{"/userdata/system/configs/emulationstation"}, opts.ESConfigDirs)
	assert.Equal(t, "/userdata/roms", opts.DefaultROMRoot)
	assert.NotNil(t, opts.ExitGame, "Batocera exits games through hotkeygen first")
	assert.Nil(t, opts.KillGame, "Batocera falls back to EmulationStation's /emukill")
	assert.NotNil(t, opts.Controls)
	assert.NotNil(t, opts.ControlNames)
}

func TestRootDirs_AlwaysIncludesBatoceraROMs(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, NewPlatform().RootDirs(cfg), "/userdata/roms")
}

func TestSettings_UpdatePayload(t *testing.T) {
	t.Parallel()

	files := NewPlatform().UpdatePayload()
	require.Len(t, files, 7)
	byArchivePath := make(map[string]updatepayload.File, len(files))
	for _, file := range files {
		byArchivePath[file.ArchivePath] = file
	}

	tests := []struct {
		name string
		want updatepayload.File
	}{
		{
			name: "game selected hook",
			want: updatepayload.File{
				SourcePath: filepath.Join("cmd", "batocera", "scripts", "configs", "emulationstation", "scripts",
					"game-selected", "zaparoo_game_select.sh"),
				ArchivePath: "scripts/configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
				InstallPath: "configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
				Mode:        0o755,
			},
		},
		{
			name: "multimedia keys",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "configs", "multimedia_keys.conf"),
				ArchivePath: "scripts/configs/multimedia_keys.conf",
				InstallPath: "configs/multimedia_keys.conf",
				Mode:        0o644,
			},
		},
		{
			name: "content downloader image",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "content_downloader.png"),
				ArchivePath: "scripts/content_downloader.png",
				InstallPath: "content_downloader.png",
				Mode:        0o644,
			},
		},
		{
			name: "ports launcher",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "ports", "Zaparoo.sh"),
				ArchivePath: "scripts/ports/Zaparoo.sh",
				InstallPath: "../roms/ports/Zaparoo.sh",
				Mode:        0o755,
			},
		},
		{
			name: "service",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "services", "zaparoo_service"),
				ArchivePath: "scripts/services/zaparoo_service",
				InstallPath: "services/zaparoo_service",
				Mode:        0o755,
			},
		},
		{
			name: "wrapper",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_wrapper.sh"),
				ArchivePath: "scripts/zaparoo_wrapper.sh",
				InstallPath: "zaparoo_wrapper.sh",
				Mode:        0o755,
			},
		},
		{
			name: "write game",
			want: updatepayload.File{
				SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_write_game.sh"),
				ArchivePath: "scripts/zaparoo_write_game.sh",
				InstallPath: "zaparoo_write_game.sh",
				Mode:        0o755,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := byArchivePath[tt.want.ArchivePath]
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Batocera, 2026-10-26: its EmulationStation has no doom system, so a WAD
// indexed from a doom folder could not be started; and prboom.wad, PrBoom's
// own data, was indexed as the game Doom.
func TestLaunchersDoom(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	p := NewPlatform()
	var doom []platforms.Launcher
	for _, l := range p.Launchers(cfg) {
		assert.NotContains(t, l.Folders, "doom", "Batocera has no doom system")
		if l.SystemID == systemdefs.SystemDoom {
			doom = append(doom, l)
		}
	}
	require.NotEmpty(t, doom)
	for _, l := range doom {
		assert.Contains(t, l.ScanExcludes, "prboom.wad", l.Folders)
	}
}
