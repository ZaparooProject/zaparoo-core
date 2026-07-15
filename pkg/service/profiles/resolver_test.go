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

package profiles

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResolver(t *testing.T) (resolver *LimitsResolver, cfg *config.Instance, st *state.State) {
	t.Helper()
	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	return NewLimitsResolver(cfg, st), cfg, st
}

func strPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

func TestLimitsResolver_NoProfileInheritsGlobal(t *testing.T) {
	t.Parallel()
	resolver, cfg, _ := newResolver(t)

	cfg.SetPlaytimeLimitsEnabled(true)
	require.NoError(t, cfg.SetDailyLimit("2h"))
	require.NoError(t, cfg.SetSessionLimit("45m"))

	assert.True(t, resolver.PlaytimeLimitsEnabled())
	assert.Equal(t, 2*time.Hour, resolver.DailyLimit())
	assert.Equal(t, 45*time.Minute, resolver.SessionLimit())
	assert.Empty(t, resolver.ActiveProfileID())
}

func TestLimitsResolver_ProfileOverrides(t *testing.T) {
	t.Parallel()
	resolver, cfg, st := newResolver(t)

	cfg.SetPlaytimeLimitsEnabled(false)
	require.NoError(t, cfg.SetDailyLimit("8h"))

	st.SetActiveProfile(&models.ActiveProfile{
		ProfileID:     "kid-a",
		Name:          "Kid A",
		LimitsEnabled: boolPtr(true),
		DailyLimit:    strPtr("1h30m"),
		SessionLimit:  strPtr("30m"),
	})

	assert.True(t, resolver.PlaytimeLimitsEnabled())
	assert.Equal(t, 90*time.Minute, resolver.DailyLimit())
	assert.Equal(t, 30*time.Minute, resolver.SessionLimit())
	assert.Equal(t, "kid-a", resolver.ActiveProfileID())
}

func TestLimitsResolver_PartialOverridesInheritRest(t *testing.T) {
	t.Parallel()
	resolver, cfg, st := newResolver(t)

	cfg.SetPlaytimeLimitsEnabled(true)
	require.NoError(t, cfg.SetDailyLimit("4h"))
	require.NoError(t, cfg.SetSessionLimit("1h"))

	// Profile overrides only the daily limit; everything else inherits.
	st.SetActiveProfile(&models.ActiveProfile{
		ProfileID:  "kid-b",
		Name:       "Kid B",
		DailyLimit: strPtr("2h"),
	})

	assert.True(t, resolver.PlaytimeLimitsEnabled())
	assert.Equal(t, 2*time.Hour, resolver.DailyLimit())
	assert.Equal(t, time.Hour, resolver.SessionLimit())
}

func TestLimitsResolver_ZeroMeansUnlimited(t *testing.T) {
	t.Parallel()
	resolver, cfg, st := newResolver(t)

	cfg.SetPlaytimeLimitsEnabled(true)
	require.NoError(t, cfg.SetDailyLimit("2h"))

	// "0" is an explicit override to unlimited, distinct from nil/inherit.
	st.SetActiveProfile(&models.ActiveProfile{
		ProfileID:  "parent",
		Name:       "Parent",
		DailyLimit: strPtr("0"),
	})

	assert.Equal(t, time.Duration(0), resolver.DailyLimit())
}

func TestLimitsResolver_WarningsAlwaysGlobal(t *testing.T) {
	t.Parallel()
	resolver, cfg, st := newResolver(t)

	require.NoError(t, cfg.SetWarningIntervals([]string{"10m", "1m"}))
	st.SetActiveProfile(&models.ActiveProfile{ProfileID: "kid-a", Name: "Kid A"})

	assert.Equal(t, []time.Duration{10 * time.Minute, time.Minute}, resolver.WarningIntervals())
}
