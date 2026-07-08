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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
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

	reader := NewReader(&config.Instance{})
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

	const devPath = "/fake/dev"
	reader := &FileReader{
		fsChecker: &mockFSChecker{
			readDirFunc: func(path string) ([]os.DirEntry, error) {
				require.Equal(t, "/fake/sys/block", path)
				return []os.DirEntry{mockDirEntry{name: "sr0"}}, nil
			},
			statFunc: func(path string) (os.FileInfo, error) {
				require.Equal(t, devPath+"/sr0", path)
				return &mockFileInfo{}, nil
			},
		},
		sysBlockPath: "/fake/sys/block",
		devPath:      devPath,
	}

	path := devPath + "/sr0"
	assert.Empty(t, reader.Detect([]string{path}))
	assert.Equal(t, "opticaldrive:"+path, reader.Detect(nil))
}

func TestOpticalPathExcluded(t *testing.T) {
	t.Parallel()

	const path = "/dev/sr0"
	assert.True(t, opticalPathExcluded(path, []string{path}))
	assert.True(t, opticalPathExcluded(path, []string{"opticaldrive:" + path}))
	assert.False(t, opticalPathExcluded(path, []string{"opticaldrive:/dev/sr1"}))
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

	require.Len(t, capabilities, 1)
	assert.Contains(t, capabilities, readers.CapabilityRemovable)
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

func TestResolveTokenID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		uuid       string
		label      string
		idSource   string
		expectedID string
	}{
		{
			name:       "uuid source uses uuid",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceUUID,
			expectedID: "abc123",
		},
		{
			name:       "uuid source requires uuid",
			uuid:       "",
			label:      "my-disc",
			idSource:   IDSourceUUID,
			expectedID: "",
		},
		{
			name:       "label source uses label",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceLabel,
			expectedID: "my-disc",
		},
		{
			name:       "label source requires label",
			uuid:       "abc123",
			label:      "",
			idSource:   IDSourceLabel,
			expectedID: "",
		},
		{
			name:       "merged source uses both",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   IDSourceMerged,
			expectedID: "abc123/my-disc",
		},
		{
			name:       "merged source requires uuid",
			uuid:       "",
			label:      "my-disc",
			idSource:   IDSourceMerged,
			expectedID: "",
		},
		{
			name:       "merged source requires label",
			uuid:       "abc123",
			label:      "",
			idSource:   IDSourceMerged,
			expectedID: "",
		},
		{
			name:       "default source is strict merged",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   "",
			expectedID: "abc123/my-disc",
		},
		{
			name:       "unknown source is strict merged",
			uuid:       "abc123",
			label:      "my-disc",
			idSource:   "unknown",
			expectedID: "abc123/my-disc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := resolveTokenID(tt.uuid, tt.label, tt.idSource)
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

func TestReadISO9660Identity(t *testing.T) {
	t.Parallel()

	image := newTestISO9660Image("SCES-01420", "1998102813221100", "1998010100000000")
	identity, found, err := readTestISO9660Identity(bytes.NewReader(image))

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "SCES-01420", identity.Label)
	assert.Equal(t, "1998-10-28-13-22-11-00", identity.UUID)
}

func TestReadISO9660Identity_FallsBackToCreatedDate(t *testing.T) {
	t.Parallel()

	image := newTestISO9660Image("SCES-01420", "0000000000000000", "1998010100000000")
	identity, found, err := readTestISO9660Identity(bytes.NewReader(image))

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "1998-01-01-00-00-00-00", identity.UUID)
}

func TestReadISO9660Identity_NotFound(t *testing.T) {
	t.Parallel()

	identity, found, err := readTestISO9660Identity(
		bytes.NewReader(make([]byte, iso9660SuperblockOffset+iso9660MaxDescriptors*iso9660SectorSize)),
	)

	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, identity)
}

func TestReadISO9660Identity_ShortReadIsMiss(t *testing.T) {
	t.Parallel()

	identity, found, err := readTestISO9660Identity(bytes.NewReader(make([]byte, iso9660SuperblockOffset+1)))

	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, identity)
}

type testReaderAtContextAdapter struct {
	reader *bytes.Reader
}

