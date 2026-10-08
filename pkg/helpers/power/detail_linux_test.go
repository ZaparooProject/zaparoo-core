//go:build linux && !android

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

package power

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(value int) *int { return &value }

func TestDetailFrom(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "sys", "class", "power_supply")

	tests := []struct {
		name     string
		supplies []supply
		want     Detail
	}{
		{
			name:     "a machine with nothing to report has no battery",
			supplies: []supply{},
			want:     Detail{Source: SourceExternal},
		},
		{
			name: "the charge is kept while charging",
			supplies: []supply{
				{name: "AC", fields: map[string]string{"type": "Mains", "online": "1"}},
				{name: "BAT0", fields: map[string]string{"type": "Battery", "capacity": "62", "status": "Charging"}},
			},
			want: Detail{
				Present: true, Percent: intPtr(62), Source: SourceExternal, State: ChargeCharging,
				Batteries: []Battery{{ID: "BAT0", Percent: intPtr(62), State: ChargeCharging}},
			},
		},
		{
			name: "a discharging battery reports the kernel's time estimate",
			supplies: []supply{
				{name: "AC", fields: map[string]string{"type": "Mains", "online": "0"}},
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "capacity": "40", "status": "Discharging", "time_to_empty_now": "5400",
				}},
			},
			want: Detail{
				Present: true, Percent: intPtr(40), Source: SourceBattery, State: ChargeDischarging,
				TimeRemaining: 90 * time.Minute,
				Batteries: []Battery{{
					ID: "BAT0", Percent: intPtr(40), State: ChargeDischarging, TimeRemaining: 90 * time.Minute,
				}},
			},
		},
		{
			name: "time remaining is derived from charge and current when the kernel has none",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "capacity": "50", "status": "Discharging",
					"charge_now": "2000000", "current_now": "1000000",
				}},
			},
			want: Detail{
				Present: true, Percent: intPtr(50), Source: SourceBattery, State: ChargeDischarging,
				TimeRemaining: 2 * time.Hour,
				Batteries: []Battery{{
					ID: "BAT0", Percent: intPtr(50), State: ChargeDischarging, TimeRemaining: 2 * time.Hour,
				}},
			},
		},
		{
			name: "a controller battery is not the device's battery",
			supplies: []supply{
				{name: "ps-controller-battery-aa:bb", fields: map[string]string{
					"type": "Battery", "scope": "Device", "capacity": "30", "status": "Discharging",
				}},
			},
			want: Detail{Source: SourceExternal},
		},
		{
			name: "an absent battery bay is skipped",
			supplies: []supply{
				{name: "BAT1", fields: map[string]string{"type": "Battery", "present": "0"}},
			},
			want: Detail{Source: SourceExternal},
		},
		{
			name: "the lowest of several batteries is the summary",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{"type": "Battery", "capacity": "80", "status": "Discharging"}},
				{name: "BAT1", fields: map[string]string{"type": "Battery", "capacity": "15", "status": "Discharging"}},
			},
			want: Detail{
				Present: true, Percent: intPtr(15), Source: SourceBattery, State: ChargeDischarging,
				Batteries: []Battery{
					{ID: "BAT0", Percent: intPtr(80), State: ChargeDischarging},
					{ID: "BAT1", Percent: intPtr(15), State: ChargeDischarging},
				},
			},
		},
		{
			name: "a charge limit reads as not charging on external power",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "capacity": "80", "status": "Not charging",
				}},
			},
			want: Detail{
				Present: true, Percent: intPtr(80), Source: SourceExternal, State: ChargeNotCharging,
				Batteries: []Battery{{ID: "BAT0", Percent: intPtr(80), State: ChargeNotCharging}},
			},
		},
		{
			name: "a battery that reports junk has no charge and no state",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{"type": "Battery", "capacity": "255", "status": "Unknown"}},
			},
			want: Detail{Present: true, Batteries: []Battery{{ID: "BAT0"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := writeSupplies(t, root, tt.supplies)
			got, err := detailFrom(fs, root)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetailFrom_NoPowerSupplyClass(t *testing.T) {
	t.Parallel()

	got, err := detailFrom(afero.NewMemMapFs(), filepath.Join(string(filepath.Separator), "missing"))
	require.NoError(t, err)
	assert.Equal(t, Detail{Source: SourceExternal}, got)
}

func FuzzDetailFrom(f *testing.F) {
	f.Add("Battery", "62", "Charging", "System", "5400")
	f.Add("Mains", "", "", "", "")
	f.Add("Battery", "-1", "Not charging", "Device", "x")
	f.Fuzz(func(t *testing.T, supplyType, capacity, status, scope, timeToEmpty string) {
		root := filepath.Join(string(filepath.Separator), "ps")
		fs := afero.NewMemMapFs()
		dir := filepath.Join(root, "BAT0")
		fields := map[string]string{
			"type": supplyType, "capacity": capacity, "status": status, "scope": scope,
			"time_to_empty_now": timeToEmpty,
		}
		for name, value := range fields {
			if err := afero.WriteFile(fs, filepath.Join(dir, name), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		detail, err := detailFrom(fs, root)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Percent != nil && (*detail.Percent < 0 || *detail.Percent > 100) {
			t.Fatalf("percent out of range: %d", *detail.Percent)
		}
		if detail.TimeRemaining < 0 {
			t.Fatalf("negative time remaining: %s", detail.TimeRemaining)
		}
	})
}
