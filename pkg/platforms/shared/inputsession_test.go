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

package shared

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingGamepad struct {
	events []keyEvent
	closed bool
}

func (r *recordingGamepad) ButtonDown(code int) error {
	r.events = append(r.events, keyEvent{kind: "down", code: code})
	return nil
}

func (r *recordingGamepad) ButtonUp(code int) error {
	r.events = append(r.events, keyEvent{kind: "up", code: code})
	return nil
}

func (r *recordingGamepad) Close() error {
	r.closed = true
	return nil
}

func newRecordingInputDevices() (*InputManager, *recordingKeyboard, *recordingGamepad) {
	keyboard := &recordingKeyboard{}
	gamepad := &recordingGamepad{}
	input := &InputManager{
		kbd: keyboard,
		gpd: gamepad,
	}
	return input, keyboard, gamepad
}

func TestInputSession_KeyboardPersistsAcrossRequests(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	assert.Equal(t, []keyEvent{{kind: "down", code: 103}}, keyboard.events)

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{release:up}"}, 0))
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 103},
		{kind: "up", code: 103},
	}, keyboard.events)
}

func TestInputSession_KeyboardTracksMultipleHeldKeys(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{
		"{press:up}",
		"{press:left}",
	}, 0))
	assert.ElementsMatch(t, []keyEvent{
		{kind: "down", code: 103},
		{kind: "down", code: 105},
	}, keyboard.events)

	require.NoError(t, session.ReleaseAll())
	assert.Len(t, keyboard.events, 4)
	assert.ElementsMatch(t, []keyEvent{
		{kind: "up", code: 103},
		{kind: "up", code: 105},
	}, keyboard.events[2:])
}

func TestInputSession_KeyboardIsolationUsesReferenceCounts(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	first := input.NewInputSession()
	second := input.NewInputSession()

	require.NoError(t, first.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	require.NoError(t, second.KeyboardPressSequence(t.Context(), []string{"{release:up}"}, 0))
	assert.Equal(t, []keyEvent{{kind: "down", code: 103}}, keyboard.events,
		"one session must not release another session's key")

	require.NoError(t, second.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	require.NoError(t, second.KeyboardPressSequence(t.Context(), []string{"{release:up}"}, 0))
	assert.Equal(t, []keyEvent{{kind: "down", code: 103}}, keyboard.events,
		"physical key remains down while first session owns it")

	require.NoError(t, first.KeyboardPressSequence(t.Context(), []string{"{release:up}"}, 0))
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 103},
		{kind: "up", code: 103},
	}, keyboard.events)
}

func pressKeys(t *testing.T, session platforms.InputSession, keys ...string) {
	t.Helper()
	require.NoError(t, session.KeyboardPressSequence(t.Context(), keys, 0))
}

func TestInputSession_ShiftedKeyHoldsShift(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:M}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 50},
	}, keyboard.events)

	pressKeys(t, session, "{release:M}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 50},
		{kind: "up", code: 42},
	}, keyboard.events[2:])
}

func TestInputSession_ShiftStaysDownWhileAnyHoldNeedsIt(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:M}", "{press:!}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 50},
		{kind: "down", code: 2},
	}, keyboard.events)

	pressKeys(t, session, "{release:M}")
	assert.Equal(t, []keyEvent{{kind: "up", code: 50}}, keyboard.events[3:],
		"shift is still needed by the other held key")

	pressKeys(t, session, "{release:!}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 2},
		{kind: "up", code: 42},
	}, keyboard.events[4:])
}

func TestInputSession_ExplicitShiftSurvivesShiftedKeyRelease(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:shift}", "{press:M}", "{release:M}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 50},
		{kind: "up", code: 50},
	}, keyboard.events)

	pressKeys(t, session, "{release:shift}")
	assert.Equal(t, keyEvent{kind: "up", code: 42}, keyboard.events[3])
}

func TestInputSession_ComboPressAndRelease(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:ctrl+c}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 29},
		{kind: "down", code: 46},
	}, keyboard.events)

	pressKeys(t, session, "{release:ctrl+c}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 46},
		{kind: "up", code: 29},
	}, keyboard.events[2:])
}

