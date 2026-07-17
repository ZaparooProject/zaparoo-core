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

package installer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstallApplication(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	tests := []struct {
		setupMock     func(*mocks.MockCommandExecutor)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "successful installation",
			setupMock: func(_ *mocks.MockCommandExecutor) {
				// Commands succeed by default from helper, no need to override
			},
			expectError: false,
		},
		{
			name: "update-desktop-database fails silently",
			setupMock: func(cmd *mocks.MockCommandExecutor) {
				// Override default - clear and set specific expectations
				cmd.ExpectedCalls = nil
				cmd.On("Run", mock.Anything, "update-desktop-database", mock.AnythingOfType("[]string")).
					Return(errors.New("command not found"))
				cmd.On("Run", mock.Anything, "gtk-update-icon-cache", mock.AnythingOfType("[]string")).
					Return(nil)
			},
			expectError: false, // Errors are ignored for these utility commands
		},
		{
			name: "gtk-update-icon-cache fails silently",
			setupMock: func(cmd *mocks.MockCommandExecutor) {
				// Override default - clear and set specific expectations
				cmd.ExpectedCalls = nil
				cmd.On("Run", mock.Anything, "update-desktop-database", mock.AnythingOfType("[]string")).
					Return(nil)
				cmd.On("Run", mock.Anything, "gtk-update-icon-cache", mock.AnythingOfType("[]string")).
					Return(errors.New("command not found"))
			},
			expectError: false, // Errors are ignored for these utility commands
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() - subtests share filesystem state

			cmd := helpers.NewMockCommandExecutor()
			if tt.setupMock != nil {
				tt.setupMock(cmd)
			}

			err := doInstallApplication(cmd)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify files were created (basic smoke test)
				binPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")
				_, err := os.Stat(binPath)
				require.NoError(t, err, "binary should be installed")

				desktopPath := filepath.Join(xdg.DataHome, "applications", "zaparoo.desktop")
				_, err = os.Stat(desktopPath)
				require.NoError(t, err, "desktop file should be installed")

				// Verify desktop file is properly templated with binary path
				//nolint:gosec // Reading test fixture file for verification
				desktopContent, err := os.ReadFile(desktopPath)
				require.NoError(t, err, "should be able to read desktop file")
				expectedPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")
				assert.Contains(t, string(desktopContent), "Exec="+expectedPath+" -start",
					"desktop file should contain templated path, not {{.ExecPath}}")

				// Verify all icon sizes were installed
				iconSizes := []string{"16x16", "32x32", "48x48", "128x128", "256x256"}
				for _, size := range iconSizes {
					iconPath := filepath.Join(xdg.DataHome, "icons", "hicolor", size, "apps", "zaparoo.png")
					_, err = os.Stat(iconPath)
					require.NoError(t, err, "icon %s should be installed", size)
				}
			}

			cmd.AssertExpectations(t)
		})
	}
}