func (r testReaderAtContextAdapter) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	n, err := r.reader.ReadAt(p, off)
	if err != nil {
		return n, fmt.Errorf("read test data: %w", err)
	}
	return n, nil
}

func readTestISO9660Identity(reader *bytes.Reader) (discIdentity, bool, error) {
	return readISO9660IdentityContext(context.Background(), testReaderAtContextAdapter{reader: reader})
}

func newTestISO9660Image(label, modified, created string) []byte {
	image := make([]byte, iso9660SuperblockOffset+iso9660SectorSize)
	desc := image[iso9660SuperblockOffset:]
	desc[0] = iso9660DescriptorTypePrimary
	copy(desc[1:6], "CD001")
	desc[6] = 1
	copy(desc[iso9660VolumeIDOffset:iso9660VolumeIDOffset+iso9660VolumeIDSize], "                                ")
	copy(desc[iso9660VolumeIDOffset:iso9660VolumeIDOffset+iso9660VolumeIDSize], label)
	copy(desc[iso9660ModifiedOffset:iso9660ModifiedOffset+16], modified)
	copy(desc[iso9660CreatedOffset:iso9660CreatedOffset+16], created)
	return image
}

type mockFSChecker struct {
	statFunc    func(path string) (os.FileInfo, error)
	readDirFunc func(path string) ([]os.DirEntry, error)
}

func (m *mockFSChecker) Stat(path string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(path)
	}
	// Default behavior for mock when no function is set - file exists
	return &mockFileInfo{}, nil
}

func (m *mockFSChecker) ReadDir(path string) ([]os.DirEntry, error) {
	if m.readDirFunc != nil {
		return m.readDirFunc(path)
	}
	return nil, nil
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string    { return m.name }
func (m mockDirEntry) IsDir() bool     { return m.isDir }
func (mockDirEntry) Type() os.FileMode { return 0 }
func (mockDirEntry) Info() (os.FileInfo, error) {
	return &mockFileInfo{}, nil
}

type mockFileInfo struct{}

func (*mockFileInfo) Name() string       { return "mock" }
func (*mockFileInfo) Size() int64        { return 0 }
func (*mockFileInfo) Mode() os.FileMode  { return 0 }
func (*mockFileInfo) ModTime() time.Time { return time.Time{} }
func (*mockFileInfo) IsDir() bool        { return false }
func (*mockFileInfo) Sys() any           { return nil }

type mockDiscIdentifier struct {
	identifyFunc func(ctx context.Context, devicePath string) (discIdentity, error)
}

func (m *mockDiscIdentifier) Identify(ctx context.Context, devicePath string) (discIdentity, error) {
	if m.identifyFunc != nil {
		return m.identifyFunc(ctx, devicePath)
	}
	return discIdentity{}, nil
}

type blockingContextReader struct {
	closed atomic.Bool
	reads  atomic.Int32
}

func (r *blockingContextReader) ReadAtContext(ctx context.Context, _ []byte, _ int64) (int, error) {
	r.reads.Add(1)
	<-ctx.Done()
	return 0, ctx.Err()
}

func (r *blockingContextReader) Close() error {
	r.closed.Store(true)
	return nil
}

func newTestReader(cfg *config.Instance) *FileReader {
	reader := NewReader(cfg)
	reader.gameIDProbe = func(string) []readers.ScanProperty { return nil }
	return reader
}

func overrideUnixDiscIO(
	t *testing.T,
	pread func(int, []byte, int64) (int, error),
	closeFn func(int) error,
) {
	t.Helper()
	oldPread := unixPread
	oldClose := unixClose
	oldDelay := discReadRetryDelay
	unixPread = pread
	unixClose = closeFn
	discReadRetryDelay = time.Millisecond
	t.Cleanup(func() {
		unixPread = oldPread
		unixClose = oldClose
		discReadRetryDelay = oldDelay
	})
}

func TestUnixDiscDeviceReaderReadAtContext_PartialReads(t *testing.T) {
	calls := 0
	offsets := make([]int64, 0, 2)
	overrideUnixDiscIO(t, func(_ int, p []byte, off int64) (int, error) {
		calls++
		offsets = append(offsets, off)
		if calls == 1 {
			return copy(p, "ab"), nil
		}
		return copy(p, "cd"), nil
	}, unix.Close)

	buf := make([]byte, 4)
	n, err := (&unixDiscDeviceReader{fd: -1}).ReadAtContext(context.Background(), buf, 10)

	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "abcd", string(buf))
	assert.Equal(t, []int64{10, 12}, offsets)
}

