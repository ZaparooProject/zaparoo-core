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

// Package sessionevidence turns a host's privacy-filtered foreground/screen/
// keyguard observations into proven, closed play intervals. It has no
// database or platform dependency: every rule here is pure so it can be
// replayed and tested without a store.
package sessionevidence

import (
	"errors"
	"fmt"
)

// ForegroundKind is the privacy-filtered vocabulary of host observations.
// It carries no unrelated package identity.
type ForegroundKind string

const (
	TargetResumed        ForegroundKind = "target_resumed"
	TargetPaused         ForegroundKind = "target_paused"
	TargetStopped        ForegroundKind = "target_stopped"
	OtherResumed         ForegroundKind = "other_resumed"
	ScreenInteractive    ForegroundKind = "screen_interactive"
	ScreenNonInteractive ForegroundKind = "screen_non_interactive"
	KeyguardShown        ForegroundKind = "keyguard_shown"
	KeyguardHidden       ForegroundKind = "keyguard_hidden"
)

// ForegroundGraceMs is the same-package pause/resume grace period: an
// emulator's own menu or a brief activity transition does not fragment a
// session. It is a versioned policy constant, not catalog data.
const ForegroundGraceMs int64 = 2000

// PolicyVersion is stamped on every segment this package produces, so a
// later change to the grace period or state machine never reinterprets
// already-closed intervals.
const PolicyVersion = 1

// ForegroundEvent is already filtered by the host to one target package
// (never a specific activity - a launch whose intent forwards through more
// than one activity of the same app must still read as one session) plus
// anonymous screen/keyguard transitions. Events must be supplied in query
// (timestamp) order.
type ForegroundEvent struct {
	Kind        ForegroundKind
	TimestampMs int64
}

type ForegroundSegment struct {
	StartMs int64
	EndMs   int64
}

// ForegroundResult contains only proven, closed play intervals. OpenStartMs
// is diagnostic state and must not be synced as completed exact playtime.
type ForegroundResult struct {
	Status      string
	Segments    []ForegroundSegment
	OpenStartMs int64
	ConfirmedMs int64
	ClosedMs    int64
	Confirmed   bool
}

func (r ForegroundResult) ClosedDurationMs() int64 {
	var duration int64
	for _, segment := range r.Segments {
		duration += segment.EndMs - segment.StartMs
	}
	return duration
}

// ReconcileForeground replays retained normalized observations from a
// launch. Replaying the same overlapping batch is idempotent. Missing
// screen/keyguard state never becomes evidence that gameplay was visible.
//
// hardCloseAtMs is 0, or the timestamp another recorded launch proved this
// one's target could no longer be current: unlike queryEndMs, which only
// says how far Core has looked so far and is never proof of an end, this is
// proof, so any interval still open at that point is force-closed there
// instead of staying an unconfirmed diagnostic guess.
func ReconcileForeground(
	requestedMs, queryEndMs, hardCloseAtMs int64, events []ForegroundEvent,
) (ForegroundResult, error) {
	if requestedMs <= 0 || queryEndMs < requestedMs {
		return ForegroundResult{}, errors.New("invalid foreground query bounds")
	}
	if hardCloseAtMs != 0 && hardCloseAtMs < requestedMs {
		return ForegroundResult{}, errors.New("invalid foreground supersede bound")
	}
	result := ForegroundResult{Status: "pending"}
	interactive, unlocked, target := false, false, false
	var opened, pausedAt int64
	lastTimestamp := requestedMs
	seen := make(map[ForegroundEvent]struct{}, len(events))
	closeSegment := func(end int64) {
		if opened > 0 && end > opened {
			result.Segments = append(result.Segments, ForegroundSegment{StartMs: opened, EndMs: end})
		}
		opened = 0
	}
	openSegment := func(at int64) {
		if target && interactive && unlocked && opened == 0 && pausedAt == 0 && result.Status != "closed" {
			opened = at
			result.Status = "active"
		}
	}
	for _, event := range events {
		if event.TimestampMs < lastTimestamp || event.TimestampMs > queryEndMs {
			return ForegroundResult{}, fmt.Errorf("invalid or out-of-order foreground event at %d", event.TimestampMs)
		}
		if _, duplicate := seen[event]; duplicate {
			continue
		}
		seen[event] = struct{}{}
		lastTimestamp = event.TimestampMs
		if pausedAt > 0 && event.TimestampMs-pausedAt > ForegroundGraceMs {
			closeSegment(pausedAt)
			result.Status = "closed"
			result.ClosedMs = pausedAt
			pausedAt, target = 0, false
		}
		if result.Status == "closed" {
			continue
		}
		switch event.Kind {
		case TargetResumed:
			if !result.Confirmed {
				result.ConfirmedMs = event.TimestampMs
			}
			result.Confirmed = true
			pausedAt, target = 0, true
			if interactive && unlocked {
				result.Status = "active"
			} else {
				result.Status = "suspended"
			}
			openSegment(event.TimestampMs)
		case TargetPaused, TargetStopped:
			if target && pausedAt == 0 {
				pausedAt = event.TimestampMs
			}
		case OtherResumed:
			if result.Confirmed {
				if pausedAt > 0 {
					closeSegment(pausedAt)
				} else {
					closeSegment(event.TimestampMs)
				}
				result.Status = "closed"
				result.ClosedMs = event.TimestampMs
				if pausedAt > 0 {
					result.ClosedMs = pausedAt
				}
			}
			target, pausedAt = false, 0
		case ScreenInteractive:
			interactive = true
			openSegment(event.TimestampMs)
		case ScreenNonInteractive:
			interactive, target = false, false
			if pausedAt > 0 {
				closeSegment(pausedAt)
				pausedAt, target = 0, false
			} else {
				closeSegment(event.TimestampMs)
			}
			if result.Confirmed {
				result.Status = "suspended"
			}
		case KeyguardShown:
			unlocked, target = false, false
			if pausedAt > 0 {
				closeSegment(pausedAt)
				pausedAt, target = 0, false
			} else {
				closeSegment(event.TimestampMs)
			}
			if result.Confirmed {
				result.Status = "suspended"
			}
		case KeyguardHidden:
			unlocked = true
			openSegment(event.TimestampMs)
		default:
			return ForegroundResult{}, fmt.Errorf("unknown foreground event %q", event.Kind)
		}
	}
	if pausedAt > 0 && queryEndMs-pausedAt > ForegroundGraceMs {
		closeSegment(pausedAt)
		result.Status = "closed"
		result.ClosedMs = pausedAt
		pausedAt = 0
	}
	result.OpenStartMs = opened
	if hardCloseAtMs != 0 && result.Status != "closed" {
		if !result.Confirmed {
			result.Status = "abandoned"
			return result, nil
		}
		switch {
		case pausedAt > 0:
			// Paused-but-not-yet-timed-out is an unresolved gap, not proof
			// of continued foreground; only a later resume could bridge it,
			// and a supersede is never that. The session's proven end is the
			// pause, not the later bound that merely revealed it.
			closeSegment(pausedAt)
			result.ClosedMs = pausedAt
		case opened > 0:
			closeSegment(hardCloseAtMs)
			result.ClosedMs = hardCloseAtMs
		default:
			result.ClosedMs = hardCloseAtMs
		}
		result.Status = "closed"
		result.OpenStartMs = 0
	}
	return result, nil
}
