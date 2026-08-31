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

package methods

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const runTestTimeout = 2 * time.Second

// runTestEnv stands in for the service worker: it owns the unbuffered token
// queue so a test can receive the token run queued and complete it however
// the case requires.
type runTestEnv struct {
	st    *state.State
	queue chan tokens.Token
}

func newRunTestEnv(t *testing.T) *runTestEnv {
	t.Helper()

	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()
	st, notifCh := state.NewState(platform, "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-notifCh:
			default:
				return
			}
		}
	})

	return &runTestEnv{st: st, queue: make(chan tokens.Token)}
}

func (e *runTestEnv) requestEnv(ctx context.Context, text string) requests.RequestEnv {
	return requests.RequestEnv{
		Context:    ctx,
		State:      e.st,
		TokenQueue: e.queue,
		Params:     []byte(fmt.Sprintf(`{"text":%q}`, text)),
	}
}

func (e *runTestEnv) receiveToken(t *testing.T) tokens.Token {
	t.Helper()

	select {
	case tok := <-e.queue:
		require.Equal(t, tokens.SourceAPI, tok.Source)
		require.NotNil(t, tok.Completion, "API tokens must carry a completion")
		return tok
	case <-time.After(runTestTimeout):
		t.Fatal("run handler did not queue a token")
		return tokens.Token{}
	}
}

type runOutcome struct {
	result any
	err    error
}

func startRun(env requests.RequestEnv) <-chan runOutcome { //nolint:gocritic // mirrors handler signature
	out := make(chan runOutcome, 1)
	go func() {
		result, err := HandleRun(env)
		out <- runOutcome{result: result, err: err}
	}()
	return out
}

func waitRun(t *testing.T, out <-chan runOutcome) runOutcome {
	t.Helper()

	select {
	case o := <-out:
		return o
	case <-time.After(runTestTimeout):
		t.Fatal("run handler did not return")
		return runOutcome{}
	}
}

func requireCategory(t *testing.T, err error, category string) {
	t.Helper()

	require.Error(t, err)
	var catErr *models.CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, category, catErr.Category)
}

func TestHandleRunWaitsForCompletionThenSucceeds(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	out := startRun(env.requestEnv(context.Background(), "**launch.system:snes"))
	tok := env.receiveToken(t)

	select {
	case o := <-out:
		t.Fatalf("run returned before execution completed: %+v", o)
	case <-time.After(50 * time.Millisecond):
	}

	require.True(t, tok.Completion.Complete(nil))
	o := waitRun(t, out)
	require.NoError(t, o.err)
	assert.Equal(t, NoContent{}, o.result)
}

