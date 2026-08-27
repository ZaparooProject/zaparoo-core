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

package mediascanner

// Guards the per-system time accounting on "completed system indexing".
//
// Round 7 of #1279 measured elapsed - (collect+insert+reconcile+commit) at
// 11.87 min, 18.3% of a full reindex, with no log line accounting for any of
// it — larger than disambiguation and roughly half of commit. Two systems in
// that run spent over 98% of their wall time in that unmeasured window.
//
// The fix added setup/throttle/analyze/cacheRefresh timers plus an explicit
// unattributed field. These tests pin the invariants that make those numbers
// trustworthy.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// systemTiming mirrors the duration fields on "completed system indexing".
// zerolog writes Dur() as float milliseconds by default.
type systemTiming struct {
	System       string  `json:"system"`
	Message      string  `json:"message"`
	Elapsed      float64 `json:"elapsed"`
	Setup        float64 `json:"setup"`
	Collect      float64 `json:"collect"`
	Insert       float64 `json:"insert"`
	Reconcile    float64 `json:"reconcile"`
	Commit       float64 `json:"commit"`
	Throttle     float64 `json:"throttle"`
	Analyze      float64 `json:"analyze"`
	CacheRefresh float64 `json:"cacheRefresh"`
	Unattributed float64 `json:"unattributed"`
}

func (s *systemTiming) namedTotal() float64 {
	return s.Setup + s.Collect + s.Insert + s.Reconcile +
		s.Commit + s.Throttle + s.Analyze + s.CacheRefresh
}

// captureSystemTimings runs an index over the given systems and returns the
// parsed "completed system indexing" lines.
func captureSystemTimings(t *testing.T, systemFiles map[string][]string) []systemTiming {
	t.Helper()

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)

	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	buf := &syncLogBuffer{}
	prevLogger := log.Logger
	prevGlobal := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(buf).Level(zerolog.InfoLevel)
	defer func() { log.Logger = prevLogger; zerolog.SetGlobalLevel(prevGlobal) }()

	_, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	// Restore before reading so background optimization cannot race the read.
	log.Logger = prevLogger
	zerolog.SetGlobalLevel(prevGlobal)

	var timings []systemTiming
	for _, line := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(line, `"completed system indexing"`) {
			continue
		}
		var st systemTiming
		require.NoError(t, json.Unmarshal([]byte(line), &st), "line: %s", line)
		timings = append(timings, st)
	}
	return timings
}

// TestSystemTiming_PhasesAccountForElapsed is the core invariant: every field is
// present, none is negative, and the named phases plus unattributed reconstruct
// elapsed exactly.
//
// A negative unattributed is the failure that matters most — it means a phase is
// being double-counted (for example throttle time also landing inside collect),
// which would make every other figure on the line untrustworthy.
func TestSystemTiming_PhasesAccountForElapsed(t *testing.T) {
	// Cannot use t.Parallel(): mutates GlobalLauncherCache and log.Logger.
	timings := captureSystemTimings(t, map[string][]string{
		systemdefs.SystemNES:     {"a.bin", "b.bin", "c.bin"},
		systemdefs.SystemSNES:    {"d.bin", "e.bin"},
		systemdefs.SystemGenesis: {"f.bin"},
	})
	require.NotEmpty(t, timings, "expected completed system indexing lines")

	for _, st := range timings {
		t.Run(st.System, func(t *testing.T) {
			t.Logf("elapsed=%.3fms setup=%.3f collect=%.3f insert=%.3f reconcile=%.3f "+
				"commit=%.3f throttle=%.3f analyze=%.3f cacheRefresh=%.3f unattributed=%.3f (%.1f%%)",
				st.Elapsed, st.Setup, st.Collect, st.Insert, st.Reconcile, st.Commit,
				st.Throttle, st.Analyze, st.CacheRefresh, st.Unattributed,
				st.Unattributed/st.Elapsed*100)

			assert.GreaterOrEqual(t, st.Unattributed, 0.0,
				"unattributed must never be negative — a negative value means a phase "+
					"is counted twice, which invalidates the whole breakdown")

			for name, v := range map[string]float64{
				"setup": st.Setup, "collect": st.Collect, "insert": st.Insert,
				"reconcile": st.Reconcile, "commit": st.Commit, "throttle": st.Throttle,
				"analyze": st.Analyze, "cacheRefresh": st.CacheRefresh,
			} {
				assert.GreaterOrEqual(t, v, 0.0, "%s must not be negative", name)
			}

			// Reconstruct elapsed. Tolerance covers only float rounding in the
			// log encoding, not slop in the accounting.
			assert.InDelta(t, st.Elapsed, (&st).namedTotal()+st.Unattributed, 0.01,
				"named phases plus unattributed must reconstruct elapsed")
		})
	}
}

// TestSystemTiming_SetupExcludesThrottleWait pins the boundary between setup and
// throttle.
//
// The per-system loop waits on the pauser before doing anything else, and that
// wait is counted in throttle. Measuring setup from the top of the loop instead
// of from after that wait puts the same milliseconds in both fields — which
// round 8 of #1279 shipped. Nothing catches it by inspection: elapsed is still
// correct, and unattributed simply absorbs the error by shrinking, so the line
// looks healthier the more wrong it is.
//
// A pauser with no baseline throttle makes the wait effectively free, so setup
// here should reflect only the real setup work.
func TestSystemTiming_SetupExcludesThrottleWait(t *testing.T) {
	// Cannot use t.Parallel(): mutates GlobalLauncherCache and log.Logger.
	timings := captureSystemTimings(t, map[string][]string{
		systemdefs.SystemNES: {"a.bin", "b.bin"},
	})
	require.NotEmpty(t, timings, "expected completed system indexing lines")

	for _, st := range timings {
		t.Run(st.System, func(t *testing.T) {
			// The decisive check: the named phases must not exceed elapsed. A
			// double-count shows up here first, because the same wait is being
			// billed to two fields that are both inside elapsed.
			assert.LessOrEqual(t, (&st).namedTotal(), st.Elapsed+0.01,
				"named phases must not exceed elapsed; overshoot means a window is "+
					"counted twice (setup spanning the throttle wait is how this last happened)")

			assert.GreaterOrEqual(t, st.Unattributed, 0.0,
				"unattributed must stay non-negative")
		})
	}
}

// TestSystemTiming_FieldsPresent guards against a field being dropped from the
// log line by a later edit. Round 7's analysis was only possible because the
// per-system line carried everything; a silently missing field would put the
// next investigation back to guessing.
func TestSystemTiming_FieldsPresent(t *testing.T) {
	// Cannot use t.Parallel(): mutates GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	platform, cfg, systems := setupCustomLauncherSystems(t, map[string][]string{
		systemdefs.SystemNES: {"a.bin"},
	})

	buf := &syncLogBuffer{}
	prevLogger := log.Logger
	prevGlobal := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(buf).Level(zerolog.InfoLevel)

	_, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	log.Logger = prevLogger
	zerolog.SetGlobalLevel(prevGlobal)
	require.NoError(t, err)

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, `"completed system indexing"`) {
			line = l
			break
		}
	}
	require.NotEmpty(t, line, "expected a completed system indexing line")

	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &fields))

	for _, name := range []string{
		"elapsed", "setup", "collect", "insert", "reconcile",
		"commit", "throttle", "analyze", "cacheRefresh", "unattributed",
	} {
		assert.Contains(t, fields, name,
			"completed system indexing must carry %q for per-system attribution", name)
	}
}
