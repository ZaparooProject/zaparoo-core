//go:build linux

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockInstaller is a test double that tracks which methods were called
// without performing any actual installation operations.
type mockInstaller struct {
	installApplicationCalled   bool
	installDesktopCalled       bool
	installServiceCalled       bool
	installHardwareCalled      bool
	uninstallApplicationCalled bool
	uninstallDesktopCalled     bool
	uninstallServiceCalled     bool
	uninstallHardwareCalled    bool
}

func (m *mockInstaller) InstallApplication() error {
	m.installApplicationCalled = true
	return nil
}

func (m *mockInstaller) InstallDesktop() error {
	m.installDesktopCalled = true
	return nil
}

func (m *mockInstaller) InstallService() error {
	m.installServiceCalled = true
	return nil
}

func (m *mockInstaller) InstallHardware() error {
	m.installHardwareCalled = true
	return nil
}

func (m *mockInstaller) UninstallApplication() error {
	m.uninstallApplicationCalled = true
	return nil
}

func (m *mockInstaller) UninstallDesktop() error {
	m.uninstallDesktopCalled = true
	return nil
}

func (m *mockInstaller) UninstallService() error {
	m.uninstallServiceCalled = true
	return nil
}

func (m *mockInstaller) UninstallHardware() error {
	m.uninstallHardwareCalled = true
	return nil
}

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
	// Not parallel - modifies package-level defaultInstaller
	mock := &mockInstaller{}
	originalInstaller := defaultInstaller
	defaultInstaller = mock
	t.Cleanup(func() {
		defaultInstaller = originalInstaller
	})

	tests := []struct {
		checkFunc func() bool
		component string
	}{
		{component: "application", checkFunc: func() bool { return mock.installApplicationCalled }},
		{component: "desktop", checkFunc: func() bool { return mock.installDesktopCalled }},
		{component: "service", checkFunc: func() bool { return mock.installServiceCalled }},
		{component: "hardware", checkFunc: func() bool { return mock.installHardwareCalled }},
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			// Reset mock state for each test
			*mock = mockInstaller{}

			err := HandleInstall(tt.component)
			require.NoError(t, err)
			assert.True(t, tt.checkFunc(),
				"Expected %s installer to be called", tt.component)
		})
	}
}

func TestHandleUninstall_ValidComponents(t *testing.T) {
	// Not parallel - modifies package-level defaultInstaller
	mock := &mockInstaller{}
	originalInstaller := defaultInstaller
	defaultInstaller = mock
	t.Cleanup(func() {
		defaultInstaller = originalInstaller
	})

	tests := []struct {
		checkFunc func() bool
		component string
	}{
		{component: "application", checkFunc: func() bool { return mock.uninstallApplicationCalled }},
		{component: "desktop", checkFunc: func() bool { return mock.uninstallDesktopCalled }},
		{component: "service", checkFunc: func() bool { return mock.uninstallServiceCalled }},
		{component: "hardware", checkFunc: func() bool { return mock.uninstallHardwareCalled }},
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			// Reset mock state for each test
			*mock = mockInstaller{}

			err := HandleUninstall(tt.component)
			require.NoError(t, err)
			assert.True(t, tt.checkFunc(),
				"Expected %s uninstaller to be called", tt.component)
		})
	}
}