func TestInstallService(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	tests := []struct {
		setupMock     func(*mocks.MockCommandExecutor)
		name          string
		errorContains string
		setupBinary   bool
		expectError   bool
	}{
		{
			name:        "successful installation",
			setupBinary: true,
			setupMock: func(_ *mocks.MockCommandExecutor) {
				// Commands succeed by default from helper
			},
			expectError: false,
		},
		{
			name:        "daemon-reload fails silently",
			setupBinary: true,
			setupMock: func(cmd *mocks.MockCommandExecutor) {
				// Override default - clear and set specific expectation
				cmd.ExpectedCalls = nil
				cmd.On("Run", mock.Anything, "systemctl", mock.AnythingOfType("[]string")).
					Return(errors.New("systemctl not found"))
			},
			expectError: false, // Command errors are ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() - subtests share filesystem state

			binPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")

			// Clean up binary before and after test
			defer func() { _ = os.Remove(binPath) }()
			_ = os.Remove(binPath) // Clean any existing binary first

			// Setup: create binary if needed
			if tt.setupBinary {
				//nolint:gosec // Test directory needs to be accessible
				_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
				//nolint:gosec // Test binary needs to be executable
				_ = os.WriteFile(binPath, []byte("test"), 0o755)
			}

			cmd := helpers.NewMockCommandExecutor()
			if tt.setupMock != nil {
				tt.setupMock(cmd)
			}

			err := doInstallService(cmd)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify service file was created
				servicePath := filepath.Join(xdg.ConfigHome, "systemd", "user", "zaparoo.service")
				_, err := os.Stat(servicePath)
				require.NoError(t, err, "service file should be installed")

				content, readErr := os.ReadFile(servicePath) //nolint:gosec // reads installed test fixture
				require.NoError(t, readErr, "should be able to read service file")
				serviceContent := string(content)
				execPath, execErr := os.Executable()
				require.NoError(t, execErr)
				execPath, execErr = filepath.EvalSymlinks(execPath)
				require.NoError(t, execErr)
				assert.Contains(t, serviceContent, "Type=simple")
				assert.Contains(t, serviceContent, "ExecStart="+execPath+" -daemon")
				assert.Contains(t, serviceContent, "TimeoutStopSec=15s")
				assert.Contains(t, serviceContent, "KillMode=control-group")
				assert.Contains(t, serviceContent, "Avoid systemd sandboxing here")

				restrictedDirectives := []string{
					"PrivateTmp=",
					"ReadWritePaths=",
					"ProtectKernelTunables=",
					"ProtectKernelModules=",
					"ProtectControlGroups=",
					"ProtectKernelLogs=",
					"PrivateDevices=",
					"DevicePolicy=",
					"DeviceAllow=",
					"NoNewPrivileges=",
					"ProtectClock=",
					"ProtectHostname=",
					"RestrictSUIDSGID=",
					"RestrictRealtime=",
					"LockPersonality=",
					"RestrictAddressFamilies=",
					"SystemCallFilter=",
					"SystemCallArchitectures=",
				}
				for _, directive := range restrictedDirectives {
					assert.NotContains(t, serviceContent, directive)
				}
			}

			cmd.AssertExpectations(t)
		})
	}
}

func TestInstallDesktop(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip on Windows - Unix file permissions don't work the same way
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Linux-specific test on Windows")
	}

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	tests := []struct {
		name          string
		errorContains string
		setupBinary   bool
		expectError   bool
	}{
		{
			name:        "successful installation",
			setupBinary: true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() - subtests share filesystem state

			binPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")

			// Clean up binary before and after test
			defer func() { _ = os.Remove(binPath) }()
			_ = os.Remove(binPath) // Clean any existing binary first

			// Setup: create binary if needed
			if tt.setupBinary {
				//nolint:gosec // Test directory needs to be accessible
				_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
				//nolint:gosec // Test binary needs to be executable
				_ = os.WriteFile(binPath, []byte("test"), 0o755)
			}

			// InstallDesktop doesn't use command executor (no commands called)
			err := InstallDesktop()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify desktop shortcut was created
				desktopPath := filepath.Join(xdg.Home, "Desktop", "zaparoo.desktop")
				_, err := os.Stat(desktopPath)
				require.NoError(t, err, "desktop shortcut should be installed")

				// Verify it's executable
				info, _ := os.Stat(desktopPath)
				assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "desktop shortcut should be executable")
			}
		})
	}
}

