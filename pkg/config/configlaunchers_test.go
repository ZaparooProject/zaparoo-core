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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLaunchersBeforeMediaStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		script   string
		expected string
	}{
		{
			name:     "empty script",
			script:   "",
			expected: "",
		},
		{
			name:     "simple script",
			script:   "**echo:before launch",
			expected: "**echo:before launch",
		},
		{
			name:     "execute script",
			script:   "**execute:/usr/bin/notify-send 'Game starting'",
			expected: "**execute:/usr/bin/notify-send 'Game starting'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Launchers: Launchers{
						BeforeMediaStart: tt.script,
					},
				},
			}

			result := cfg.LaunchersBeforeMediaStart()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetExecuteAllowListForTesting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		testCmd   string
		allowList []string
		expected  bool
	}{
		{
			name:      "empty allow list blocks all",
			allowList: []string{},
			testCmd:   "echo hello",
			expected:  false,
		},
		{
			name:      "wildcard allows all",
			allowList: []string{".*"},
			testCmd:   "echo hello",
			expected:  true,
		},
		{
			name:      "specific command allowed",
			allowList: []string{"^echo$"},
			testCmd:   "echo",
			expected:  true,
		},
		{
			name:      "specific command not in list blocked",
			allowList: []string{"^echo$"},
			testCmd:   "rm -rf",
			expected:  false,
		},
		{
			name:      "path pattern matching",
			allowList: []string{"/usr/bin/.*"},
			testCmd:   "/usr/bin/notify-send",
			expected:  true,
		},
		{
			name:      "multiple patterns",
			allowList: []string{"^echo$", "^notify-send$"},
			testCmd:   "notify-send",
			expected:  true,
		},
		{
			name:      "invalid regex is skipped gracefully",
			allowList: []string{"[invalid", "^echo$"},
			testCmd:   "echo",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{}
			cfg.SetExecuteAllowListForTesting(tt.allowList)

			result := cfg.IsExecuteAllowed(tt.testCmd)
			assert.Equal(t, tt.expected, result, "command: %s, allowList: %v", tt.testCmd, tt.allowList)
		})
	}
}

func TestSetExecuteAllowListForTesting_CompilesRegex(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	cfg.SetExecuteAllowListForTesting([]string{"^test.*", "^echo$"})

	// Verify internal state was set correctly
	assert.Len(t, cfg.vals.ZapScript.AllowExecute, 2)
	assert.Len(t, cfg.vals.ZapScript.allowExecuteRe, 2)
	assert.NotNil(t, cfg.vals.ZapScript.allowExecuteRe[0])
	assert.NotNil(t, cfg.vals.ZapScript.allowExecuteRe[1])
}
