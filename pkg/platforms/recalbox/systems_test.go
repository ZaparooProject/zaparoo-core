//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func systemNames(systems []rbSystem) []string {
	names := make([]string, 0, len(systems))
	for i := range systems {
		names = append(names, systems[i].Name)
	}
	return names
}

func TestLoadSystems_TemplateOnly(t *testing.T) {
	t.Parallel()
	systems, err := loadSystems(newTestFs(t))
	require.NoError(t, err)
	assert.Equal(t, []string{"snes", "segacd", "2048", "outrun", "240ptestsuite", "nes"}, systemNames(systems),
		"favorites must be skipped")
}

func TestLoadSystems_UserReplacesByUUIDAndAppends(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, userSystemList, testUserSystems)

	systems, err := loadSystems(fs)
	require.NoError(t, err)
	assert.Equal(t, []string{"snes", "segacd", "2048", "outrun", "240ptestsuite", "nes", "mame"}, systemNames(systems))
	assert.Equal(t, "Super Nintendo (user)", systems[0].FullName)
	assert.Equal(t, ".sfc", systems[0].Descriptor.Extensions)
}

func TestLoadSystems_Errors(t *testing.T) {
	t.Parallel()

	_, err := loadSystems(afero.NewMemMapFs())
	require.Error(t, err, "missing template")

	fs := newTestFs(t)
	writeFile(t, fs, userSystemList, "<systemList><system")
	_, err = loadSystems(fs)
	require.Error(t, err, "broken user list")
}

func TestSystemFolder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"%ROOT%/snes", "snes"},
		{"%ROOT%/ports/Cave Story", "ports/Cave Story"},
		{"%ROOT%/snes/", "snes"},
		{"/media/usb/arcade/", "/media/usb/arcade"},
		{"%ROOT%", ""},
		{"%ROOT%/../etc", ""},
		{"%ROOT%/a/../../etc", ""},
		{"relative/path", ""},
		{"", ""},
	}
	for _, tt := range tests {
		sys := rbSystem{Descriptor: rbDescriptor{Path: tt.path}}
		assert.Equal(t, tt.want, sys.folder(), tt.path)
	}
}

func TestSystemExtensions(t *testing.T) {
	t.Parallel()
	info := esde.SystemInfo{Extensions: []string{".fallback"}}

	sys := rbSystem{Descriptor: rbDescriptor{Extensions: ".SMC .sfc,.zip  bad . .7z"}}
	assert.Equal(t, []string{".smc", ".sfc", ".zip", ".7z"}, sys.extensions(info))

	empty := rbSystem{}
	assert.Equal(t, []string{".fallback"}, empty.extensions(info))
}

func TestLookupSystem(t *testing.T) {
	t.Parallel()

	info, ok := lookupSystem("segacd")
	require.True(t, ok)
	assert.Equal(t, systemdefs.SystemMegaCD, info.SystemID)

	info, ok = lookupSystem("2048")
	require.True(t, ok)
	assert.Equal(t, systemdefs.SystemPC, info.SystemID)
	assert.Equal(t, portsLauncherID, info.GetLauncherID())

	info, ok = lookupSystem("SNES")
	require.True(t, ok, "shared ES map is used for common names")
	assert.Equal(t, systemdefs.SystemSNES, info.SystemID)

	for name, id := range map[string]string{
		"exl100":   systemdefs.SystemEXL100,
		"p2000t":   systemdefs.SystemP2000T,
		"vg5000":   systemdefs.SystemVG5000,
		"zmachine": systemdefs.SystemZMachine,
	} {
		info, ok = lookupSystem(name)
		require.True(t, ok, name)
		assert.Equal(t, id, info.SystemID, name)
	}

	_, ok = lookupSystem("240ptestsuite")
	assert.False(t, ok, "systems Zaparoo has no ID for stay unmapped")
}

func TestRecalboxSystemsUseKnownIDs(t *testing.T) {
	t.Parallel()
	for name, info := range recalboxSystems {
		_, err := systemdefs.GetSystem(info.SystemID)
		assert.NoError(t, err, name)
	}
}

func TestSystemForPath(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, userSystemList, testUserSystems)
	systems, err := loadSystems(fs)
	require.NoError(t, err)
	roots := []string{"/recalbox/share/roms", "/recalbox/share/externals/usb0/roms"}

	sys, err := systemForPath(systems, "/recalbox/share/roms/snes/sub/game.sfc", roots)
	require.NoError(t, err)
	assert.Equal(t, "snes", sys.Name)

	sys, err = systemForPath(systems, "/recalbox/share/externals/usb0/roms/segacd/a.chd", roots)
	require.NoError(t, err)
	assert.Equal(t, "segacd", sys.Name)

	sys, err = systemForPath(systems, "/recalbox/share/roms/ports/OutRun/outrun.zip", roots)
	require.NoError(t, err)
	assert.Equal(t, "outrun", sys.Name)

	sys, err = systemForPath(systems, "/media/usb/arcade/pacman.zip", roots)
	require.NoError(t, err)
	assert.Equal(t, "mame", sys.Name)

	_, err = systemForPath(systems, "/recalbox/share/roms/snesx/game.sfc", roots)
	require.Error(t, err, "folder prefix must end at a path separator")

	_, err = systemForPath(systems, "/recalbox/share/roms/snes", roots)
	require.Error(t, err, "the folder itself is not a ROM")
}
