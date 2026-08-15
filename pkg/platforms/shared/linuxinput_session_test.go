//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package shared

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/bendahl/uinput"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingGamepad struct {
	events []keyEvent
	closed bool
}

func (*recordingGamepad) ButtonPress(_ int) error { return nil }
func (r *recordingGamepad) ButtonDown(code int) error {
	r.events = append(r.events, keyEvent{kind: "down", code: code})
	return nil
}

func (r *recordingGamepad) ButtonUp(code int) error {
	r.events = append(r.events, keyEvent{kind: "up", code: code})
	return nil
}
func (*recordingGamepad) LeftStickMoveX(_ float32) error         { return nil }
func (*recordingGamepad) LeftStickMoveY(_ float32) error         { return nil }
func (*recordingGamepad) RightStickMoveX(_ float32) error        { return nil }
func (*recordingGamepad) RightStickMoveY(_ float32) error        { return nil }
func (*recordingGamepad) LeftStickMove(_, _ float32) error       { return nil }
func (*recordingGamepad) RightStickMove(_, _ float32) error      { return nil }
func (*recordingGamepad) HatPress(_ uinput.HatDirection) error   { return nil }
func (*recordingGamepad) HatRelease(_ uinput.HatDirection) error { return nil }
func (r *recordingGamepad) Close() error {
	r.closed = true
	return nil
}

func newRecordingInputDevices() (*LinuxInput, *recordingKeyboard, *recordingGamepad) {
	keyboard := &recordingKeyboard{}
	gamepad := &recordingGamepad{}
	input := &LinuxInput{
		kbd: linuxinput.Keyboard{Device: keyboard},
		gpd: linuxinput.Gamepad{Device: gamepad},
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
			"{delay:1h}",
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
			"{delay:1h}",
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
