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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/gamehandoff"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		want    []string
		wantErr bool
	}{
		{name: "plain text", text: "ab1", want: []string{"a", "b", "1"}},
		{name: "special key", text: "{f10}x", want: []string{"{f10}", "x"}},
		{name: "combo", text: "{ctrl+f11}", want: []string{"{ctrl+f11}"}},
		{name: "shifted punctuation", text: `*"|:`, want: []string{"*", `"`, "|", ":"}},
		{name: "command line", text: "*DBOOT 5{enter}", want: []string{
			"*", "d", "b", "o", "o", "t", " ", "5", "{enter}",
		}},
		{name: "empty", text: "", want: []string{}},
		{name: "inline controls", text: "{press:capslock}{delay:400ms}0{delay:50}{release:capslock}", want: []string{
			"{press:capslock}", "{delay:400ms}", "0", "{delay:50}", "{release:capslock}",
		}},
		{name: "hold with duration", text: "{hold:esc:500}", want: []string{"{hold:esc:500}"}},
		{name: "shifted key combo", text: "{shift+'}", want: []string{"{shift+'}"}},
		{name: "bad delay", text: "{delay:soon}", wantErr: true},
		{name: "delay too long", text: "{delay:1m}", wantErr: true},
		{name: "press of an unknown key", text: "{press:nosuchkey}", wantErr: true},
		{name: "unterminated", text: "{enter", wantErr: true},
		{name: "unknown key", text: "{nosuchkey}", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := keyTokens(tt.text, keyLayout{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestKeyTokens_LayoutMapsCharactersOntoTheMachineKeyboard(t *testing.T) {
	t.Parallel()

	got, err := keyTokens(`LOAD"*",8:{enter}`, commodoreLayout())
	require.NoError(t, err)
	assert.Equal(t, []string{
		"l", "o", "a", "d", "{shift+2}", "]", "{shift+2}", ",", "8", ";", "{enter}",
	}, got)

	got, err = keyTokens("*DBOOT 5", acornLayout())
	require.NoError(t, err)
	assert.Equal(t, []string{"{shift+'}", "d", "b", "o", "o", "t", " ", "5"}, got)

	got, err = keyTokens(`J""`, spectrumLayout())
	require.NoError(t, err)
	assert.Equal(t, []string{"j", "'", "'"}, got)

	got, err = keyTokens("c:x", nextLayout())
	require.NoError(t, err)
	assert.Equal(t, []string{
		"c", "{press:ctrl}", "{delay:400ms}", "z", "{delay:400ms}", "{release:ctrl}", "{delay:400ms}", "x",
	}, got)

	got, err = keyTokens(":EXDOS", enterpriseLayout())
	require.NoError(t, err)
	assert.Equal(t, []string{"'", "e", "x", "d", "o", "s"}, got)

	got, err = keyTokens("{f10}Ab", keyLayout{})
	require.NoError(t, err)
	assert.Equal(t, []string{"{f10}", "a", "b"}, got, "braced keys pass through; letters are typed unshifted")
}

func TestLoadPlan_ReadyTimeoutCoversSettleAndTyping(t *testing.T) {
	t.Parallel()

	plan := loadPlan{
		settle: 4 * time.Second,
		steps:  []loadStep{typeAfter(2*time.Second, "ab")},
	}

	want := loadTimeoutPad + 4*time.Second + 2*time.Second + 2*loadKeyDelay*loadKeyFudge
	assert.Equal(t, want, plan.readyTimeout())
}

func TestLoadPlans_RecordPeekTake(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	now := h.clock.Now()

	_, ok := h.r.plans.peek("/a", now)
	assert.False(t, ok)

	h.r.plans.record("/a", &loadPlan{target: "/t"}, now)
	got, ok := h.r.plans.peek("/a", now)
	require.True(t, ok)
	assert.Equal(t, "/t", got.target)

	_, ok = h.r.plans.take("/a", now)
	require.True(t, ok)
	_, ok = h.r.plans.take("/a", now)
	assert.False(t, ok)
}

func TestLoadPlans_StalePlanIsIgnoredAndPruned(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	now := h.clock.Now()

	h.r.plans.record("/old", &loadPlan{}, now)
	later := now.Add(loadPlanFreshness + time.Second)

	_, ok := h.r.plans.peek("/old", later)
	assert.False(t, ok)

	h.r.plans.record("/new", &loadPlan{}, later)
	_, ok = h.r.plans.peek("/old", now)
	assert.False(t, ok, "recording prunes plans no probe claimed")
}

type typed struct {
	tokens []string
	delay  time.Duration
}

func (h *readyHarness) captureTyping() *[]typed {
	var out []typed
	h.r.pressSequence = func(tokens []string, delay time.Duration) error {
		out = append(out, typed{tokens: tokens, delay: delay})
		return nil
	}
	return &out
}

func TestRunPlan_NoPlanTypesNothing(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	out := h.captureTyping()

	require.NoError(t, h.r.runPlan(t.Context(), "/none"))
	assert.Empty(t, *out)
}

func TestRunPlan_TypesEachStepInOrder(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	out := h.captureTyping()
	h.r.plans.record("/g", &loadPlan{steps: []loadStep{typeText("{f10}"), typeText("ab{enter}")}}, h.clock.Now())

	require.NoError(t, h.r.runPlan(t.Context(), "/g"))

	require.Len(t, *out, 2)
	assert.Equal(t, []string{"{f10}"}, (*out)[0].tokens)
	assert.Equal(t, []string{"a", "b", "{enter}"}, (*out)[1].tokens)
	assert.Equal(t, loadKeyDelay, (*out)[1].delay)
}

func TestRunPlan_WaitsOutSettleAndPauses(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	out := h.captureTyping()
	h.r.plans.record("/g", &loadPlan{
		settle: 3 * time.Second,
		steps:  []loadStep{typeAfter(2*time.Second, "x")},
	}, h.clock.Now())

	done := make(chan error, 1)
	go func() { done <- h.r.runPlan(t.Context(), "/g") }()

	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	assert.Empty(t, *out)
	h.clock.Advance(3 * time.Second)
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(2 * time.Second)

	require.NoError(t, <-done)
	require.Len(t, *out, 1)
}

func TestRunPlan_CancelledBeforeTypingTypesNothing(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	out := h.captureTyping()
	h.r.plans.record("/g", &loadPlan{settle: time.Hour, steps: []loadStep{typeText("x")}}, h.clock.Now())
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- h.r.runPlan(ctx, "/g") }()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	assert.Empty(t, *out)
}

func TestRunPlan_InputFailureIsReported(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.r.pressSequence = func([]string, time.Duration) error { return errors.New("uinput gone") }
	h.r.plans.record("/g", &loadPlan{steps: []loadStep{typeText("x")}}, h.clock.Now())

	err := h.r.runPlan(t.Context(), "/g")

	require.ErrorContains(t, err, "uinput gone")
}

func TestRunPlan_NoKeyboardIsReported(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.r.plans.record("/g", &loadPlan{steps: []loadStep{typeText("x")}}, h.clock.Now())

	err := h.r.runPlan(t.Context(), "/g")

	require.ErrorIs(t, err, errLoadKeyboardUnavailable)
}

func TestCoreReadiness_PlanTypesAfterTheImageIsMounted(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	out := h.captureTyping()
	image := "/media/fat/games/BBCMicro/boot.vhd"
	game := image + "/Elite"
	h.files[image] = 100 << 20
	var watched string
	h.r.watch = func(_ context.Context, path string) (<-chan gamehandoff.Kind, error) {
		watched = path
		return h.events, nil
	}
	h.r.plans.record(game, &loadPlan{target: image, steps: []loadStep{typeText("*DBOOT 5{enter}")}}, h.clock.Now())
	done := h.start(t.Context(), mustCore(t, "BBCMicro"), game)

	h.events <- gamehandoff.Open
	require.NoError(t, h.advanceUntilDone(t, done))

	assert.Equal(t, image, watched)
	require.Len(t, *out, 1)
	assert.Equal(t, "*dboot 5{enter}", joinTokens((*out)[0].tokens))
}

func joinTokens(tokens []string) string {
	return strings.Join(tokens, "")
}

func TestCoreReadiness_PlanInputFailureFailsTheProbe(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.r.pressSequence = func([]string, time.Duration) error { return errors.New("no keyboard") }
	game := "/media/fat/games/C64/game.d64"
	h.files[game] = 170 << 10
	h.r.plans.record(game, &loadPlan{target: game, steps: []loadStep{typeText("x")}}, h.clock.Now())
	done := h.start(t.Context(), mustCore(t, "C64"), game)

	h.events <- gamehandoff.Open
	require.ErrorContains(t, h.advanceUntilDone(t, done), "no keyboard")
}

func TestCoreReadiness_ReadyTimeoutFollowsThePlan(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	plan := loadPlan{settle: 4 * time.Second, steps: []loadStep{typeText("abc")}}
	h.r.plans.record("/g", &plan, h.clock.Now())

	assert.Equal(t, readyCoreWait+plan.readyTimeout(), h.r.readyTimeout(probeMedia("/g")))
	assert.NotEqual(t, readyCoreWait+plan.readyTimeout(), h.r.readyTimeout(probeMedia("/other.md")))
}

func TestLaunchPlanned_NoPlanUsesTheOriginalLaunch(t *testing.T) {
	t.Parallel()
	called := ""
	fallback := func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		called = path
		return nil, nil //nolint:nilnil // a launch with no process handle
	}
	launch := launchPlanned(nil, "C64", func(*config.Instance, string) (loadPlan, bool, error) {
		return loadPlan{}, false, nil
	}, fallback)

	_, err := launch(nil, "/g.prg", nil)

	require.NoError(t, err)
	assert.Equal(t, "/g.prg", called)
}

func TestLaunchPlanned_PlannerErrorFailsTheLaunch(t *testing.T) {
	t.Parallel()
	launch := launchPlanned(nil, "C64", func(*config.Instance, string) (loadPlan, bool, error) {
		return loadPlan{}, false, errNoSuchDisk
	}, func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		t.Fatal("must not launch")
		return nil, nil //nolint:nilnil // unreachable
	})

	_, err := launch(nil, "/g", nil)

	require.ErrorIs(t, err, errNoSuchDisk)
}

func TestLaunchPlanned_BadKeyRejectsTheLaunchBeforeAnythingStarts(t *testing.T) {
	t.Parallel()
	launch := launchPlanned(nil, "C64", func(*config.Instance, string) (loadPlan, bool, error) {
		return loadPlan{target: "/g", steps: []loadStep{typeText("{nosuchkey}")}}, true, nil
	}, nil)

	_, err := launch(nil, "/g", nil)

	require.Error(t, err)
	_, recorded := launchReadiness.plans.peek("/g", launchReadiness.clock.Now())
	assert.False(t, recorded)
}

func TestWithLoadPlans_ExtendsOnlyPlannedStockLaunchers(t *testing.T) {
	t.Parallel()
	launch := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		return nil, nil //nolint:nilnil // a launch with no process handle
	}
	ls := []platforms.Launcher{
		{ID: systemdefs.SystemBBCMicro, SystemID: systemdefs.SystemBBCMicro, Launch: launch},
		{ID: "Other", SystemID: systemdefs.SystemBBCMicro, Launch: launch},
		{ID: systemdefs.SystemNES, SystemID: systemdefs.SystemNES, Launch: launch},
	}

	got := withLoadPlans(nil, ls)

	assert.NotNil(t, got[0].Scanner, "stock BBC launcher lists the games in its images")
	assert.NotNil(t, got[0].Test)
	assert.Nil(t, got[1].Scanner, "a launcher with another ID is left alone")
	assert.Nil(t, got[2].Scanner)
}

