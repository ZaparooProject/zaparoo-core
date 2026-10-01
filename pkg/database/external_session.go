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

package database

import "context"

// ExternalSession is an immutable launch snapshot plus its durable dispatch
// state, for a launcher whose process Core cannot track directly
// (LifecycleExternal). A pending/dispatched session is not proof that the
// target was played.
type ExternalSession struct {
	ProfileID     *string
	MediaIdentity *MediaIdentity
	CursorMs      *int64
	DispatchedMs  *int64
	Status        string
	MediaName     string
	LauncherID    string
	MediaPath     string
	// Target is the whole component the host confirms is foreground - an
	// Android package - never a specific activity: a launcher whose intent
	// forwards through more than one activity of the same app must still
	// read as one continuous session.
	Target             string
	BootID             string
	Source             string
	LaunchID           string
	SystemName         string
	SystemID           string
	RequestedMs        int64
	RequestedElapsedMs int64
	InitialInteractive bool
	InitialUnlocked    bool
}

// ForegroundEvent contains only a launch target or an anonymous state
// transition. The host must never send identities of unrelated applications.
type ForegroundEvent struct {
	Activity    string `json:"targetActivity,omitempty"`
	Kind        string `json:"kind"`
	TimestampMs int64  `json:"timestampMs"`
	Sequence    int    `json:"sequence"`
}

// ForegroundEvidence is a complete, bounded host query for one launch.
// Denied/incomplete queries cannot advance the persisted evidence cursor.
type ForegroundEvidence struct {
	LaunchID          string            `json:"launchId"`
	BootID            string            `json:"bootId"`
	Permission        string            `json:"permission"`
	Events            []ForegroundEvent `json:"events"`
	QueryFromMs       int64             `json:"queryFromMs"`
	QueryToMs         int64             `json:"queryToMs"`
	ObservedElapsedMs int64             `json:"observedElapsedMs"`
	ObservedWallMs    int64             `json:"observedWallMs"`
	Version           int               `json:"version"`
	Complete          bool              `json:"complete"`
}

// ExternalSessionStore is the narrow optional UserDB capability a platform
// with LifecycleExternal launchers uses; other platforms and database test
// doubles need none of it.
type ExternalSessionStore interface {
	BeginExternalSession(context.Context, *ExternalSession) error
	RecordExternalDispatch(context.Context, string, int64) (bool, error)
	AbandonExternalSession(context.Context, string, int64) (bool, error)
	UnresolvedExternalSessions(context.Context) ([]ExternalSession, error)
	ApplyExternalEvidence(context.Context, *ForegroundEvidence) (bool, error)
	CloseExternalSessionApproximate(ctx context.Context, launchID string, startMs, endMs int64) (bool, error)
	MarkExternalSessionStale(context.Context, string, int64) (bool, error)
	// NextExternalLaunchElapsed returns the smallest RequestedElapsedMs over
	// every session in bootID requested after afterElapsedMs, regardless of
	// that session's own current status: an already-closed or stale later
	// launch still replaced the one being reconciled. Abandoned is the one
	// status excluded, since it means dispatch never happened and that
	// launch never actually replaced anything.
	NextExternalLaunchElapsed(
		ctx context.Context, bootID string, afterElapsedMs int64,
	) (elapsedMs int64, found bool, err error)
}
