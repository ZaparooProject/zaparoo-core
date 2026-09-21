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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategorizedErrorHidesCauseFromMessage(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("file not found")
	cause := fmt.Errorf("%w: /media/fat/games/secret.sfc", sentinel)
	err := CategorizedErr(ErrorCategoryMediaNotFound, "media not found", cause)

	assert.Equal(t, "media not found", err.Error())
	assert.NotContains(t, err.Error(), "/media/fat")
	require.ErrorIs(t, err, sentinel)

	var catErr *CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, ErrorCategoryMediaNotFound, catErr.Category)
	assert.Equal(t, cause, catErr.Err)
	assert.Empty(t, catErr.Reason)
	assert.Empty(t, catErr.Params)
}

func TestCategorizedDetailErrCarriesStructuredDetail(t *testing.T) {
	t.Parallel()

	cause := errors.New("binder transaction failed")
	params := map[string]string{"launcher": "RetroArch", "plugin": "Mesen"}
	err := CategorizedDetailErr(ErrorCategoryLaunchRepair,
		"this launcher's plugin for this system is not installed",
		"launcher_plugin_missing", params, cause)

	var catErr *CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, ErrorCategoryLaunchRepair, catErr.Category)
	assert.Equal(t, "launcher_plugin_missing", catErr.Reason)
	assert.Equal(t, params, catErr.Params)
	require.ErrorIs(t, err, cause)
}

// ErrorData gained reason and params, so every category that does not document
// them must still serialise exactly as it did before.
func TestErrorDataOmitsUndocumentedDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		data ErrorData
		name string
		want string
	}{
		{
			name: "category alone",
			data: ErrorData{Category: ErrorCategoryMediaNotFound},
			want: `{"category":"media_not_found"}`,
		},
		{
			name: "empty reason and params stay absent",
			data: ErrorData{Category: ErrorCategoryExecutionFailed, Reason: "", Params: nil},
			want: `{"category":"execution_failed"}`,
		},
		{
			name: "an empty params map stays absent",
			data: ErrorData{Category: ErrorCategoryExecutionFailed, Params: map[string]string{}},
			want: `{"category":"execution_failed"}`,
		},
		{
			name: "a reason without params",
			data: ErrorData{Category: ErrorCategoryLaunchRepair, Reason: "host_unavailable"},
			want: `{"category":"launch_repair","reason":"host_unavailable"}`,
		},
		{
			name: "a reason with its display names",
			data: ErrorData{
				Category: ErrorCategoryLaunchRepair,
				Reason:   "launcher_plugin_missing",
				Params:   map[string]string{"launcher": "RetroArch", "plugin": "Mesen"},
			},
			want: `{"params":{"launcher":"RetroArch","plugin":"Mesen"},` +
				`"category":"launch_repair","reason":"launcher_plugin_missing"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(tt.data)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))
		})
	}
}