func TestInputSession_ComboReleaseIgnoresKeyOrder(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:ctrl+shift+a}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 29},
		{kind: "down", code: 42},
		{kind: "down", code: 30},
	}, keyboard.events)

	pressKeys(t, session, "{release:shift+ctrl+a}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 30},
		{kind: "up", code: 42},
		{kind: "up", code: 29},
	}, keyboard.events[3:], "a release naming the same keys in another order matches the hold")
}

func TestInputSession_ShiftedComboReleaseIgnoresKeyOrder(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:ctrl+A}", "{release:a+shift+ctrl}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 29},
		{kind: "down", code: 42},
		{kind: "down", code: 30},
		{kind: "up", code: 30},
		{kind: "up", code: 42},
		{kind: "up", code: 29},
	}, keyboard.events)
}

func TestInputSession_ReorderedPressMatchesExistingHold(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:ctrl+c}", "{press:c+ctrl}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 29},
		{kind: "down", code: 46},
	}, keyboard.events, "the reordered press names a hold the session already owns")

	pressKeys(t, session, "{release:ctrl+c}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 46},
		{kind: "up", code: 29},
	}, keyboard.events[2:], "one release ends the hold")
}

func TestInputSession_ShiftComboMatchesShiftedCharacter(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:A}", "{press:shift+a}")
	assert.Len(t, keyboard.events, 2, "both tokens name the same hold")

	pressKeys(t, session, "{release:shift+a}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 30},
		{kind: "up", code: 30},
		{kind: "up", code: 42},
	}, keyboard.events)
}

// TestInputSession_HoldSetModelMatchesDevice replays random press and release
// tokens against a model that treats a hold as the set of keys it names, and
// checks the virtual keyboard ends up holding exactly the keys the model says.
// It is the general form of the ordering cases above: a press or release that
// names keys in another order must land on the same hold.
// comboTokens builds count distinct hold tokens, each naming a different pair
// of keys, so a session asked to hold them all keeps count separate holds.
func comboTokens(t *testing.T, action string, count int) []string {
	t.Helper()

	letters := "abcdefghijklmnopqrstuvwxyz"
	tokens := make([]string, 0, count)
	for i := range len(letters) {
		for j := i + 1; j < len(letters); j++ {
			if len(tokens) == count {
				return tokens
			}
			tokens = append(tokens, fmt.Sprintf("{%s:%c+%c}", action, letters[i], letters[j]))
		}
	}
	require.Len(t, tokens, count, "not enough distinct pairs")
	return tokens
}

// TestInputSession_HoldsAreBounded verifies one session cannot accumulate
// unbounded hold state. A combo names a set of keys, so without a cap a single
// request could turn a few KB of macro into tens of MB retained for the life
// of the WebSocket.
func TestInputSession_HoldsAreBounded(t *testing.T) {
	t.Parallel()

	input, _, _ := newRecordingInputDevices()
	session, ok := input.NewInputSession().(*inputSession)
	require.True(t, ok)

	tokens := comboTokens(t, "press", maxSessionKeyboardHolds+1)
	err := input.keyboardPressSequenceLocked(t.Context(), tokens, time.Nanosecond, session)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key combinations")

	input.inputMu.Lock()
	holds := len(input.inputSessions[session].keyboardHolds)
	input.inputMu.Unlock()
	assert.Equal(t, maxSessionKeyboardHolds, holds, "the hold that hit the cap was not stored")

	require.NoError(t, session.ReleaseAll())
}

// TestInputSession_ReleasedHoldFreesItsSlot verifies the cap tracks what is
// held now, not how many holds a session has ever taken.
func TestInputSession_ReleasedHoldFreesItsSlot(t *testing.T) {
	t.Parallel()

	input, _, _ := newRecordingInputDevices()
	session, ok := input.NewInputSession().(*inputSession)
	require.True(t, ok)

	presses := comboTokens(t, "press", maxSessionKeyboardHolds)
	require.NoError(t, input.keyboardPressSequenceLocked(t.Context(), presses, time.Nanosecond, session))
	require.NoError(t, input.keyboardPressSequenceLocked(
		t.Context(), comboTokens(t, "release", 1), time.Nanosecond, session))

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{press:ctrl+shift+f9}"}, 0))
	require.NoError(t, session.ReleaseAll())
}

