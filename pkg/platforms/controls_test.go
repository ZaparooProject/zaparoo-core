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

package platforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlNames(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ControlNames(nil))
	assert.Nil(t, ControlNames(&Launcher{}))
	assert.Equal(t,
		[]string{ControlReset, ControlSaveState, ControlStop},
		ControlNames(&Launcher{Controls: map[string]Control{
			ControlStop: {}, ControlSaveState: {}, ControlReset: {},
		}}),
	)
}

func TestSlotArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args    map[string]string
		name    string
		want    int
		wantErr bool
	}{
		{name: "absent selects the minimum", args: nil, want: 1},
		{name: "empty selects the minimum", args: map[string]string{"slot": ""}, want: 1},
		{name: "lower bound", args: map[string]string{"slot": "1"}, want: 1},
		{name: "upper bound", args: map[string]string{"slot": "4"}, want: 4},
		{name: "below range", args: map[string]string{"slot": "0"}, wantErr: true},
		{name: "above range", args: map[string]string{"slot": "5"}, wantErr: true},
		{name: "not a number", args: map[string]string{"slot": "one"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SlotArg(ControlParams{Args: tt.args}, 1, 4)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
