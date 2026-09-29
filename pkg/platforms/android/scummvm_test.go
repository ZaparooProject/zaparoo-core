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

package android

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A .scummvm file's identity is the same plain source path the RetroArch core
// plays, never a scheme derived from the file's content.
func TestScummVMLauncherMatchesTheSameFilesAsTheCore(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	launcher := launcherByID(t, platform.Launchers(nil), scummVMStandaloneID)

	cfg := &config.Instance{}
	assert.True(t, helpers.PathIsLauncher(cfg, platform, launcher, identity(t, "scummvm", "sky.scummvm")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, launcher, identity(t, "ScummVM", "Sub", "sky.SCUMMVM")))
	assert.False(t, helpers.PathIsLauncher(cfg, platform, launcher, identity(t, "scummvm", "sky.zip")))
}

func TestScummVMTargetPrefersFileContentOverName(t *testing.T) {
	t.Parallel()
	host := &fakeHost{files: map[string][]byte{
		"scummvm/sky.scummvm":        []byte("sky\n"),
		"scummvm/empty.scummvm":      []byte(""),
		"scummvm/bad-target.scummvm": []byte("-not-a-target"),
	}}
	platform := startedPlatform(t.Context(), t, host)

	require.NoError(t, platform.dispatchScummVM(identity(t, "scummvm", "sky.scummvm")))
	require.Len(t, host.dispatched, 1)
	assert.Equal(t, "scummvm:sky", host.dispatched[0].definition.Data)

	// An empty file falls back to the file's own name, less the extension.
	require.NoError(t, platform.dispatchScummVM(identity(t, "scummvm", "empty.scummvm")))
	require.Len(t, host.dispatched, 2)
	assert.Equal(t, "scummvm:empty", host.dispatched[1].definition.Data)

	// A target that is not a plain ID (starts with a dash: ScummVM would read
	// it as an option) is refused rather than sent.
	err := platform.dispatchScummVM(identity(t, "scummvm", "bad-target.scummvm"))
	require.EqualError(t, err, msgWrongMedia)
	assert.Len(t, host.dispatched, 2, "a refused target never reaches the host")
}

// A target at scummVMTargetPattern's own maximum length must not be rejected
// by validData's length bound on the assembled Data field: the two limits
// have to agree.
func TestScummVMTargetAtMaximumLengthIsAccepted(t *testing.T) {
	t.Parallel()
	target := strings.Repeat("a", 64) + ":" + strings.Repeat("b", 64)
	require.Len(t, target, 129, "matches scummVMTargetPattern's own maximum")
	require.True(t, scummVMTargetPattern.MatchString(target))
	host := &fakeHost{files: map[string][]byte{"scummvm/game.scummvm": []byte(target)}}
	platform := startedPlatform(t.Context(), t, host)

	require.NoError(t, platform.dispatchScummVM(identity(t, "scummvm", "game.scummvm")))

	require.Len(t, host.dispatched, 1)
	assert.Equal(t, "scummvm:"+target, host.dispatched[0].definition.Data)
}

func TestScummVMDispatchSendsTheDefinitionUnchangedExceptData(t *testing.T) {
	t.Parallel()
	host := &fakeHost{files: map[string][]byte{"scummvm/sky.scummvm": []byte("sky")}}
	platform := startedPlatform(t.Context(), t, host)

	require.NoError(t, platform.dispatchScummVM(identity(t, "scummvm", "sky.scummvm")))

	require.Len(t, host.dispatched, 1)
	sent := host.dispatched[0].definition
	assert.Equal(t, scummVMPackage, sent.Package)
	assert.Equal(t, scummVMActivity, sent.Activity)
	assert.Equal(t, StrategyApp, sent.Strategy)
	assert.Equal(t, "scummvm:sky", sent.Data)
	assert.Empty(t, scummVMEntry.definition.Data, "the shared template is never mutated")
}

func TestScummVMDispatchReportsAnUnreadableFile(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)

	err := platform.dispatchScummVM(identity(t, "scummvm", "gone.scummvm"))

	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	assert.Equal(t, repairMessage(FailureSourceUnavailable, ""), repair.Error())
}
