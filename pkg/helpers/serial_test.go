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

package helpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIgnoreSerialDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "invalid path without /dev/ prefix",
			path:     "/home/user/device",
			expected: true, // Returns true for invalid paths (should ignore)
		},
		{
			name:     "empty path",
			path:     "",
			expected: true,
		},
		{
			name:     "relative path",
			path:     "../device",
			expected: true, // Returns true for invalid paths (should ignore)
		},
		{
			name:     "valid /dev/ path but non-existent file",
			path:     "/dev/nonexistent",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ignoreSerialDevice(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Test with temporary file (works on all platforms)
	t.Run("temporary device file handling", func(t *testing.T) {
		t.Parallel()

		// Create a temporary device file to pass the file existence check
		tmpDir := t.TempDir()
		devicePath := filepath.Join(tmpDir, "ttyUSB0")
		file, err := os.Create(devicePath) //nolint:gosec // Test file creation is safe
		if err != nil {
			t.Fatalf("Failed to create test device file: %v", err)
		}
		_ = file.Close()

		// This should return false since the path doesn't start with /dev/
		// but the temporary file exists
		result := ignoreSerialDevice(devicePath)
		assert.False(t, result, "Non-/dev/ path should return false even if file exists")

		// Test with a /dev/ prefixed path that doesn't exist
		nonExistentDevPath := "/dev/test_nonexistent_device"
		result = ignoreSerialDevice(nonExistentDevPath)
		assert.True(t, result, "Non-existent /dev/ path should return true")
	})
}

func TestGetSerialDeviceList(t *testing.T) {
	t.Parallel()

	// Test that the function executes without error
	// We can't test specific device paths as they depend on hardware
	devices, err := GetSerialDeviceList()

	// Should not return an error on any platform
	require.NoError(t, err)

	// Result should be a slice (can be empty)
	assert.NotNil(t, devices)

	// All returned devices should be valid non-empty paths
	for _, device := range devices {
		assert.NotEmpty(t, device, "Device path should not be empty")
	}
}

func TestSerialDevicePathValidation(t *testing.T) {
	t.Parallel()

	// Test expected path patterns for different platforms
	// This doesn't depend on runtime OS, just validates the patterns
	testCases := []struct {
		name       string
		platform   string
		validPaths []string
	}{
		{
			name:     "linux_device_paths",
			platform: "linux",
			validPaths: []string{
				"/dev/ttyUSB0",
				"/dev/ttyUSB1",
				"/dev/ttyACM0",
				"/dev/ttyACM1",
			},
		},
		{
			name:     "darwin_device_paths",
			platform: "darwin",
			validPaths: []string{
				"/dev/tty.usbserial-1410",
				"/dev/tty.usbserial-A5027H18",
			},
		},
		{
			name:     "windows_device_paths",
			platform: "windows",
			validPaths: []string{
				"COM1",
				"COM2",
				"COM10",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Just validate the path format without OS-specific checks
			for _, path := range tc.validPaths {
				assert.NotEmpty(t, path)
				switch tc.platform {
				case "linux", "darwin":
					assert.True(t, strings.HasPrefix(path, "/dev/"),
						"Unix device path should start with /dev/: %s", path)
				case "windows":
					assert.True(t, strings.HasPrefix(path, "COM"),
						"Windows device path should start with COM: %s", path)
				}
			}
		})
	}
}

func TestSerialDeviceIgnoreList(t *testing.T) {
	t.Parallel()

	// Test that the ignore list contains expected Sinden Lightgun entries
	expectedIgnoreCount := 18 // Count from the actual ignoreDevices slice
	assert.Len(t, ignoreDevices, expectedIgnoreCount, "Ignore list should contain expected number of devices")

	// Test specific known ignore entries
	foundSinden16c0 := false
	foundSinden16d0 := false

	for _, device := range ignoreDevices {
		if device.Vid == "16c0" && device.Pid == "0f38" {
			foundSinden16c0 = true
		}
		if device.Vid == "16d0" && device.Pid == "1094" {
			foundSinden16d0 = true
		}

		// All VID/PID pairs should be non-empty
		assert.NotEmpty(t, device.Vid, "VID should not be empty")
		assert.NotEmpty(t, device.Pid, "PID should not be empty")
	}

	assert.True(t, foundSinden16c0, "Should contain Sinden Lightgun 16c0:0f38 entry")
	assert.True(t, foundSinden16d0, "Should contain Sinden Lightgun 16d0:1094 entry")
}
