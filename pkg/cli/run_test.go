//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleInstall_UnknownComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component string
		errMsg    string
		wantErr   bool
	}{
		{
			name:      "unknown_component",
			component: "unknown",
			wantErr:   true,
			errMsg:    "unknown component: unknown",
		},
		{
			name:      "empty_component",
			component: "",
			wantErr:   true,
			errMsg:    "unknown component:",
		},
		{
			name:      "typo_in_component",
			component: "applicaiton",
			wantErr:   true,
			errMsg:    "unknown component: applicaiton",
		},
		{
			name:      "case_sensitive_component",
			component: "Application",
			wantErr:   true,
			errMsg:    "unknown component: Application",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := HandleInstall(tt.component)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Contains(t, err.Error(), "valid: application, desktop, service, hardware")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHandleUninstall_UnknownComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component string
		errMsg    string
		wantErr   bool
	}{
		{
			name:      "unknown_component",
			component: "unknown",
			wantErr:   true,
			errMsg:    "unknown component: unknown",
		},
		{
			name:      "empty_component",
			component: "",
			wantErr:   true,
			errMsg:    "unknown component:",
		},
		{
			name:      "typo_in_component",
			component: "desktpo",
			wantErr:   true,
			errMsg:    "unknown component: desktpo",
		},
		{
			name:      "case_sensitive_component",
			component: "Service",
			wantErr:   true,
			errMsg:    "unknown component: Service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := HandleUninstall(tt.component)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Contains(t, err.Error(), "valid: application, desktop, service, hardware")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHandleInstall_ValidComponents(t *testing.T) {
	t.Parallel()

	// These are the valid component names that should be accepted
	// The actual installation may fail due to environment constraints,
	// but the switch case should route correctly
	validComponents := []string{
		"application",
		"desktop",
		"service",
		"hardware",
	}

	for _, component := range validComponents {
		t.Run(component, func(t *testing.T) {
			t.Parallel()

			err := HandleInstall(component)
			// If there's an error, it should NOT be about unknown component
			if err != nil {
				assert.NotContains(t, err.Error(), "unknown component",
					"Valid component %q should not be reported as unknown", component)
			}
		})
	}
}

func TestHandleUninstall_ValidComponents(t *testing.T) {
	t.Parallel()

	// These are the valid component names that should be accepted
	// The actual uninstallation may fail due to environment constraints,
	// but the switch case should route correctly
	validComponents := []string{
		"application",
		"desktop",
		"service",
		"hardware",
	}

	for _, component := range validComponents {
		t.Run(component, func(t *testing.T) {
			t.Parallel()

			err := HandleUninstall(component)
			// If there's an error, it should NOT be about unknown component
			if err != nil {
				assert.NotContains(t, err.Error(), "unknown component",
					"Valid component %q should not be reported as unknown", component)
			}
		})
	}
}
