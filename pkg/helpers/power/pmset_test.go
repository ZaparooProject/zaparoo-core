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

	"github.com/stretchr/testify/assert"
)

// The samples below are real `pmset -g batt` output shapes. The updater
// refuses an install it cannot prove is safe, so the case that matters most is
// a desktop Mac reading as mains-powered rather than as an unreadable battery.
func TestParsePmsetBatt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   Status
	}{
		{
			name:   "desktop Mac has no battery line",
			output: "Now drawing from 'AC Power'\n",
			want:   Status{Source: SourceNoBattery},
		},
		{
			name: "laptop on a charger is external",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t45%; charging; 1:12 remaining present: true\n",
			want: Status{Source: SourceExternal},
		},
		{
			name: "laptop charged and plugged in is external",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t100%; charged; 0:00 remaining present: true\n",
			want: Status{Source: SourceExternal},
		},
		{
			name: "laptop discharging reports its charge",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true\n",
			want: Status{Source: SourceBattery, Percent: 62},
		},
		{
			name: "a charging battery is external even when the source line disagrees",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t45%; charging; 1:12 remaining present: true\n",
			want: Status{Source: SourceExternal},
		},
		{
			name: "an absent battery does not count as a supply",
			output: "Now drawing from 'AC Power'\n" +
				" -InternalBattery-0 (id=4653155)\t0%; charged; 0:00 remaining present: false\n",
			want: Status{Source: SourceNoBattery},
		},
		{
			name: "the lowest of several batteries decides",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true\n" +
				" -InternalBattery-1 (id=4653156)\t18%; discharging; 0:51 remaining present: true\n",
			want: Status{Source: SourceBattery, Percent: 18},
		},
		{
			name: "a present battery with no readable charge is unknown",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t(no estimate); discharging; present: true\n",
			want: Status{Source: SourceUnknown},
		},
		{
			name: "one unreadable present battery makes the combined reading unknown",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true\n" +
				" -InternalBattery-1 (id=4653156)\t(no estimate); discharging; present: true\n",
			want: Status{Source: SourceUnknown},
		},
		{
			name:   "empty output is unknown, not a green light",
			output: "",
			want:   Status{Source: SourceUnknown},
		},
		{
			name:   "output that is not pmset at all is unknown",
			output: "command not found\n",
			want:   Status{Source: SourceUnknown},
		},
		{
			name: "a nonsense charge does not read as a full battery",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t999%; discharging; 3:32 remaining present: true\n",
			want: Status{Source: SourceUnknown},
		},
		{
			name: "a signed charge is not partially parsed",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t-1%; discharging; 3:32 remaining present: true\n",
			want: Status{Source: SourceUnknown},
		},
		{
			name: "a four-digit charge is not partially parsed",
			output: "Now drawing from 'Battery Power'\n" +
				" -InternalBattery-0 (id=4653155)\t1000%; discharging; 3:32 remaining present: true\n",
			want: Status{Source: SourceUnknown},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, parsePmsetBatt(tt.output))
		})
	}
}
