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

package client

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

func TestDisableZapScriptErrorClassification(t *testing.T) {
	// Serial: capture the package-global logger without affecting parallel tests.
	for _, tc := range []struct {
		failure     error
		name, level string
	}{
		{name: "refused", failure: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), level: "warn"},
		{name: "timeout", failure: context.DeadlineExceeded, level: "error"},
		{name: "auth", failure: errors.New("unauthorized"), level: "error"},
		{name: "protocol", failure: errors.New("invalid response"), level: "error"},
		{name: "text is not classification", failure: errors.New("connection refused"), level: "error"},
	} {
		for _, restore := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/restore_%t", tc.name, restore), func(t *testing.T) {
				var output bytes.Buffer
				old, oldLevel := log.Logger, zerolog.GlobalLevel()
				log.Logger = zerolog.New(&output)
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
				t.Cleanup(func() { log.Logger = old; zerolog.SetGlobalLevel(oldLevel) })
				calls := 0
				enable := disableZapScriptWithRequest(nil, func(
					_ context.Context, _ *config.Instance, method, params string,
				) (string, error) {
					calls++
					assert.Equal(t, models.MethodSettingsUpdate, method)
					if calls == 1 {
						assert.JSONEq(t, `{"runZapScript":false}`, params)
						if restore {
							return "", nil
						}
					} else {
						assert.JSONEq(t, `{"runZapScript":true}`, params)
					}
					return "", tc.failure
				})
				require.NotNil(t, enable)
				enable()
				if restore {
					assert.Equal(t, 2, calls)
				} else {
					assert.Equal(t, 1, calls)
				}
				assert.Contains(t, output.String(), `"level":"`+tc.level+`"`)
				if tc.level == "warn" {
					assert.NotContains(t, output.String(), `"level":"error"`)
				}
			})
		}
	}
}

func TestDisableZapScriptSuccessfulRestore(t *testing.T) {
	t.Parallel()
	var paramsSeen []string
	enable := disableZapScriptWithRequest(nil, func(
		_ context.Context, _ *config.Instance, method, params string,
	) (string, error) {
		assert.Equal(t, models.MethodSettingsUpdate, method)
		paramsSeen = append(paramsSeen, params)
		return "", nil
	})
	require.Len(t, paramsSeen, 1)
	enable()
	require.Len(t, paramsSeen, 2)
	assert.JSONEq(t, `{"runZapScript":false}`, paramsSeen[0])
	assert.JSONEq(t, `{"runZapScript":true}`, paramsSeen[1])
}
