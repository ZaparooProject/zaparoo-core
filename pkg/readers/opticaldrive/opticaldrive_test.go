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

//go:build linux

package opticaldrive

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	metadata := reader.Metadata()

	assert.Equal(t, "opticaldrive", metadata.ID)
	assert.Equal(t, "Optical drive CD/DVD reader", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	ids := reader.IDs()

	require.Len(t, ids, 2)
	assert.Equal(t, "opticaldrive", ids[0])
	assert.Equal(t, "optical_drive", ids[1])
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	result := reader.Detect([]string{"any", "input"})

	assert.Empty(t, result, "optical drive does not support auto-detection")
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}

	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "optical drive reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &FileReader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		polling  bool
		expected bool
	}{
		{
			name:     "not polling",
			polling:  false,
			expected: false,
		},
		{
			name:     "polling",
			polling:  true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &FileReader{
				polling: tt.polling,
			}

			assert.Equal(t, tt.expected, reader.Connected())
		})
	}
}

func TestGetID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		uuid       string
		label      string
		idSource   string
		expectedID string
	}{
		{
			name:       "only uuid",
			uuid:       "abc123",
			label:      "",
			idSource:   IDSourceUUID,
			expectedID: "abc123",
		},
		{
			name:       "only label",
			uuid:       "",
			label:      "my-disc",
			idSource:   IDSourceLabel,
			expectedID: "my-disc",
		},
		{
			name:       "both uuid and label - uuid source",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceUUID,
			expectedID: "abc123",
		},
		{
			name:       "both uuid and label - label source",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceLabel,
			expectedID: "my-disc",
		},
		{
			name:       "both uuid and label - merged source",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceMerged,
			expectedID: "abc123/my-disc",
		},
		{
			name:       "both uuid and label - default (merged)",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   "",
			expectedID: "abc123/my-disc",
		},
		{
			name:       "both uuid and label - unknown source (defaults to merged)",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   "unknown",
			expectedID: "abc123/my-disc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &FileReader{
				device: config.ReadersConnect{
					IDSource: tt.idSource,
				},
			}

			// Create the getID function as it appears in Open()
			getID := func(uuid string, label string) string {
				if uuid == "" {
					return label
				} else if label == "" {
					return uuid
				}

				switch reader.device.IDSource {
				case IDSourceUUID:
					return uuid
				case IDSourceLabel:
					return label
				case IDSourceMerged:
					return uuid + MergedIDSeparator + label
				default:
					return uuid + MergedIDSeparator + label
				}
			}

			result := getID(tt.uuid, tt.label)
			assert.Equal(t, tt.expectedID, result)
		})
	}
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "disc", TokenType)
	assert.Equal(t, "uuid", IDSourceUUID)
	assert.Equal(t, "label", IDSourceLabel)
	assert.Equal(t, "merged", IDSourceMerged)
	assert.Equal(t, "/", MergedIDSeparator)
}

type mockFSChecker struct {
	statFunc func(path string) (os.FileInfo, error)
}

func (m *mockFSChecker) Stat(path string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(path)
	}
	// Default behavior for mock when no function is set - file exists
	return &mockFileInfo{}, nil
}

type mockFileInfo struct{}

func (*mockFileInfo) Name() string       { return "mock" }
func (*mockFileInfo) Size() int64        { return 0 }
func (*mockFileInfo) Mode() os.FileMode  { return 0 }
func (*mockFileInfo) ModTime() time.Time { return time.Time{} }
func (*mockFileInfo) IsDir() bool        { return false }
func (*mockFileInfo) Sys() any           { return nil }

type mockCommandRunner struct {
	blkidFunc func(ctx context.Context, valueType, devicePath string) ([]byte, error)
}

func (m *mockCommandRunner) RunBlkid(ctx context.Context, valueType, devicePath string) ([]byte, error) {
	if m.blkidFunc != nil {
		return m.blkidFunc(ctx, valueType, devicePath)
	}
	// Default behavior for mock when no function is set
	return []byte{}, nil
}

