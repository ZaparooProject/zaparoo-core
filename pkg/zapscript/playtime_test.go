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

package zapscript

import (
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newExtendPlatform is the minimum platform the advanced-argument parser
// needs: it reads the launcher list to build its validation context.
func newExtendPlatform(t *testing.T) platforms.Platform {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("Launchers", mock.Anything).Return([]platforms.Launcher{}).Maybe()
	return pl
}

// extendEnv builds the command environment for a card carrying nothing but
// a playtime extension, scanned on a physical reader.
func extendEnv(amount string, advArgs map[string]string) platforms.CmdEnv {
	args := []string{}
	if amount != "" {
		args = append(args, amount)
	}
	return platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name:    gozapscript.ZapScriptCmdPlaytimeExtend,
			Args:    args,
			AdvArgs: gozapscript.NewAdvArgs(advArgs),
		},
		Source:        tokens.SourceReader,
		TotalCommands: 1,
	}
}

func TestCmdPlaytimeExtend_Duration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount string
		want   time.Duration
	}{
		{name: "minutes", amount: "15m", want: 15 * time.Minute},
		{name: "compound", amount: "1h30m", want: 90 * time.Minute},
		{name: "hours", amount: "2h", want: 2 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := cmdPlaytimeExtend(newExtendPlatform(t),
				extendEnv(tt.amount, map[string]string{"profile": "switch-abc"}))
			require.NoError(t, err)

			require.NotNil(t, result.PlaytimeExtension)
			assert.Equal(t, models.PlaytimeExtendModeDuration, result.PlaytimeExtension.Mode)
			assert.Equal(t, tt.want, result.PlaytimeExtension.Duration)
			assert.Equal(t, "switch-abc", result.PlaytimeExtension.AuthorizerSwitchID)
		})
	}
}

func TestCmdPlaytimeExtend_Today(t *testing.T) {
	t.Parallel()

	// The keyword is matched case-insensitively; a card may be written by
	// hand in any case.
	for _, amount := range []string{"today", "Today", "TODAY"} {
		t.Run(amount, func(t *testing.T) {
			t.Parallel()

			result, err := cmdPlaytimeExtend(newExtendPlatform(t),
				extendEnv(amount, map[string]string{"profile": "switch-abc"}))
			require.NoError(t, err)

			require.NotNil(t, result.PlaytimeExtension)
			assert.Equal(t, models.PlaytimeExtendModeToday, result.PlaytimeExtension.Mode)
			assert.Equal(t, time.Duration(0), result.PlaytimeExtension.Duration)
		})
	}
}

// A grant weakens somebody's limits, so it must require physical possession
// of a card. Any other source would turn a path that can run ZapScript into
// a way around a limit.
func TestCmdPlaytimeExtend_RejectsNonReaderSources(t *testing.T) {
	t.Parallel()

	sources := []string{
		tokens.SourceAPI, tokens.SourceHook, tokens.SourcePlaylist,
		tokens.SourceGMC, tokens.SourceControl, tokens.SourceRemote, "",
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			env := extendEnv("15m", map[string]string{"profile": "switch-abc"})
			env.Source = source

			result, err := cmdPlaytimeExtend(newExtendPlatform(t), env)
			require.ErrorIs(t, err, ErrExtendSourceNotReader)
			assert.Nil(t, result.PlaytimeExtension)
		})
	}
}

// A combo card must not be able to order an extension ahead of a launch to
// slip past the pre-launch limit check.
func TestCmdPlaytimeExtend_RejectsMixedScript(t *testing.T) {
	t.Parallel()

	env := extendEnv("15m", map[string]string{"profile": "switch-abc"})
	env.TotalCommands = 2

	result, err := cmdPlaytimeExtend(newExtendPlatform(t), env)
	require.ErrorIs(t, err, ErrExtendNotAlone)
	assert.Nil(t, result.PlaytimeExtension)
}

func TestCmdPlaytimeExtend_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		advArgs map[string]string
		name    string
		amount  string
	}{
		{
			name: "missing amount", amount: "",
			advArgs: map[string]string{"profile": "switch-abc"}, wantErr: ErrArgCount,
		},
		{
			name: "missing profile", amount: "15m",
			advArgs: map[string]string{}, wantErr: ErrExtendProfileMissing,
		},
		{
			name: "empty profile", amount: "15m",
			advArgs: map[string]string{"profile": ""}, wantErr: ErrExtendProfileMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := cmdPlaytimeExtend(newExtendPlatform(t), extendEnv(tt.amount, tt.advArgs))
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, result.PlaytimeExtension)
		})
	}
}

func TestCmdPlaytimeExtend_RejectsUnparseableAmount(t *testing.T) {
	t.Parallel()

	for _, amount := range []string{"soon", "15", "tomorrow", "-", "15 minutes"} {
		t.Run(amount, func(t *testing.T) {
			t.Parallel()

			result, err := cmdPlaytimeExtend(newExtendPlatform(t),
				extendEnv(amount, map[string]string{"profile": "switch-abc"}))
			require.Error(t, err)
			assert.Nil(t, result.PlaytimeExtension)
		})
	}
}

func TestCmdPlaytimeExtend_RejectsUnknownAdvArg(t *testing.T) {
	t.Parallel()

	result, err := cmdPlaytimeExtend(newExtendPlatform(t), extendEnv("15m", map[string]string{
		"profile": "switch-abc",
		"pin":     "1234",
	}))
	require.Error(t, err, "an unknown argument must not be silently ignored")
	assert.Nil(t, result.PlaytimeExtension)
}

func TestCmdPlaytimeExtend_IsSensitive(t *testing.T) {
	t.Parallel()

	// Both commands carry a profile switch ID and must stay out of logs.
	assert.True(t, isSensitiveCommand(gozapscript.ZapScriptCmdPlaytimeExtend))
	assert.True(t, isSensitiveCommand(gozapscript.ZapScriptCmdProfile))
	assert.False(t, isSensitiveCommand(gozapscript.ZapScriptCmdLaunch))
}

func TestCmdPlaytimeExtend_IsRegistered(t *testing.T) {
	t.Parallel()

	assert.True(t, IsValidCommand(gozapscript.ZapScriptCmdPlaytimeExtend))
}

// An extension changes no media, so it must not be treated as a launch or
// staged behind the launch guard.
func TestCmdPlaytimeExtend_DoesNotDisruptMedia(t *testing.T) {
	t.Parallel()

	assert.False(t, IsMediaLaunchingCommand(gozapscript.ZapScriptCmdPlaytimeExtend))
	assert.False(t, IsMediaDisruptingCommand(gozapscript.ZapScriptCmdPlaytimeExtend))
}
