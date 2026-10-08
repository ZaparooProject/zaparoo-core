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

func percentPtr(value int) *int { return &value }

func TestParsePmsetDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   Detail
	}{
		{
			name:   "desktop Mac has no battery",
			output: "Now drawing from 'AC Power'\n",
			want:   Detail{Source: SourceExternal},
		},
		{
			name: "the charge is kept while charging",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t45%; charging; 1:12 remaining present: true\n",
			want: Detail{
				Present: true, Percent: percentPtr(45), Source: SourceExternal, State: ChargeCharging,
				Batteries: []Battery{{ID: "InternalBattery-0", Percent: percentPtr(45), State: ChargeCharging}},
			},
		},
		{
			name: "a discharging laptop reports its estimate",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true\n",
			want: Detail{
				Present: true, Percent: percentPtr(62), Source: SourceBattery, State: ChargeDischarging,
				TimeRemaining: 3*time.Hour + 32*time.Minute,
				Batteries: []Battery{{
					ID: "InternalBattery-0", Percent: percentPtr(62), State: ChargeDischarging,
					TimeRemaining: 3*time.Hour + 32*time.Minute,
				}},
			},
		},
		{
			name: "a full battery on the charger",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t100%; charged; 0:00 remaining present: true\n",
			want: Detail{
				Present: true, Percent: percentPtr(100), Source: SourceExternal, State: ChargeFull,
				Batteries: []Battery{{ID: "InternalBattery-0", Percent: percentPtr(100), State: ChargeFull}},
			},
		},
		{
			name: "a charge limit reads as not charging",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t80%; AC attached; not charging present: true\n",
			want: Detail{
				Present: true, Percent: percentPtr(80), Source: SourceExternal, State: ChargeNotCharging,
				Batteries: []Battery{{ID: "InternalBattery-0", Percent: percentPtr(80), State: ChargeNotCharging}},
			},
		},
		{
			name: "an unseated battery is not counted",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t0%; present: false\n",
			want: Detail{Source: SourceExternal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parsePmsetDetail(tt.output))
		})
	}
}

func FuzzParsePmsetDetail(f *testing.F) {
	f.Add("Now drawing from 'Battery Power'\n" +
		" -InternalBattery-0 (id=1)\t62%; discharging; 3:32 remaining present: true\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, output string) {
		detail := parsePmsetDetail(output)
		if detail.Percent != nil && (*detail.Percent < 0 || *detail.Percent > 100) {
			t.Fatalf("percent out of range: %d", *detail.Percent)
		}
		if detail.TimeRemaining < 0 {
			t.Fatalf("negative time remaining: %s", detail.TimeRemaining)
		}
	})
}
