// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

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
		expectedMethods []string
		expectedError   string
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