func TestOpen_InvalidPath_NotAbsolute(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "relative/path",
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
}

func TestOpen_ParentDirectoryDoesNotExist(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat device parent directory")
}

func TestOpen_SuccessfulDiscDetection(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			// Parent directory and device both exist
			return &mockFileInfo{}, nil
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			if valueType == "UUID" {
				return []byte("abc-123-uuid\n"), nil
			}
			return []byte("My Disc\n"), nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceMerged,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Wait for disc detection (1 second poll interval)
	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)

	assert.NotNil(t, scan.Token)
	assert.Equal(t, "abc-123-uuid/My Disc", scan.Token.UID)
	assert.Equal(t, "disc", scan.Token.Type)
	assert.False(t, scan.ReaderError)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_DeviceDisappearsWithActiveToken(t *testing.T) {
	t.Parallel()

	pollCount := 0
	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			pollCount++
			// First poll: device exists (parent check + first device check)
			// Second poll: device exists
			// Third poll: device disappears (reader error)
			if pollCount <= 3 {
				return &mockFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			if valueType == "UUID" {
				return []byte("test-uuid\n"), nil
			}
			return []byte("test-label\n"), nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uuid/test-label", scan1.Token.UID)
	assert.False(t, scan1.ReaderError)

	// Wait for device to disappear and ReaderError scan to be sent
	// This tests the fix for issue #326
	scan2 := testutils.AssertScanReceived(t, scanQueue, 3*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil on reader error")
	assert.True(t, scan2.ReaderError, "ReaderError should be true to prevent on_remove execution")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_BlkidFailsNormalDiscRemoval(t *testing.T) {
	t.Parallel()

	callCount := 0
	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			// Device always exists (no reader error)
			return &mockFileInfo{}, nil
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			callCount++
			// First call: successful detection
			if callCount == 1 || callCount == 2 {
				if valueType == "UUID" {
					return []byte("test-uuid\n"), nil
				}
				return []byte("test-label\n"), nil
			}
			// Subsequent calls: blkid fails (disc removed normally)
			return nil, assert.AnError
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uuid/test-label", scan1.Token.UID)

	// Second scan: disc removed (blkid fails but device exists)
	// This should be a normal removal, NOT a ReaderError
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil on disc removal")
	assert.False(t, scan2.ReaderError, "ReaderError should be false for normal disc removal")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_EmptyUUIDAndLabel_RemovesToken(t *testing.T) {
	t.Parallel()

	callCount := 0
	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			callCount++
			// First poll: return valid UUID/LABEL
			if callCount == 1 || callCount == 2 {
				if valueType == "UUID" {
					return []byte("test-uuid\n"), nil
				}
				return []byte("test-label\n"), nil
			}
			// Second poll: return empty values
			return []byte(""), nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)

	// Second scan: empty UUID/LABEL removes token
	scan2 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.Nil(t, scan2.Token, "token should be removed when UUID and LABEL are empty")
	assert.False(t, scan2.ReaderError)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_IDSourceUUID(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			if valueType == "UUID" {
				return []byte("my-uuid\n"), nil
			}
			return []byte("my-label\n"), nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceUUID,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "my-uuid", scan.Token.UID, "should use UUID only")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_IDSourceLabel(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	mockCmd := &mockCommandRunner{
		blkidFunc: func(_ context.Context, valueType string, _ string) ([]byte, error) {
			if valueType == "UUID" {
				return []byte("my-uuid\n"), nil
			}
			return []byte("my-label\n"), nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.commandRunner = mockCmd
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceLabel,
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "my-label", scan.Token.UID, "should use LABEL only")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_InvalidDevicePath_NotUnderDev(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/tmp/not-a-device",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// The reader should reject the path during polling (not under /dev/)
	// No scans should be sent
	time.Sleep(1500 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_DeviceDisappearsWithoutToken_NoReaderError(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(path string) (os.FileInfo, error) {
			// Device never exists after parent check
			if path == "/dev" {
				return &mockFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Wait for first poll - no token, device missing → no scan sent
	time.Sleep(1500 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}