func TestHandleRunReportsExecutionFailureByCategory(t *testing.T) {
	t.Parallel()

	const leakedPath = "/media/fat/games/SNES/Secret Game.sfc"

	tests := []struct {
		cause    error
		name     string
		category string
		message  string
	}{
		{
			name:     "launch in progress",
			cause:    fmt.Errorf("launch guard: %w", state.ErrLaunchInProgress),
			category: models.ErrorCategoryBusy,
			message:  "another launch is in progress",
		},
		{
			name:     "media launch in progress",
			cause:    state.ErrMediaLaunchInProgress,
			category: models.ErrorCategoryBusy,
			message:  "another launch is in progress",
		},
		{
			name:     "file not found",
			cause:    fmt.Errorf("failed to run zapscript command: %w: %s", zapscript.ErrFileNotFound, leakedPath),
			category: models.ErrorCategoryMediaNotFound,
			message:  "media not found",
		},
		{
			name:     "no title match",
			cause:    fmt.Errorf("resolve %s: %w", leakedPath, titles.ErrNoMatch),
			category: models.ErrorCategoryMediaNotFound,
			message:  "media not found",
		},
		{
			name:     "low confidence match",
			cause:    titles.ErrLowConfidence,
			category: models.ErrorCategoryMediaNotFound,
			message:  "media not found",
		},
		{
			name:     "run zapscript disabled",
			cause:    state.ErrRunZapScriptDisabled,
			category: models.ErrorCategoryDisabled,
			message:  "ZapScript execution is disabled",
		},
		{
			name:     "parse failure",
			cause:    fmt.Errorf("failed to parse script: %w: parse error at 3", zapscript.ErrInvalidScript),
			category: models.ErrorCategoryInvalidScript,
			message:  "ZapScript is invalid",
		},
		{
			name:     "unknown command",
			cause:    fmt.Errorf("%w: nonexistent.cmd", zapscript.ErrUnknownCommand),
			category: models.ErrorCategoryInvalidScript,
			message:  "ZapScript is invalid",
		},
		{
			name:     "unknown system",
			cause:    fmt.Errorf("%w: neo", systemdefs.ErrUnknownSystem),
			category: models.ErrorCategoryInvalidScript,
			message:  "ZapScript is invalid",
		},
		{
			name:     "invalid next action",
			cause:    state.ErrInvalidNextAction,
			category: models.ErrorCategoryInvalidScript,
			message:  "ZapScript is invalid",
		},
		{
			name:     "command blocked",
			cause:    fmt.Errorf("%w: shell", zapscript.ErrCommandBlocked),
			category: models.ErrorCategoryBlocked,
			message:  "ZapScript execution was blocked",
		},
		{
			name:     "hook blocked launch",
			cause:    fmt.Errorf("%w: %w", state.ErrLaunchBlockedByHook, errors.New("hook exit 1")),
			category: models.ErrorCategoryBlocked,
			message:  "ZapScript execution was blocked",
		},
		{
			name:     "profile required",
			cause:    state.ErrLaunchRequiresProfile,
			category: models.ErrorCategoryBlocked,
			message:  "ZapScript execution was blocked",
		},
		{
			name:     "playtime limit",
			cause:    fmt.Errorf("%w: daily (2h0m0s / 1h0m0s)", playtime.ErrLimitReached),
			category: models.ErrorCategoryPlaytimeLimit,
			message:  "playtime limit reached",
		},
		{
			name:     "unclassified failure",
			cause:    errors.New("launcher exploded while opening " + leakedPath),
			category: models.ErrorCategoryExecutionFailed,
			message:  "ZapScript execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newRunTestEnv(t)
			out := startRun(env.requestEnv(context.Background(), "**launch:"+leakedPath))
			tok := env.receiveToken(t)
			require.True(t, tok.Completion.Complete(tt.cause))

			o := waitRun(t, out)
			assert.Nil(t, o.result)
			requireCategory(t, o.err, tt.category)
			assert.Equal(t, tt.message, o.err.Error())
			assert.NotContains(t, o.err.Error(), leakedPath)
			require.ErrorIs(t, o.err, tt.cause, "cause must stay reachable for logging")
		})
	}
}

func TestHandleRunCancelledWhileWaiting(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := startRun(env.requestEnv(ctx, "**launch.system:snes"))
	tok := env.receiveToken(t)

	cancel()
	o := waitRun(t, out)
	requireCategory(t, o.err, models.ErrorCategoryCancelled)
	require.ErrorIs(t, o.err, context.Canceled)

	// The worker finishing later must neither block nor be double-counted.
	assert.True(t, tok.Completion.Complete(nil))
	assert.False(t, tok.Completion.Complete(nil))
}

func TestHandleRunTimesOutWhileWaiting(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	out := startRun(env.requestEnv(ctx, "**launch.system:snes"))
	tok := env.receiveToken(t)

	o := waitRun(t, out)
	requireCategory(t, o.err, models.ErrorCategoryTimeout)
	require.ErrorIs(t, o.err, context.DeadlineExceeded)

	assert.True(t, tok.Completion.Complete(nil))
}

func TestHandleRunReturnsUnavailableOnShutdown(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	// Production derives the request context from the service context, so a
	// shutdown cancels the request too; the handler must report shutdown,
	// not a plain cancellation.
	ctx, cancel := context.WithCancel(env.st.GetContext())
	defer cancel()
	out := startRun(env.requestEnv(ctx, "**launch.system:snes"))
	tok := env.receiveToken(t)

	env.st.StopService()
	o := waitRun(t, out)
	requireCategory(t, o.err, models.ErrorCategoryUnavailable)
	require.ErrorIs(t, o.err, context.Canceled)

	assert.True(t, tok.Completion.Complete(errors.New("service shutting down")))
}

func TestHandleRunReturnsUnavailableWhenShutdownPreemptsQueueing(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	env.st.StopService()
	// Nobody reads the queue, so only the cancelled context can release run.
	o := waitRun(t, startRun(env.requestEnv(env.st.GetContext(), "**launch.system:snes")))
	requireCategory(t, o.err, models.ErrorCategoryUnavailable)
}

func TestHandleRunPrefersCompletedResultOverCancellation(t *testing.T) {
	t.Parallel()

	env := newRunTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := startRun(env.requestEnv(ctx, "**launch.system:snes"))
	tok := env.receiveToken(t)

	require.True(t, tok.Completion.Complete(nil))
	cancel()

	o := waitRun(t, out)
	require.NoError(t, o.err, "a finished script must not be reported as cancelled")
	assert.Equal(t, NoContent{}, o.result)
}
