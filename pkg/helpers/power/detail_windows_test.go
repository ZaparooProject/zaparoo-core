//go:build windows

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDetailFromRaw(t *testing.T) {
	t.Parallel()

	percent := func(value int) *int { return &value }

	tests := []struct {
		name string
		want Detail
		raw  systemPowerStatus
	}{
		{
			name: "a desktop has no battery",
			raw: systemPowerStatus{
				ACLineStatus: acLineOnline, BatteryFlag: batteryFlagNoBattery,
				BatteryLifePercent: batteryPercentUnknown,
			},
			want: Detail{Source: SourceExternal},
		},
		{
			name: "the charge is kept while charging",
			raw: systemPowerStatus{
				ACLineStatus: acLineOnline, BatteryFlag: batteryFlagCharging, BatteryLifePercent: 55,
				BatteryLifeTime: batteryLifeTimeUnknown,
			},
			want: Detail{
				Present: true, Percent: percent(55), Source: SourceExternal, State: ChargeCharging,
				Batteries: []Battery{{ID: "system", Percent: percent(55), State: ChargeCharging}},
			},
		},
		{
			name: "an unplugged laptop reports its estimate",
			raw: systemPowerStatus{
				ACLineStatus: acLineOffline, BatteryFlag: 1, BatteryLifePercent: 70, BatteryLifeTime: 3600,
			},
			want: Detail{
				Present: true, Percent: percent(70), Source: SourceBattery, State: ChargeDischarging,
				TimeRemaining: time.Hour,
				Batteries: []Battery{{
					ID: "system", Percent: percent(70), State: ChargeDischarging, TimeRemaining: time.Hour,
				}},
			},
		},
		{
			name: "an unreadable charge is left empty",
			raw: systemPowerStatus{
				ACLineStatus: acLineOffline, BatteryFlag: batteryFlagUnknown,
				BatteryLifePercent: batteryPercentUnknown,
				BatteryLifeTime:    batteryLifeTimeUnknown,
			},
			want: Detail{
				Present: true, Source: SourceBattery, State: ChargeDischarging,
				Batteries: []Battery{{ID: "system", State: ChargeDischarging}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, detailFromRaw(&tt.raw))
		})
	}
}
