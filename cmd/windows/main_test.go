//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	syswindows "golang.org/x/sys/windows"
)

func TestRestartAfterReleasing_ReleasesSingletonBeforeRestart(t *testing.T) {
	t.Parallel()

	var events []string
	instance := &singleInstance{
		handle: 1,
		closeHandle: func(syswindows.Handle) error {
			events = append(events, "release")
			return nil
		},
	}

	err := restartAfterReleasing(instance, func() error {
		events = append(events, "restart")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"release", "restart"}, events)
	assert.Zero(t, instance.handle)
}

func TestRestartAfterReleasing_DoesNotRestartWhenReleaseFails(t *testing.T) {
	t.Parallel()

	releaseErr := errors.New("close failed")
	restarted := false
	instance := &singleInstance{
		handle:      1,
		closeHandle: func(syswindows.Handle) error { return releaseErr },
	}

	err := restartAfterReleasing(instance, func() error {
		restarted = true
		return nil
	})

	require.ErrorIs(t, err, releaseErr)
	assert.False(t, restarted)
	assert.NotZero(t, instance.handle)
}
