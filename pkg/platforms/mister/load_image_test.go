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
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const electronDir = "/media/fat/games/AcornElectron"

// mmbBytes builds the catalog of an MMB image: one read/write disc per title.
func mmbBytes(titles ...string) []byte {
	data := make([]byte, 0x2000)
	for i, title := range titles {
		entry := data[0x10+i*16:]
		copy(entry[:12], title)
		entry[15] = 0x0F
	}
	return data
}

func TestSplitImagePath(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})

	image, inner, ok := splitImagePath(fs, electronDir+"/boot.vhd/Elite")
	require.True(t, ok)
	assert.Equal(t, electronDir+"/boot.vhd", image)
	assert.Equal(t, "Elite", inner)

	image, inner, ok = splitImagePath(fs, electronDir+"/boot.VHD/dir/Sub Game")
	assert.False(t, ok, "the match is on the real file name")
	assert.Empty(t, image)
	assert.Empty(t, inner)

	_, _, ok = splitImagePath(fs, electronDir+"/boot.vhd")
	assert.False(t, ok, "the image itself is not inside an image")

	_, _, ok = splitImagePath(fs, electronDir+"/missing.vhd/Elite")
	assert.False(t, ok)
}

func TestSplitImagePath_NestedPathKeepsEverythingBelowTheImage(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes()})

	image, inner, ok := splitImagePath(fs, electronDir+"/boot.vhd/games/Elite")

	require.True(t, ok)
	assert.Equal(t, electronDir+"/boot.vhd", image)
	assert.Equal(t, "games/Elite", inner)
}

func TestInsideImage(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		electronDir + "/boot.vhd":  mmbBytes("Elite"),
		electronDir + "/plain.uef": {1},
	})

	assert.True(t, insideImage(fs, electronDir+"/boot.vhd/Elite"))
	assert.False(t, insideImage(fs, electronDir+"/boot.vhd"))
	assert.False(t, insideImage(fs, electronDir+"/plain.uef"))
	assert.False(t, insideImage(fs, electronDir+"/gone.vhd/Elite"))
}

func TestMMBTitles_DuplicateTitlesCarryTheDiskNumber(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite", "Chuckie", "elite")})

	got := listMMB(fs, electronDir+"/boot.vhd")

	assert.Equal(t, []string{"Elite (0)", "Chuckie", "elite (2)"}, got)
}

func TestListMMB_NotAnMMBListsNothing(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": make([]byte, 100)})

	assert.Empty(t, listMMB(fs, electronDir+"/boot.vhd"))
	assert.Empty(t, listMMB(fs, electronDir+"/missing.vhd"))
}

func TestMMBPlanner_PlansTheDiskBoot(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite", "Chuckie Egg")})

	plan, ok, err := mmbPlanner(fs, systemdefs.SystemAcornElectron)(nil, electronDir+"/boot.vhd/Chuckie Egg")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, electronDir+"/boot.vhd", plan.target)
	require.Len(t, plan.steps, 2)
	assert.Equal(t, chord("capslock", "0"), plan.steps[0].text)
	assert.Equal(t, "*DBOOT 1{enter}", plan.steps[1].text)
	for _, step := range plan.steps {
		_, tokenErr := keyTokens(step.text, keyLayout{})
		require.NoError(t, tokenErr)
	}
}

func TestMMBPlanner_BBCWaitsAtThePromptWithoutAutostart(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{"/media/fat/games/BBCMicro/boot.vhd": mmbBytes("Elite")})

	plan, ok, err := mmbPlanner(fs, systemdefs.SystemBBCMicro)(nil, "/media/fat/games/BBCMicro/boot.vhd/Elite")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"*DBOOT 0{enter}"}, stepTexts(plan.steps))
	require.Len(t, plan.options, 1)
	assert.Equal(t, mgls.Option{Name: "Autostart", Value: "No"}, plan.options[0])
}

func TestMMBPlanner_FindsDuplicatesByNumberAndTitle(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite", "Other", "Elite")})
	plan := mmbPlanner(fs, systemdefs.SystemAcornElectron)

	byNumber, ok, err := plan(nil, electronDir+"/boot.vhd/Elite (2)")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "*DBOOT 2{enter}", byNumber.steps[1].text)

	byTitle, ok, err := plan(nil, electronDir+"/boot.vhd/elite")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "*DBOOT 0{enter}", byTitle.steps[1].text, "a bare title takes the first disc with it")
}