func TestInputSession_HoldSetModelMatchesDevice(t *testing.T) {
	t.Parallel()

	tokens := []string{
		"M", "!", "m", "shift", "ctrl+c", "c", "ctrl", "shift+a", "A", "a",
		"ctrl+shift+a", "shift+ctrl+a", "a+ctrl", "rshift+a", "ctrl+A",
		"ctrl+plus", "f4", "alt", "up", "1", "shift+1",
	}
	canonical := func(codes []int) string {
		sorted := slices.Clone(codes)
		slices.Sort(sorted)
		return fmt.Sprint(sorted)
	}

	rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic test input
	for iter := range 2000 {
		input, keyboard, _ := newRecordingInputDevices()
		sessions := []platforms.InputSession{input.NewInputSession(), input.NewInputSession()}
		holds := []map[string][]int{{}, {}}

		var seq []string
		for range 1 + rng.IntN(10) {
			token := tokens[rng.IntN(len(tokens))]
			action := "press"
			if rng.IntN(3) == 0 {
				action = "release"
			}
			owner := rng.IntN(len(sessions))
			macro := fmt.Sprintf("{%s:%s}", action, token)
			seq = append(seq, fmt.Sprintf("session %d %s", owner, macro))

			codes, err := resolveHoldKeyCodes(token)
			require.NoError(t, err)
			require.NoError(t, sessions[owner].KeyboardPressSequence(t.Context(), []string{macro}, 0),
				"iteration %d: %v", iter, seq)

			id := canonical(codes)
			if action == "press" {
				if _, held := holds[owner][id]; !held {
					holds[owner][id] = codes
				}
				continue
			}
			delete(holds[owner], id)
		}

		want := map[int]bool{}
		for _, sessionHolds := range holds {
			for _, codes := range sessionHolds {
				for _, code := range codes {
					want[code] = true
				}
			}
		}
		got := map[int]bool{}
		for code, balance := range keyEventBalance(keyboard.events) {
			require.GreaterOrEqual(t, balance, 0, "iteration %d: %v gives %v", iter, seq, keyboard.events)
			require.LessOrEqual(t, balance, 1, "iteration %d: %v gives %v", iter, seq, keyboard.events)
			if balance == 1 {
				got[code] = true
			}
		}
		require.Equal(t, want, got, "iteration %d: %v gives %v", iter, seq, keyboard.events)
	}
}

// keyEventBalance reduces a recorded event list to how many times each key is
// still down.
func keyEventBalance(events []keyEvent) map[int]int {
	balance := make(map[int]int)
	for _, event := range events {
		if event.kind == "down" {
			balance[event.code]++
			continue
		}
		balance[event.code]--
	}
	return balance
}

func TestInputSession_RepeatedShiftedPressIsReleasedOnce(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:M}", "{press:M}", "{release:M}")
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 50},
		{kind: "up", code: 50},
		{kind: "up", code: 42},
	}, keyboard.events)
}

func TestInputSession_UnshiftedReleaseLeavesShiftedHold(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:M}", "{release:m}")
	assert.Len(t, keyboard.events, 2, "m was never held on its own")
}

func TestInputSession_ShiftedHoldSharedBetweenSessions(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	first := input.NewInputSession()
	second := input.NewInputSession()

	pressKeys(t, first, "{press:M}")
	pressKeys(t, second, "{press:M}")
	pressKeys(t, first, "{release:M}")
	assert.Len(t, keyboard.events, 2, "second session still holds both keys")

	pressKeys(t, second, "{release:M}")
	assert.Equal(t, []keyEvent{
		{kind: "up", code: 50},
		{kind: "up", code: 42},
	}, keyboard.events[2:])
}

