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

//go:build windows

package power

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want Status
		raw  systemPowerStatus
	}{
		{
			name: "a desktop has no battery",
			raw: systemPowerStatus{
				ACLineStatus:       acLineOnline,
				BatteryFlag:        batteryFlagNoBattery,
				BatteryLifePercent: batteryPercentUnknown,
			},
			want: Status{Source: SourceNoBattery},
		},
		{
			name: "a plugged-in laptop is on external power",
			raw: systemPowerStatus{
				ACLineStatus:       acLineOnline,
				BatteryFlag:        1,
				BatteryLifePercent: 90,
			},
			want: Status{Source: SourceExternal},
		},
		{
			name: "an unplugged laptop reports its charge",
			raw: systemPowerStatus{
				ACLineStatus:       0,
				BatteryFlag:        2,
				BatteryLifePercent: 37,
			},
			want: Status{Source: SourceBattery, Percent: 37},
		},
		{
			name: "an unreadable charge is unknown, not full",
			raw: systemPowerStatus{
				ACLineStatus:       0,
				BatteryFlag:        batteryFlagUnknown,
				BatteryLifePercent: batteryPercentUnknown,
			},
			want: Status{Source: SourceUnknown},
		},
		{
			name: "an unknown battery flag does not read as no battery",
			raw: systemPowerStatus{
				ACLineStatus:       0,
				BatteryFlag:        batteryFlagUnknown,
				BatteryLifePercent: 15,
			},
			want: Status{Source: SourceBattery, Percent: 15},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, statusFrom(&tt.raw))
		})
	}
}