func TestUnixDiscDeviceReaderReadAtContext_RetriesTemporaryErrors(t *testing.T) {
	calls := 0
	overrideUnixDiscIO(t, func(_ int, p []byte, _ int64) (int, error) {
		calls++
		switch calls {
		case 1:
			return 0, unix.EAGAIN
		case 2:
			return 0, unix.EINTR
		default:
			return copy(p, "ok"), nil
		}
	}, unix.Close)

	buf := make([]byte, 2)
	n, err := (&unixDiscDeviceReader{fd: -1}).ReadAtContext(context.Background(), buf, 0)

	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "ok", string(buf))
	assert.Equal(t, 3, calls)
}

func TestUnixDiscDeviceReaderReadAtContext_ZeroReadIsEOF(t *testing.T) {
	overrideUnixDiscIO(t, func(_ int, _ []byte, _ int64) (int, error) {
		return 0, nil
	}, unix.Close)

	n, err := (&unixDiscDeviceReader{fd: -1}).ReadAtContext(context.Background(), make([]byte, 1), 0)

	assert.Equal(t, 0, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestUnixDiscDeviceReaderReadAtContext_CancelDuringRetry(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	overrideUnixDiscIO(t, func(_ int, _ []byte, _ int64) (int, error) {
		calls++
		cancel()
		return 0, unix.EAGAIN
	}, unix.Close)

	n, err := (&unixDiscDeviceReader{fd: -1}).ReadAtContext(ctx, make([]byte, 1), 0)

	assert.Equal(t, 0, n)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestUnixDiscDeviceReaderClose_ReturnsCloseError(t *testing.T) {
	overrideUnixDiscIO(t, unix.Pread, func(int) error {
		return unix.EBADF
	})

	err := (&unixDiscDeviceReader{fd: -1}).Close()

	require.ErrorIs(t, err, unix.EBADF)
}

func TestDefaultDiscIdentifierIdentify_TimeoutDoesNotLeakGoroutine(t *testing.T) {
	reader := &blockingContextReader{}
	oldOpen := openDiscDeviceReader
	openDiscDeviceReader = func(string) (contextReaderAtCloser, error) {
		return reader, nil
	}
	t.Cleanup(func() {
		openDiscDeviceReader = oldOpen
	})

	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := defaultDiscIdentifier{}.Identify(ctx, "/dev/sr0")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), reader.reads.Load())
	assert.True(t, reader.closed.Load())
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= before+1
	}, time.Second, 10*time.Millisecond)
}

func TestOpen_InvalidPath_NotAbsolute(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "relative/path",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})

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

	err := reader.Open(device, scanQueue, readers.OpenOpts{})

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

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{UUID: "abc-123-uuid", Label: "My Disc"}, nil
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceMerged,
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	// Wait for disc detection (1 second poll interval)
	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)

	assert.NotNil(t, scan.Token)
	assert.Equal(t, "abc-123-uuid/My Disc", scan.Token.UID)
	assert.Equal(t, "disc", scan.Token.Type)
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
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

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{UUID: "test-uuid", Label: "test-label"}, nil
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uuid/test-label", scan1.Token.UID)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
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

