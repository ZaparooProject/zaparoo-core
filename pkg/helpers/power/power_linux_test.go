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

//go:build linux

package power

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supply is one directory under the kernel's power-supply class.
type supply struct {
	fields map[string]string
	name   string
}

func writeSupplies(t *testing.T, root string, supplies []supply) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(root, 0o755))
	for _, s := range supplies {
		dir := filepath.Join(root, s.name)
		require.NoError(t, fs.MkdirAll(dir, 0o755))
		for name, value := range s.fields {
			require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), []byte(value+"\n"), 0o644))
		}
	}
	return fs
}

func TestStatusFrom(t *testing.T) {
	t.Parallel()

	const root = "/sys/class/power_supply"

	tests := []struct {
		name     string
		supplies []supply
		want     Status
	}{
		{
			name:     "a machine with nothing to report has no battery",
			supplies: []supply{},
			want:     Status{Source: SourceNoBattery},
		},
		{
			name: "mains only is no battery",
			supplies: []supply{
				{name: "AC", fields: map[string]string{"type": "Mains", "online": "1"}},
			},
			want: Status{Source: SourceNoBattery},
		},
		{
			name: "a wireless mouse is not the machine's battery",
			supplies: []supply{
				{name: "hidpp_battery_0", fields: map[string]string{
					"type": "Battery", "scope": "Device", "status": "Discharging", "capacity": "4",
				}},
			},
			want: Status{Source: SourceNoBattery},
		},
		{
			name: "a plugged-in handheld is on external power",
			supplies: []supply{
				{name: "AC", fields: map[string]string{"type": "Mains", "online": "1"}},
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "status": "Discharging", "capacity": "12",
				}},
			},
			want: Status{Source: SourceExternal},
		},
		{
			name: "a charging battery is external power even with no mains supply",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "status": "Charging", "capacity": "12",
				}},
			},
			want: Status{Source: SourceExternal},
		},
		{
			name: "a full battery is external power",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{"type": "Battery", "status": "Full"}},
			},
			want: Status{Source: SourceExternal},
		},
		{
			name: "an unplugged handheld reports its charge",
			supplies: []supply{
				{name: "AC", fields: map[string]string{"type": "Mains", "online": "0"}},
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "status": "Discharging", "capacity": "63",
				}},
			},
			want: Status{Source: SourceBattery, Percent: 63},
		},
		{
			name: "the lowest of several batteries decides",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "status": "Discharging", "capacity": "80",
				}},
				{name: "BAT1", fields: map[string]string{
					"type": "Battery", "status": "Discharging", "capacity": "22",
				}},
			},
			want: Status{Source: SourceBattery, Percent: 22},
		},
		{
			name: "a system battery ranks above a peripheral one",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "scope": "System", "status": "Discharging", "capacity": "70",
				}},
				{name: "hidpp_battery_0", fields: map[string]string{
					"type": "Battery", "scope": "Device", "status": "Discharging", "capacity": "3",
				}},
			},
			want: Status{Source: SourceBattery, Percent: 70},
		},
		{
			name: "a battery with no readable charge is unknown, not full",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{"type": "Battery", "status": "Discharging"}},
			},
			want: Status{Source: SourceUnknown},
		},
		{
			name: "a charge outside 0-100 is junk and reads as unknown",
			supplies: []supply{
				{name: "BAT0", fields: map[string]string{
					"type": "Battery", "status": "Discharging", "capacity": "6553",
				}},
			},
			want: Status{Source: SourceUnknown},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := writeSupplies(t, root, tt.supplies)
			got, err := statusFrom(fs, root)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A kernel built without the power-supply class has nothing to report, which
// is hardware that runs on mains rather than a device hiding a flat battery.
func TestStatusFromMissingClass(t *testing.T) {
	t.Parallel()

	got, err := statusFrom(afero.NewMemMapFs(), "/sys/class/power_supply")
	require.NoError(t, err)
	assert.Equal(t, Status{Source: SourceNoBattery}, got)
}
