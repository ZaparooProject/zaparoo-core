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

package sessionevidence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// seeded is what BeginExternalSession always synthesizes at requestedMs from
// the host's dispatch-time snapshot: screen and keyguard state before the
// target itself is ever confirmed foreground.
func seeded(ts int64, interactive, unlocked bool, rest ...ForegroundEvent) []ForegroundEvent {
	events := make([]ForegroundEvent, 0, len(rest)+2)
	if interactive {
		events = append(events, ForegroundEvent{Kind: ScreenInteractive, TimestampMs: ts})
	}
	if unlocked {
		events = append(events, ForegroundEvent{Kind: KeyguardHidden, TimestampMs: ts})
	}
	return append(events, rest...)
}

func TestReconcileForegroundConfirmsAndClosesOnOtherResumed(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 5000},
	)
	result, err := ReconcileForeground(1000, 6000, 0, events)
	require.NoError(t, err)
	require.True(t, result.Confirmed)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 5000}}, result.Segments)
	require.Equal(t, int64(4000), result.ClosedDurationMs())
}

func TestReconcileForegroundPauseResumeWithinGraceStaysOneSegment(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: TargetPaused, TimestampMs: 5000},
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 5000 + ForegroundGraceMs - 1},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 9000},
	)
	result, err := ReconcileForeground(1000, 10000, 0, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Len(t, result.Segments, 1, "a same-package pause/resume inside grace must not fragment the segment")
	require.Equal(t, int64(1000), result.Segments[0].StartMs)
	require.Equal(t, int64(9000), result.Segments[0].EndMs)
}

func TestReconcileForegroundPauseExpiringGraceClosesAtThePause(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: TargetPaused, TimestampMs: 5000},
	)
	result, err := ReconcileForeground(1000, 5000+ForegroundGraceMs+1, 0, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 5000}}, result.Segments,
		"grace expiry closes at the pause instant, not the later query end")
}

func TestReconcileForegroundExcludesScreenOffAndKeyguardIntervals(t *testing.T) {
	t.Parallel()
	// A real screen-off or keyguard-shown interruption also passes/stops the
	// foreground activity, so its return is a fresh target_resumed - the
	// state machine never infers "still playing" from the screen alone.
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: ScreenNonInteractive, TimestampMs: 2000},
		ForegroundEvent{Kind: ScreenInteractive, TimestampMs: 4000},
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 4000},
		ForegroundEvent{Kind: KeyguardShown, TimestampMs: 5000},
		ForegroundEvent{Kind: KeyguardHidden, TimestampMs: 6000},
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 6000},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 7000},
	)
	result, err := ReconcileForeground(1000, 8000, 0, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{
		{StartMs: 1000, EndMs: 2000},
		{StartMs: 4000, EndMs: 5000},
		{StartMs: 6000, EndMs: 7000},
	}, result.Segments, "screen-off and keyguard time must not be counted as played")
}

func TestReconcileForegroundOtherResumedDoesNotRevive(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 2000},
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 3000},
	)
	result, err := ReconcileForeground(1000, 4000, 0, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status,
		"a target resume after another app closed the session must not reopen it without a new Zaparoo launch")
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 2000}}, result.Segments)
}

func TestReconcileForegroundOutOfOrderEventIsRejected(t *testing.T) {
	t.Parallel()
	events := []ForegroundEvent{
		{Kind: TargetResumed, TimestampMs: 2000},
		{Kind: OtherResumed, TimestampMs: 1000},
	}
	_, err := ReconcileForeground(1000, 3000, 0, events)
	require.Error(t, err)
}

func TestReconcileForegroundDuplicateEventIsIdempotent(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 2000},
	)
	result, err := ReconcileForeground(1000, 3000, 0, events)
	require.NoError(t, err)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 2000}}, result.Segments)
}

func TestReconcileForegroundUnconfirmedNeverOpensASegment(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true)
	result, err := ReconcileForeground(1000, 5000, 0, events)
	require.NoError(t, err)
	require.False(t, result.Confirmed)
	require.Empty(t, result.Segments)
	require.Equal(t, "pending", result.Status)
}

func TestReconcileForegroundLockedOrScreenOffNeverOpensASegment(t *testing.T) {
	t.Parallel()
	events := []ForegroundEvent{{Kind: TargetResumed, TimestampMs: 1000}}
	result, err := ReconcileForeground(1000, 5000, 0, events)
	require.NoError(t, err)
	require.True(t, result.Confirmed, "a target resume alone confirms the launch")
	require.Equal(t, "suspended", result.Status, "with no interactive, unlocked screen it cannot be timed as playing")
	require.Empty(t, result.Segments)
	require.Zero(t, result.OpenStartMs)
}

func TestReconcileForegroundStillOpenAtQueryEndIsNotClosed(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true, ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000})
	result, err := ReconcileForeground(1000, 5000, 0, events)
	require.NoError(t, err)
	require.Equal(t, "active", result.Status)
	require.Empty(t, result.Segments, "an open interval is diagnostic only, never a proven closed segment")
	require.Equal(t, int64(1000), result.OpenStartMs)
}

func TestReconcileForegroundHardCloseClosesAnOpenSegmentAtTheBound(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true, ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000})
	result, err := ReconcileForeground(1000, 4000, 4000, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 4000}}, result.Segments,
		"a later recorded launch proves this target ended by the supersede bound")
	require.Zero(t, result.OpenStartMs)
	require.Equal(t, int64(4000), result.ClosedMs)
}

func TestReconcileForegroundHardCloseWithinGraceClosesAtThePause(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: TargetPaused, TimestampMs: 3000},
	)
	result, err := ReconcileForeground(1000, 3500, 3500, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 3000}}, result.Segments)
	require.Equal(t, int64(3000), result.ClosedMs, "the proven end is the pause, not the later supersede bound")
}

func TestReconcileForegroundHardCloseOnUnconfirmedAbandons(t *testing.T) {
	t.Parallel()
	result, err := ReconcileForeground(1000, 2000, 2000, nil)
	require.NoError(t, err)
	require.Equal(t, "abandoned", result.Status)
	require.False(t, result.Confirmed)
	require.Empty(t, result.Segments)
}

func TestReconcileForegroundHardCloseAlreadyClosedIsUnchanged(t *testing.T) {
	t.Parallel()
	events := seeded(1000, true, true,
		ForegroundEvent{Kind: TargetResumed, TimestampMs: 1000},
		ForegroundEvent{Kind: OtherResumed, TimestampMs: 1500},
	)
	result, err := ReconcileForeground(1000, 4000, 4000, events)
	require.NoError(t, err)
	require.Equal(t, "closed", result.Status)
	require.Equal(t, []ForegroundSegment{{StartMs: 1000, EndMs: 1500}}, result.Segments,
		"a session that already closed naturally must not be re-extended to the hard-close bound")
}

func TestReconcileForegroundInvalidBoundsAreRejected(t *testing.T) {
	t.Parallel()
	_, err := ReconcileForeground(0, 1000, 0, nil)
	require.Error(t, err)
	_, err = ReconcileForeground(2000, 1000, 0, nil)
	require.Error(t, err)
	_, err = ReconcileForeground(2000, 3000, 1000, nil)
	require.Error(t, err, "a supersede bound before the launch itself is invalid")
}