func TestOpen_DiscIdentificationFailsNormalDiscRemoval(t *testing.T) {
	t.Parallel()

	callCount := 0
	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			// Device always exists (no reader error)
			return &mockFileInfo{}, nil
		},
	}

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			callCount++
			if callCount == 1 {
				return discIdentity{UUID: "test-uuid", Label: "test-label"}, nil
			}
			return discIdentity{}, assert.AnError
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "test-uuid/test-label", scan1.Token.UID)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

	// Second scan: disc removed (identification fails but device exists)
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

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			callCount++
			if callCount == 1 {
				return discIdentity{UUID: "test-uuid", Label: "test-label"}, nil
			}
			return discIdentity{}, nil
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	// First scan: disc detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

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

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{UUID: "my-uuid", Label: "my-label"}, nil
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceUUID,
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "my-uuid", scan.Token.UID, "should use UUID only")
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

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

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{UUID: "my-uuid", Label: "my-label"}, nil
		},
	}

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver:   "optical_drive",
		Path:     "/dev/sr0",
		IDSource: IDSourceLabel,
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 2*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "my-label", scan.Token.UID, "should use LABEL only")
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

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

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
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

	reader := newTestReader(&config.Instance{})
	reader.fsChecker = mockFS
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	// Wait for first poll - no token, device missing → no scan sent
	time.Sleep(1500 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_NoRepeatedGameIDProbeForUnchangedUnidentifiedDisc(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{}, nil
		},
	}

	var probeCount atomic.Int32
	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	reader.gameIDProbe = func(_ string) []readers.ScanProperty {
		probeCount.Add(1)
		return nil
	}
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	time.Sleep(3500 * time.Millisecond)
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	assert.Equal(t, int32(1), probeCount.Load(),
		"gameid probe should only run once for an unchanged, unidentified disc")

	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_GameIDPropertyDoesNotBecomeTokenID(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			return discIdentity{}, errors.New("disc identity unavailable")
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	reader.gameIDProbe = func(_ string) []readers.ScanProperty {
		return []readers.ScanProperty{{System: "PSX", Name: "gameid", Value: "SCES-01420"}}
	}
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 1500*time.Millisecond)
	require.NotNil(t, scan.Token)
	assert.Empty(t, scan.Token.UID)
	require.Len(t, scan.Properties, 1)
	assert.Equal(t, "SCES-01420", scan.Properties[0].Value)

	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_GameIDPropertyKeepsInitialTokenIDStable(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	var callCount atomic.Int32
	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			call := callCount.Add(1)
			if call == 1 {
				return discIdentity{}, errors.New("disc identity unavailable")
			}
			return discIdentity{UUID: "1998-10-28-13-22-11-00", Label: "SCES-01420"}, nil
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	reader.gameIDProbe = func(_ string) []readers.ScanProperty {
		return []readers.ScanProperty{{System: "PSX", Name: "gameid", Value: "SCES-01420"}}
	}
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 1500*time.Millisecond)
	require.NotNil(t, scan.Token)
	assert.Empty(t, scan.Token.UID)
	require.Len(t, scan.Properties, 1)

	testutils.AssertNoScan(t, scanQueue, 2500*time.Millisecond)

	err = reader.Close()
	require.NoError(t, err)
}

func TestOpen_KeepsExistingTokenWhenStableIDTemporarilyUnavailable(t *testing.T) {
	t.Parallel()

	mockFS := &mockFSChecker{
		statFunc: func(_ string) (os.FileInfo, error) {
			return &mockFileInfo{}, nil
		},
	}

	var callCount atomic.Int32
	mockIdentifier := &mockDiscIdentifier{
		identifyFunc: func(_ context.Context, _ string) (discIdentity, error) {
			call := callCount.Add(1)
			if call == 1 {
				return discIdentity{UUID: "1998-10-28-13-22-11-00", Label: "SCES-01420"}, nil
			}
			return discIdentity{}, errors.New("disc identity unavailable")
		},
	}

	reader := NewReader(&config.Instance{})
	reader.fsChecker = mockFS
	reader.discIdentifier = mockIdentifier
	reader.gameIDProbe = func(_ string) []readers.ScanProperty {
		return []readers.ScanProperty{{System: "PSX", Name: "gameid", Value: "SCES-01420"}}
	}
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "optical_drive",
		Path:   "/dev/sr0",
	}

	err := reader.Open(device, scanQueue, readers.OpenOpts{})
	require.NoError(t, err)

	scan := testutils.AssertScanReceived(t, scanQueue, 1500*time.Millisecond)
	require.NotNil(t, scan.Token)
	assert.Equal(t, "1998-10-28-13-22-11-00/SCES-01420", scan.Token.UID)
	require.Len(t, scan.Properties, 1)

	testutils.AssertNoScan(t, scanQueue, 2500*time.Millisecond)

	err = reader.Close()
	require.NoError(t, err)
}
