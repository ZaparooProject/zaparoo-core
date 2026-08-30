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

package service

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// These tests drive processTokenQueue the way the JSON-RPC run method does:
// a token with a Completion goes straight onto the worker queue, and the
// test asserts what the completion reports for each terminal path.

func (env *scanBehaviorEnv) sendAPIToken(t *testing.T, text string) *tokens.Completion {
	t.Helper()
	c := tokens.NewCompletion()
	env.sendAPITokenWith(t, tokens.Token{
		Text:       text,
		ScanTime:   time.Now(),
		Source:     tokens.SourceAPI,
		Completion: c,
	})
	return c
}

func (env *scanBehaviorEnv) sendAPITokenWith(t *testing.T, tok tokens.Token) { //nolint:gocritic // test helper
	t.Helper()
	select {
	case env.itq <- tok:
	case <-time.After(behaviorTimeout):
		t.Fatal("token worker did not accept the token")
	}
}

func waitCompletion(t *testing.T, c *tokens.Completion) error {
	t.Helper()
	select {
	case err := <-c.Done():
		return err
	case <-time.After(behaviorTimeout):
		t.Fatal("token was never completed")
		return nil
	}
}

// assertCompletedOnce reads the result and proves no second delivery is
// possible.
func assertCompletedOnce(t *testing.T, c *tokens.Completion) error {
	t.Helper()
	err := waitCompletion(t, c)
	assert.False(t, c.Complete(nil), "completion must be delivered exactly once")
	return err
}

func (env *scanBehaviorEnv) waitForHistory(t *testing.T, tokenValue string) database.HistoryEntry {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		select {
		case he := <-env.historyCh:
			if he.TokenValue == tokenValue {
				return he
			}
		case <-deadline:
			t.Fatalf("no history entry recorded for %q", tokenValue)
			return database.HistoryEntry{}
		}
	}
}

func TestTokenCompletion_SuccessAfterLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	path := env.gamePath("game1.gba")
	c := env.sendAPIToken(t, path)

	assert.Equal(t, path, env.waitForLaunch(t))
	require.NoError(t, assertCompletedOnce(t, c))
}

func TestTokenCompletion_CommandFailureIsReported(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	c := env.sendAPIToken(t, "**nonexistent.cmd")
	require.ErrorIs(t, assertCompletedOnce(t, c), zapscript.ErrUnknownCommand)
	env.expectNoLaunch(t)
	assert.False(t, env.waitForHistory(t, "**nonexistent.cmd").Success)
}

func TestTokenCompletion_DisabledIsReportedButHistoryStaysSuccessful(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.st.SetRunZapScript(false)

	path := env.gamePath("game1.gba")
	c := env.sendAPIToken(t, path)

	require.ErrorIs(t, assertCompletedOnce(t, c), state.ErrRunZapScriptDisabled)
	env.expectNoLaunch(t)
	assert.True(t, env.waitForHistory(t, path).Success, "disabled keeps the prior no-op success in history")
}

func TestTokenCompletion_ProfileGateRejectsBeforeLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetProfilesRequireForLaunch(true)

	path := env.gamePath("game1.gba")
	c := env.sendAPIToken(t, path)

	require.ErrorIs(t, assertCompletedOnce(t, c), state.ErrLaunchRequiresProfile)
	env.expectNoLaunch(t)
	assert.False(t, env.waitForHistory(t, path).Success)
}

func TestTokenCompletion_PlaytimeLimitRejectsBeforeLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	env.cfg.SetPlaytimeLimitsEnabled(true)
	require.NoError(t, env.cfg.SetDailyLimit("1h"))
	// Two hours already played today.
	env.userDB.On("SumMediaPlayTimeForDay", mock.Anything).Return(int64(7200), nil).Maybe()

	path := env.gamePath("game1.gba")
	c := env.sendAPIToken(t, path)

	require.ErrorIs(t, assertCompletedOnce(t, c), playtime.ErrLimitReached)
	env.expectNoLaunch(t)
	assert.False(t, env.waitForHistory(t, path).Success)
}

