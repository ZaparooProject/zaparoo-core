//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package knulli

import (
	"testing"
	"time"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatform_Identity(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	assert.Equal(t, platformids.Knulli, p.ID())
	settings := p.Settings()
	assert.Equal(t, "/userdata/system/.local/share/zaparoo", settings.DataDir)
	assert.Equal(t, "/userdata/system/.config/zaparoo", settings.ConfigDir)
	assert.Equal(t, "/userdata/system/.local/share/zaparoo/logs", settings.LogDir)
	assert.False(t, p.ManagedByPackageManager())
	assert.Empty(t, p.UpdatePayload())

	opts := Options()
	assert.Equal(t, []string{"/usr/share/emulationstation", "/userdata/system/configs/emulationstation"},
		opts.ESConfigDirs)
	assert.Equal(t, "/userdata/roms", opts.DefaultROMRoot)
	assert.Nil(t, opts.ExitGame, "Knulli has no hotkeygen; stops go to /emukill")
	assert.Nil(t, opts.KillGame, "Knulli stops through EmulationStation's /emukill")
	assert.Nil(t, opts.Controls)
	assert.Nil(t, opts.ControlNames)
	assert.True(t, opts.OnlyESSystems)
	assert.Equal(t, "megacd", opts.SystemAliases["segacd"])
	assert.NotNil(t, opts.HasKodi)
	assert.Equal(t, 2*time.Second, opts.KillRetryDelay)
}

func TestRootDirs_AlwaysIncludesKnulliROMs(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, NewPlatform().RootDirs(cfg), "/userdata/roms")
}
