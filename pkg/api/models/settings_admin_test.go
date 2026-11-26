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

package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNullableBoolTellsNullFromMissing(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		want  *bool
		name  string
		input string
		sent  bool
	}{
		{name: "missing", input: `{}`},
		{name: "null", input: `{"inputGamepadEnabled":null}`, sent: true},
		{name: "true", input: `{"inputGamepadEnabled":true}`, sent: true, want: new(true)},
		{name: "false", input: `{"inputGamepadEnabled":false}`, sent: true, want: new(false)},
	} {
		var p AdminSettingsParams
		require.NoError(t, json.Unmarshal([]byte(tt.input), &p), tt.name)
		assert.Equal(t, tt.sent, p.InputGamepadEnabled.Sent, tt.name)
		assert.Equal(t, tt.want, p.InputGamepadEnabled.Value, tt.name)
		assert.Equal(t, tt.sent, p.Set(), tt.name)

		out, err := json.Marshal(p)
		require.NoError(t, err, tt.name)
		assert.JSONEq(t, tt.input, string(out), "%s: marshals back to what was sent", tt.name)
	}

	var p AdminSettingsParams
	require.Error(t, json.Unmarshal([]byte(`{"inputGamepadEnabled":"yes"}`), &p))
}