func TestLoadPlanners_CoverTheCoresThatTypeAfterLoading(t *testing.T) {
	t.Parallel()
	sets := loadPlannersFor(nil)

	for _, id := range []string{
		systemdefs.SystemAcornElectron, systemdefs.SystemBBCMicro, systemdefs.SystemC64,
		systemdefs.SystemAmstrad, systemdefs.SystemZXSpectrum, systemdefs.SystemCoCo2,
	} {
		assert.Contains(t, sets, id)
	}
}

func memFS(t *testing.T, files map[string][]byte) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	for path, data := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.FromSlash(path), data, 0o644))
	}
	return fs
}

func TestPreflightPlan_FailsBeforeAnythingIsStopped(t *testing.T) {
	t.Parallel()
	fs := memFS(t, map[string][]byte{electronDir + "/boot.vhd": mmbBytes("Elite")})
	preflight := preflightPlan(mmbPlanner(fs, systemdefs.SystemAcornElectron), nil)

	require.NoError(t, preflight(nil, electronDir+"/boot.vhd/Elite", nil))
	require.NoError(t, preflight(nil, electronDir+"/boot.vhd", nil))
	require.ErrorIs(t, preflight(nil, electronDir+"/boot.vhd/Nothing", nil), errNoSuchDisk)
}

func TestPreflightPlan_PriorPreflightRunsFirst(t *testing.T) {
	t.Parallel()
	prior := func(*config.Instance, string, *platforms.LaunchOptions) error { return errNoSuchFile }
	planned := false
	plan := func(*config.Instance, string) (loadPlan, bool, error) {
		planned = true
		return loadPlan{}, false, nil
	}

	err := preflightPlan(plan, prior)(nil, "/x", nil)

	require.ErrorIs(t, err, errNoSuchFile)
	assert.False(t, planned)
}