func TestInstallUdevRules(t *testing.T) {
	const (
		customOriginalRule = "# user-managed original rule\n"
		customKillerRule   = "# user-managed PN532Killer rule\n"
		expectedKillerRule = "# Allow user access to PN532Killer USB UART readers\n" +
			"SUBSYSTEMS==\"usb\", ATTRS{idVendor}==\"1a86\", ATTRS{idProduct}==\"55d3\", " +
			"ATTRS{product}==\"PN532Killer-UART\", MODE=\"0660\", TAG+=\"uaccess\"\n"
	)

	tests := []struct {
		setup               func(*testing.T, []udevRuleFile)
		name                string
		wantOriginalContent string
		wantKillerContent   string
		wantReload          bool
	}{
		{
			name:                "fresh installation creates both files",
			wantOriginalContent: udevFile,
			wantKillerContent:   expectedKillerRule,
			wantReload:          true,
		},
		{
			name: "upgrade preserves original and adds PN532Killer rule",
			setup: func(t *testing.T, rules []udevRuleFile) {
				t.Helper()
				require.NoError(t, os.WriteFile(rules[0].path, []byte(customOriginalRule), 0o644))
			},
			wantOriginalContent: customOriginalRule,
			wantKillerContent:   expectedKillerRule,
			wantReload:          true,
		},
		{
			name: "reinstallation preserves existing files",
			setup: func(t *testing.T, rules []udevRuleFile) {
				t.Helper()
				require.NoError(t, os.WriteFile(rules[0].path, []byte(customOriginalRule), 0o644))
				require.NoError(t, os.WriteFile(rules[1].path, []byte(customKillerRule), 0o644))
			},
			wantOriginalContent: customOriginalRule,
			wantKillerContent:   customKillerRule,
			wantReload:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := testUdevRuleFiles(t)
			if tt.setup != nil {
				tt.setup(t, rules)
			}

			cmd := &mocks.MockCommandExecutor{}
			if tt.wantReload {
				expectUdevReload(cmd)
			}

			err := installUdevRules(cmd, rules)
			require.NoError(t, err)

			originalContent, err := os.ReadFile(rules[0].path) //nolint:gosec // Test fixture path.
			require.NoError(t, err)
			assert.Equal(t, tt.wantOriginalContent, string(originalContent))
			killerContent, err := os.ReadFile(rules[1].path) //nolint:gosec // Test fixture path.
			require.NoError(t, err)
			assert.Equal(t, tt.wantKillerContent, string(killerContent))
			if !tt.wantReload {
				assert.Empty(t, cmd.Calls)
			}
			cmd.AssertExpectations(t)
		})
	}
}

func TestUninstallUdevRules(t *testing.T) {
	rules := testUdevRuleFiles(t)
	for _, rule := range rules {
		require.NoError(t, os.WriteFile(rule.path, []byte(rule.content), 0o644))
	}

	cmd := &mocks.MockCommandExecutor{}
	expectUdevReload(cmd)

	err := uninstallUdevRules(cmd, rules)
	require.NoError(t, err)
	for _, rule := range rules {
		_, statErr := os.Stat(rule.path)
		assert.True(t, os.IsNotExist(statErr), "%s should be removed", rule.path)
	}
	cmd.AssertExpectations(t)
}

func testUdevRuleFiles(t *testing.T) []udevRuleFile {
	t.Helper()

	dir := t.TempDir()
	return []udevRuleFile{
		{path: filepath.Join(dir, "60-zaparoo.rules"), content: udevFile},
		{path: filepath.Join(dir, "60-zaparoo-pn532killer.rules"), content: pn532KillerUdevFile},
	}
}

func expectUdevReload(cmd *mocks.MockCommandExecutor) {
	cmd.On("Run", mock.Anything, "udevadm", []string{"control", "--reload-rules"}).Return(nil).Once()
	cmd.On("Run", mock.Anything, "udevadm", []string{"trigger"}).Return(nil).Once()
}