func TestTokenCompletion_NextActionOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("blocked single command reports a configuration denial", func(t *testing.T) {
		t.Parallel()
		env := setupScanBehavior(t, "tap", 0)
		require.NoError(t, env.cfg.LoadTOML(`[zapscript]
block_commands = ["input.keyboard"]`))

		// Preflight rejects this before RunCommand sees it, so it has to
		// report the same category a multi-command script would.
		c := env.sendAPIToken(t, "**input.keyboard:a")
		require.ErrorIs(t, assertCompletedOnce(t, c), zapscript.ErrCommandBlocked)
	})

	t.Run("arming a next-card write is success", func(t *testing.T) {
		t.Parallel()
		env := setupScanBehavior(t, "tap", 0)

		c := env.sendAPIToken(t, "**write:payload")
		require.NoError(t, assertCompletedOnce(t, c))
		assert.True(t, env.waitForHistory(t, "**write:payload").Success)
	})
}

func TestTokenCompletion_BlockedCommandInsideScript(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	require.NoError(t, env.cfg.LoadTOML(`[zapscript]
block_commands = ["input.keyboard"]`))

	// Two commands skip next-action preflight, so the block surfaces from
	// RunCommand instead of the preflight path above.
	c := env.sendAPIToken(t, "**input.keyboard:a||**input.keyboard:b")
	require.ErrorIs(t, assertCompletedOnce(t, c), zapscript.ErrCommandBlocked)
}

func TestTokenCompletion_EmptyTokenIsRejected(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	c := tokens.NewCompletion()
	env.sendAPITokenWith(t, tokens.Token{Text: "ignored", Source: tokens.SourceAPI, Completion: c})
	require.ErrorIs(t, assertCompletedOnce(t, c), errEmptyToken)
}

func TestTokenCompletion_PanicIsReportedAndWorkerContinues(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	panicPath := env.gamePath("panic.gba")
	env.launchHook.set(func(path string) {
		if path == panicPath {
			panic("launcher exploded")
		}
	})

	c1 := env.sendAPIToken(t, panicPath)
	require.ErrorIs(t, assertCompletedOnce(t, c1), errLaunchPanicked)
	assert.False(t, env.waitForHistory(t, panicPath).Success, "a panic is still recorded as a failed scan")

	nextPath := env.gamePath("game2.gba")
	c2 := env.sendAPIToken(t, nextPath)
	assert.Equal(t, nextPath, env.waitForLaunch(t))
	require.NoError(t, assertCompletedOnce(t, c2))
}

func TestTokenCompletion_ShutdownMidLaunchStillCompletes(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	started := make(chan struct{})
	env.launchHook.set(func(string) {
		close(started)
		<-env.st.GetContext().Done()
	})

	c := env.sendAPIToken(t, env.gamePath("slow.gba"))
	select {
	case <-started:
	case <-time.After(behaviorTimeout):
		t.Fatal("launch never started")
	}

	env.st.StopService()
	// The result may be success or a shutdown error depending on where the
	// script was interrupted; what matters is that the caller is released.
	err := assertCompletedOnce(t, c)
	t.Logf("completion after shutdown: %v", err)
}

func TestTokenCompletion_AbandonedCallerNeverBlocksWorker(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	// Nobody ever reads this completion, as if the request timed out.
	first := env.gamePath("abandoned.gba")
	env.sendAPIToken(t, first)
	assert.Equal(t, first, env.waitForLaunch(t))

	second := env.gamePath("game2.gba")
	c := env.sendAPIToken(t, second)
	assert.Equal(t, second, env.waitForLaunch(t))
	require.NoError(t, assertCompletedOnce(t, c))
}

func TestTokenCompletion_ReaderTokensCarryNoCompletion(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)

	// Physical scans go through the reader path untouched: they launch and
	// nothing waits on them.
	env.sendGameScan("card1", env.gamePath("game1.gba"))
	env.waitForLaunch(t)
	assert.Nil(t, env.st.GetActiveCard().Completion)
}