func TestMMBPlanner_UnknownDiskFailsAndTheImageLaunchesPlain(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})
	plan := mmbPlanner(fs, systemdefs.SystemAcornElectron)

	_, _, err := plan(nil, electronDir+"/boot.vhd/Nothing")
	require.ErrorIs(t, err, errNoSuchDisk)

	_, ok, err := plan(nil, electronDir+"/boot.vhd")
	require.NoError(t, err)
	assert.False(t, ok, "the bare image keeps launching to the image's own menu")
}

func TestMMBPlanner_BrokenImageFailsTheLaunch(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": make([]byte, 64)})

	_, _, err := mmbPlanner(fs, systemdefs.SystemAcornElectron)(nil, electronDir+"/boot.vhd/Elite")

	require.Error(t, err)
}

func TestExtendForImages_ScannerAddsGamesAfterTheImages(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{
		electronDir + "/boot.vhd":  mmbBytes("Elite", "Chuckie"),
		electronDir + "/other.vhd": make([]byte, 10),
	})
	launcher := platforms.Launcher{}
	extendForImages(fs, listMMB, nil, nil)(&launcher)

	got, err := launcher.Scanner(context.Background(), nil, systemdefs.SystemAcornElectron, []platforms.ScanResult{
		{Path: electronDir + "/boot.vhd"},
		{Path: electronDir + "/other.vhd"},
		{Path: electronDir + "/tape.uef"},
	})

	require.NoError(t, err)
	paths := make([]string, 0, len(got))
	for _, r := range got {
		paths = append(paths, r.Path)
	}
	assert.Equal(t, []string{
		electronDir + "/boot.vhd", electronDir + "/other.vhd", electronDir + "/tape.uef",
		electronDir + "/boot.vhd/Elite", electronDir + "/boot.vhd/Chuckie",
	}, paths)
	assert.True(t, got[3].NoExt)
	assert.True(t, got[4].NoExt)
}

func TestExtendForImages_ScannerStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})
	launcher := platforms.Launcher{}
	extendForImages(fs, listMMB, nil, nil)(&launcher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := launcher.Scanner(ctx, nil, "", []platforms.ScanResult{{Path: electronDir + "/boot.vhd"}})

	require.ErrorIs(t, err, context.Canceled)
}

func TestExtendForImages_TestClaimsGamesInsideImagesAndKeepsThePriorTest(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})
	launcher := platforms.Launcher{Test: func(_ *config.Instance, path string) bool { return path == "/special" }}
	extendForImages(fs, listMMB, nil, nil)(&launcher)

	assert.True(t, launcher.Test(nil, electronDir+"/boot.vhd/Elite"))
	assert.True(t, launcher.Test(nil, "/special"))
	assert.False(t, launcher.Test(nil, electronDir+"/boot.vhd"))
	assert.False(t, launcher.Test(nil, filepath.Join(electronDir, "elsewhere", "Elite")))
}

func TestExtendForImages_RunsThePriorScannerFirst(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{})
	launcher := platforms.Launcher{
		Scanner: func(
			_ context.Context, _ *config.Instance, _ string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return append(results, platforms.ScanResult{Path: "/prior"}), nil
		},
	}
	extendForImages(fs, listMMB, nil, nil)(&launcher)

	got, err := launcher.Scanner(context.Background(), nil, "", nil)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "/prior", got[0].Path)
}

func TestExtendForImages_ScannerListsImagesTheScanLeftOut(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})
	launcher := platforms.Launcher{}
	discover := func(context.Context, *config.Instance, string) []string {
		return []string{electronDir + "/boot.vhd", electronDir + "/seen.vhd"}
	}
	extendForImages(fs, listMMB, nil, discover)(&launcher)

	got, err := launcher.Scanner(context.Background(), nil, "", []platforms.ScanResult{
		{Path: electronDir + "/seen.vhd"},
	})

	require.NoError(t, err)
	paths := make([]string, 0, len(got))
	for _, r := range got {
		paths = append(paths, r.Path)
	}
	assert.Equal(t, []string{electronDir + "/seen.vhd", electronDir + "/boot.vhd/Elite"}, paths,
		"boot.vhd stays out of the index itself but its games are listed; a found image is not listed twice")
}