func TestUninstallApplication(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	t.Run("removes all application files", func(t *testing.T) {
		// Cannot use t.Parallel() - test shares filesystem state

		cmd := helpers.NewMockCommandExecutor()

		// Create dummy files to be removed
		binPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")
		desktopPath := filepath.Join(xdg.DataHome, "applications", "zaparoo.desktop")

		//nolint:gosec // Test directory needs to be accessible
		_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
		//nolint:gosec // Test directory needs to be accessible
		_ = os.MkdirAll(filepath.Dir(desktopPath), 0o755)

		// Create icon files at all sizes
		iconSizes := []string{"16x16", "32x32", "48x48", "128x128", "256x256"}
		for _, size := range iconSizes {
			iconPath := filepath.Join(xdg.DataHome, "icons", "hicolor", size, "apps", "zaparoo.png")
			//nolint:gosec // Test directory needs to be accessible
			_ = os.MkdirAll(filepath.Dir(iconPath), 0o755)
			//nolint:gosec // Test file permissions
			_ = os.WriteFile(iconPath, []byte("test"), 0o644)
		}

		//nolint:gosec // Test binary needs to be executable
		_ = os.WriteFile(binPath, []byte("test"), 0o755)
		//nolint:gosec // Test file permissions
		_ = os.WriteFile(desktopPath, []byte("test"), 0o644)

		err := doUninstallApplication(cmd)
		require.NoError(t, err)

		// Verify files were removed
		_, err = os.Stat(binPath)
		assert.True(t, os.IsNotExist(err), "binary should be removed")

		_, err = os.Stat(desktopPath)
		assert.True(t, os.IsNotExist(err), "desktop file should be removed")

		// Verify all icon sizes were removed
		for _, size := range iconSizes {
			iconPath := filepath.Join(xdg.DataHome, "icons", "hicolor", size, "apps", "zaparoo.png")
			_, err = os.Stat(iconPath)
			assert.True(t, os.IsNotExist(err), "icon %s should be removed", size)
		}

		cmd.AssertExpectations(t)
	})
}

func TestUninstallService(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	t.Run("stops service and removes service file", func(t *testing.T) {
		// Cannot use t.Parallel() - test shares filesystem state

		cmd := helpers.NewMockCommandExecutor()
		// Clear default and set specific expectations
		cmd.ExpectedCalls = nil
		cmd.On("Run", mock.Anything, "systemctl", []string{"--user", "stop", "zaparoo"}).Return(nil).Once()
		cmd.On("Run", mock.Anything, "systemctl", []string{"--user", "disable", "zaparoo"}).Return(nil).Once()
		cmd.On("Run", mock.Anything, "systemctl", []string{"--user", "daemon-reload"}).Return(nil).Once()

		// Create dummy service file
		servicePath := filepath.Join(xdg.ConfigHome, "systemd", "user", "zaparoo.service")
		//nolint:gosec // Test directory needs to be accessible
		_ = os.MkdirAll(filepath.Dir(servicePath), 0o755)
		//nolint:gosec // Test file permissions
		_ = os.WriteFile(servicePath, []byte("test"), 0o644)

		err := doUninstallService(cmd)
		require.NoError(t, err)

		// Verify service file was removed
		_, err = os.Stat(servicePath)
		assert.True(t, os.IsNotExist(err), "service file should be removed")

		cmd.AssertExpectations(t)
	})
}

func TestUninstallDesktop(t *testing.T) {
	// Cannot use t.Parallel() - tests modify shared XDG paths

	// Skip if running as root (cannot test non-root requirement)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test non-root installer as root")
	}

	t.Run("removes desktop shortcut", func(t *testing.T) {
		// Cannot use t.Parallel() - test shares filesystem state

		// Create dummy desktop shortcut
		desktopPath := filepath.Join(xdg.Home, "Desktop", "zaparoo.desktop")
		//nolint:gosec // Test directory needs to be accessible
		_ = os.MkdirAll(filepath.Dir(desktopPath), 0o755)
		//nolint:gosec // Test file needs to be executable
		_ = os.WriteFile(desktopPath, []byte("test"), 0o755)

		err := UninstallDesktop()
		require.NoError(t, err)

		// Verify desktop shortcut was removed
		_, err = os.Stat(desktopPath)
		assert.True(t, os.IsNotExist(err), "desktop shortcut should be removed")
	})
}