func TestInputSession_FailedHoldRollsBackItsModifier(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	keyboard.failOnCode = 50
	session, ok := input.NewInputSession().(*inputSession)
	require.True(t, ok)

	input.inputMu.Lock()
	err := input.sessionKeyboardDownLocked(session, []int{42, 50})
	sessions := len(input.inputSessions)
	input.inputMu.Unlock()

	require.Error(t, err)
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "up", code: 42},
	}, keyboard.events)
	assert.Zero(t, sessions)
}

func TestInputSession_FailedRollbackKeepsSharedModifierForOtherHold(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:!}")
	keyboard.failOnCode = 50
	keyboard.failOnce = true
	require.Error(t, session.KeyboardPressSequence(t.Context(), []string{"{press:M}"}, 0))

	assert.ElementsMatch(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 2},
		{kind: "up", code: 2},
		{kind: "up", code: 42},
	}, keyboard.events, "a failed request releases everything the session holds, once")
}

func TestInputSession_ReleaseAllReleasesShiftedHolds(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	pressKeys(t, session, "{press:M}", "{press:!}", "{press:ctrl+c}")
	require.NoError(t, session.ReleaseAll())

	assert.Len(t, keyboard.events, 10)
	assert.ElementsMatch(t, []keyEvent{
		{kind: "up", code: 42},
		{kind: "up", code: 50},
		{kind: "up", code: 2},
		{kind: "up", code: 29},
		{kind: "up", code: 46},
	}, keyboard.events[5:])
}

func TestInputSession_FailedShiftReleaseIsRetried(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()
	pressKeys(t, session, "{press:M}")
	keyboard.failUpOnCode = 42
	keyboard.failUpOnce = true

	require.Error(t, session.ReleaseAll())
	require.NoError(t, session.ReleaseAll())
	assert.ElementsMatch(t, []keyEvent{
		{kind: "down", code: 42},
		{kind: "down", code: 50},
		{kind: "up", code: 50},
		{kind: "up", code: 42},
	}, keyboard.events)
}

func TestInputSession_RequestScopedPressDoesNotReleasePersistentKey(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	require.NoError(t, input.KeyboardPress("{up}"))
	assert.Equal(t, []keyEvent{{kind: "down", code: 103}}, keyboard.events)

	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{release:up}"}, 0))
	assert.Equal(t, keyEvent{kind: "up", code: 103}, keyboard.events[1])
}

func TestInputSession_KeyboardErrorReleasesHeldInput(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()

	err := session.KeyboardPressSequence(t.Context(), []string{
		"{press:a}",
		"{not-a-key}",
	}, 0)
	require.Error(t, err)
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 30},
		{kind: "up", code: 30},
	}, keyboard.events)
}

func TestInputSession_CancellationReleasesHeldInput(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	keyboard.keyDownSignal = make(chan int, 1)
	session := input.NewInputSession()
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.KeyboardPressSequence(ctx, []string{
			"{press:a}",
			"{delay:30s}",
		}, 0)
	}()

	assert.Equal(t, 30, <-keyboard.keyDownSignal)
	cancel()
	err := <-errCh
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 30},
		{kind: "up", code: 30},
	}, keyboard.events)
}

func TestInputSession_PreCanceledRequestDoesNotPressInput(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := session.KeyboardPressSequence(ctx, []string{"{press:a}"}, 0)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, keyboard.events)
}

