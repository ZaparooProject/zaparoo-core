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

package systray

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReloadCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		failedMethod    string
		expectedError   string
		expectedMethods []string
	}{
		{
			name: "reloads settings then launchers",
			expectedMethods: []string{
				models.MethodSettingsReload,
				models.MethodLaunchersRefresh,
			},
		},
		{
			name:            "stops when settings reload fails",
			failedMethod:    models.MethodSettingsReload,
			expectedMethods: []string{models.MethodSettingsReload},
			expectedError:   "reload settings: request failed",
		},
		{
			name:         "reports launcher refresh failure",
			failedMethod: models.MethodLaunchersRefresh,
			expectedMethods: []string{
				models.MethodSettingsReload,
				models.MethodLaunchersRefresh,
			},
			expectedError: "refresh launchers: request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Instance{}
			var methods []string
			localClient := func(
				_ context.Context,
				actualCfg *config.Instance,
				method string,
				params string,
			) (string, error) {
				require.Same(t, cfg, actualCfg)
				assert.Empty(t, params)
				methods = append(methods, method)
				if method == tt.failedMethod {
					return "", errors.New("request failed")
				}
				return "", nil
			}

			err := reloadCore(cfg, localClient)

			assert.Equal(t, tt.expectedMethods, methods)
			if tt.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.expectedError)
			}
		})
	}
}
