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

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReloadCore(t *testing.T) {
	t.Parallel()

	var methods []string
	call := func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		methods = append(methods, method)
		return "", nil
	}

	require.NoError(t, reloadCore(context.Background(), nil, call))
	assert.Equal(t, []string{models.MethodSettingsReload, models.MethodLaunchersRefresh}, methods)
}

func TestReloadCore_StopsAfterFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		failed      string
		errContains string
		expected    []string
	}{
		{
			name:        "settings reload",
			failed:      models.MethodSettingsReload,
			expected:    []string{models.MethodSettingsReload},
			errContains: "reload settings",
		},
		{
			name:        "launcher refresh",
			failed:      models.MethodLaunchersRefresh,
			expected:    []string{models.MethodSettingsReload, models.MethodLaunchersRefresh},
			errContains: "refresh launchers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var methods []string
			call := func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
				methods = append(methods, method)
				if method == tt.failed {
					return "", errors.New("request failed")
				}
				return "", nil
			}

			err := reloadCore(context.Background(), nil, call)
			require.ErrorContains(t, err, tt.errContains)
			assert.Equal(t, tt.expected, methods)
		})
	}
}

func TestLogClientCommandError(t *testing.T) {
	tests := []struct {
		err           error
		name          string
		expectedLevel string
	}{
		{
			name:          "connection refused logs at warn",
			err:           syscall.ECONNREFUSED,
			expectedLevel: "warn",
		},
		{
			name:          "wrapped connection refused logs at warn",
			err:           fmt.Errorf("dial failed: %w", syscall.ECONNREFUSED),
			expectedLevel: "warn",
		},
		{
			name:          "other error logs at error",
			err:           errors.New("something else broke"),
			expectedLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
			prevLogger := log.Logger
			log.Logger = logger
			t.Cleanup(func() { log.Logger = prevLogger })

			logClientCommandError(tt.err, "error running")

			output := buf.String()
			assert.Contains(t, output, `"level":"`+tt.expectedLevel+`"`)
			assert.Contains(t, output, "error running")
		})
	}
}