func TestInputSession_ReleaseNotBlockedByAnotherSessionDelay(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	keyboard.keyDownSignal = make(chan int, 2)
	first := input.NewInputSession()
	second := input.NewInputSession()
	require.NoError(t, first.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	assert.Equal(t, 103, <-keyboard.keyDownSignal)

	ctx, cancel := context.WithCancel(t.Context())
	sequenceErr := make(chan error, 1)
	go func() {
		sequenceErr <- second.KeyboardPressSequence(ctx, []string{
			"{press:b}",
			"{delay:30s}",
		}, 0)
	}()
	assert.Equal(t, 48, <-keyboard.keyDownSignal)

	releaseDone := make(chan error, 1)
	go func() {
		releaseDone <- first.ReleaseAll()
	}()
	select {
	case err := <-releaseDone:
		require.NoError(t, err)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("input release blocked behind another session's delay")
	}

	cancel()
	require.ErrorIs(t, <-sequenceErr, context.Canceled)
	assert.Contains(t, keyboard.events, keyEvent{kind: "up", code: 103})
	assert.Contains(t, keyboard.events, keyEvent{kind: "up", code: 48})
}

func TestInputSession_ClosedSessionRetriesFailedRelease(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	session := input.NewInputSession()
	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{press:a}"}, 0))
	keyboard.failUpOnCode = 30
	keyboard.failUpOnce = true

	require.Error(t, session.ReleaseAll())
	require.NoError(t, session.ReleaseAll())
	assert.Equal(t, []keyEvent{
		{kind: "down", code: 30},
		{kind: "up", code: 30},
	}, keyboard.events)
}

func TestCloseDevicesNotBlockedByRequestScopedDelay(t *testing.T) {
	t.Parallel()

	input, keyboard, _ := newRecordingInputDevices()
	keyboard.keyDownSignal = make(chan int, 1)
	sequenceDone := make(chan error, 1)
	go func() {
		sequenceDone <- input.KeyboardPressSequence([]string{
			"{press:a}",
			"{delay:300ms}",
		}, 0)
	}()
	assert.Equal(t, 30, <-keyboard.keyDownSignal)

	closeDone := make(chan struct{})
	go func() {
		input.CloseDevices()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("device shutdown blocked behind request-scoped delay")
	}

	require.NoError(t, <-sequenceDone)
	assert.True(t, keyboard.closed)
	assert.Contains(t, keyboard.events, keyEvent{kind: "up", code: 30})
}

func TestParseBoundedInputMacroDuration(t *testing.T) {
	t.Parallel()

	duration, err := parseBoundedInputMacroDuration(maxInputMacroDuration.String())
	require.NoError(t, err)
	assert.Equal(t, maxInputMacroDuration, duration)

	_, err = parseBoundedInputMacroDuration((maxInputMacroDuration + time.Millisecond).String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be between")

	_, err = parseBoundedInputMacroDuration("-1ms")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be between")
}

func TestInputSequencesRejectExcessiveDurations(t *testing.T) {
	t.Parallel()

	input, keyboard, gamepad := newRecordingInputDevices()

	err := input.KeyboardPressSequence([]string{"{delay:31s}"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid delay token")

	err = input.KeyboardPressSequence([]string{"{hold:a:31s}"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hold duration")
	assert.Empty(t, keyboard.events)

	err = input.GamepadPressSequence([]string{"{delay:31s}"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid delay token")

	err = input.GamepadPressSequence([]string{"{hold:start:31s}"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hold duration")
	assert.Empty(t, gamepad.events)
}

func TestInputSession_GamepadPersistsAcrossRequests(t *testing.T) {
	t.Parallel()

	input, _, gamepad := newRecordingInputDevices()
	session := input.NewInputSession()

	require.NoError(t, session.GamepadPressSequence(t.Context(), []string{"{press:up}"}, 0))
	require.Len(t, gamepad.events, 1)
	assert.Equal(t, "down", gamepad.events[0].kind)

	require.NoError(t, session.GamepadPressSequence(t.Context(), []string{"{release:up}"}, 0))
	require.Len(t, gamepad.events, 2)
	assert.Equal(t, gamepad.events[0].code, gamepad.events[1].code)
	assert.Equal(t, "up", gamepad.events[1].kind)
}

func TestInputSession_CloseDevicesReleasesBeforeClosing(t *testing.T) {
	t.Parallel()

	input, keyboard, gamepad := newRecordingInputDevices()
	session := input.NewInputSession()
	require.NoError(t, session.KeyboardPressSequence(t.Context(), []string{"{press:up}"}, 0))
	require.NoError(t, session.GamepadPressSequence(t.Context(), []string{"{press:start}"}, 0))

	input.CloseDevices()

	assert.Equal(t, "up", keyboard.events[len(keyboard.events)-1].kind)
	assert.Equal(t, "up", gamepad.events[len(gamepad.events)-1].kind)
	assert.True(t, keyboard.closed)
	assert.True(t, gamepad.closed)
	assert.Error(t, session.KeyboardPressSequence(t.Context(), []string{"{press:a}"}, 0))
}
