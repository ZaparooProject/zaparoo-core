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

package externaldrive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	// testEventTimeout is the maximum time to wait for an expected event
	testEventTimeout = 2 * time.Second
	// testNoEventTimeout is the time to wait to verify no event occurs
	testNoEventTimeout = 200 * time.Millisecond
)

// testContext returns a context with the specified timeout for test synchronization.
// Using context instead of raw time.After provides better semantics and cancellation support.
func testContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// mockMountDetector implements MountDetector for testing
type mockMountDetector struct {
	eventsChan     chan MountEvent
	unmountsChan   chan string
	forgottenDevs  []string
	forgottenMutex syncutil.Mutex
	started        bool
	stopped        bool
}

func newMockMountDetector() *mockMountDetector {
	return &mockMountDetector{
		eventsChan:    make(chan MountEvent, 10),
		unmountsChan:  make(chan string, 10),
		forgottenDevs: make([]string, 0),
	}
}

func (m *mockMountDetector) Events() <-chan MountEvent {
	return m.eventsChan
}

func (m *mockMountDetector) Unmounts() <-chan string {
	return m.unmountsChan
}

func (m *mockMountDetector) Start() error {
	m.started = true
	return nil
}

func (m *mockMountDetector) Stop() {
	m.stopped = true
	close(m.eventsChan)
	close(m.unmountsChan)
}

func (m *mockMountDetector) Forget(deviceID string) {
	m.forgottenMutex.Lock()
	defer m.forgottenMutex.Unlock()
	m.forgottenDevs = append(m.forgottenDevs, deviceID)
}

func (m *mockMountDetector) wasForgotten(deviceID string) bool {
	m.forgottenMutex.Lock()
	defer m.forgottenMutex.Unlock()
	for _, id := range m.forgottenDevs {
		if id == deviceID {
			return true
		}
	}
	return false
}

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
	assert.NotNil(t, reader.activeTokens)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	metadata := reader.Metadata()

	assert.Equal(t, "externaldrive", metadata.ID)
	assert.Equal(t, "External drive reader (USB sticks, SD cards, external HDDs)", metadata.Description)
	assert.False(t, metadata.DefaultEnabled, "Should be opt-in only")
	assert.True(t, metadata.DefaultAutoDetect, "Should automatically detect mounted devices")
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	require.Len(t, ids, 2)
	assert.Equal(t, "externaldrive", ids[0])
	assert.Equal(t, "external_drive", ids[1])
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 1)
	assert.Contains(t, capabilities, readers.CapabilityRemovable)
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	// Should not panic
	reader.CancelWrite()
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err)
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{})

	// Should return "driver:path" format if platform supports mount detection
	// (will return empty string on platforms without detector implementation)
	// For external_drive, path is empty since it monitors all mounts
	if result != "" {
		assert.Equal(t, DriverID+":", result)
	}
}

func TestDetect_PlatformSupport(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{})

	// On supported platforms (Linux, macOS, Windows), Detect() should succeed
	// and return the driver ID in "driver:path" format (with empty path),
	// verifying that the platform-specific detector can be instantiated successfully.
	assert.NotEmpty(t, result, "Detect() should return driver:path format on supported platforms")
	assert.Equal(t, DriverID+":", result, "Detect() should return the correct driver:path format")
}

func TestDetect_MultipleCalls(t *testing.T) {
	t.Parallel()

	// Call Detect() multiple times to verify caching works
	reader := &Reader{}

	// First call initializes the cache
	result1 := reader.Detect([]string{})

	// Subsequent calls should return the same cached result
	result2 := reader.Detect([]string{})
	result3 := reader.Detect([]string{})

	// All results should be identical (platform check is cached)
	assert.Equal(t, result1, result2, "Detect() should return cached result on second call")
	assert.Equal(t, result1, result3, "Detect() should return cached result on third call")

	// On supported platforms, all should return "external_drive:"
	if result1 != "" {
		assert.Equal(t, DriverID+":", result1)
		assert.Equal(t, DriverID+":", result2)
		assert.Equal(t, DriverID+":", result3)
	}
}

func TestDetect_DetectorInstantiation(t *testing.T) {
	// Verify that NewMountDetector can be called successfully
	detector, err := NewMountDetector()
	require.NoError(t, err, "NewMountDetector should succeed on supported platforms")
	require.NotNil(t, detector, "Detector should not be nil")

	// Clean up
	detector.Stop()

	// Verify the detector provides the expected channels
	events := detector.Events()
	unmounts := detector.Unmounts()
	assert.NotNil(t, events, "Events channel should not be nil")
	assert.NotNil(t, unmounts, "Unmounts channel should not be nil")
}

func TestMountDetector_StartStop(t *testing.T) {
	// Create a new detector
	detector, err := NewMountDetector()
	require.NoError(t, err, "NewMountDetector should succeed")
	require.NotNil(t, detector, "Detector should not be nil")

	// Start the detector
	err = detector.Start()
	require.NoError(t, err, "Detector Start() should succeed")

	// Stop the detector
	detector.Stop()

	// Verify channels are closed after Stop()
	_, eventsOpen := <-detector.Events()
	_, unmountsOpen := <-detector.Unmounts()
	assert.False(t, eventsOpen, "Events channel should be closed after Stop()")
	assert.False(t, unmountsOpen, "Unmounts channel should be closed after Stop()")
}

func TestMountDetector_MultipleStopCalls(t *testing.T) {
	detector, err := NewMountDetector()
	require.NoError(t, err)
	require.NotNil(t, detector)

	err = detector.Start()
	require.NoError(t, err)

	// Multiple Stop() calls should be safe (tests stopOnce)
	detector.Stop()
	detector.Stop()
	detector.Stop()

	// Should not panic or cause issues
}

func TestOpen_InvalidDriver(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: "invalid",
	}

	err := reader.Open(device, scanChan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reader id")
}

func TestOpen_Success(t *testing.T) {
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	// Inject mock detector before Open
	mockDetector := newMockMountDetector()

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	// We can't fully test Open without mocking NewMountDetector,
	// but we can test the setup logic by manually injecting the detector
	reader.detector = mockDetector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = device

	// Verify detector would be started (manual start since we skip Open's NewMountDetector call)
	err := reader.detector.Start()
	require.NoError(t, err)
	assert.True(t, mockDetector.started, "Detector should be started")

	// Clean up
	err = reader.Close()
	require.NoError(t, err)
	assert.True(t, mockDetector.stopped, "Detector should be stopped after Close")
}

func TestClose(t *testing.T) {
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	mockDetector := newMockMountDetector()

	reader.detector = mockDetector
	reader.stopChan = make(chan struct{})

	err := reader.Close()

	require.NoError(t, err)
	assert.True(t, mockDetector.stopped, "Detector should be stopped")
}

func TestProcessEvents_MountEvent(t *testing.T) {
	// Create temp directory with zaparoo.txt
	tempDir := t.TempDir()
	tokenPath := filepath.Join(tempDir, "zaparoo.txt")
	tokenContents := "**launch.system:nes"
	err := os.WriteFile(tokenPath, []byte(tokenContents), 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)
	mockDetector := newMockMountDetector()

	reader.detector = mockDetector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Start event loop
	reader.wg.Add(1)
	go reader.processEvents()

	// Inject mount event
	mockDetector.eventsChan <- MountEvent{
		DeviceID:    "test-device-123",
		MountPath:   tempDir,
		VolumeLabel: "TEST_USB",
		DeviceType:  "USB",
	}

	// Should receive scan with token
	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token, "Should receive token from mount event")
		assert.Equal(t, TokenType, scan.Token.Type)
		assert.Contains(t, scan.Token.Text, tokenContents)
		assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for scan event")
	}

	// Verify token was added to active tokens
	reader.mu.RLock()
	_, exists := reader.activeTokens["test-device-123"]
	reader.mu.RUnlock()
	assert.True(t, exists, "Token should be in active tokens")

	// Clean up
	close(reader.stopChan)
	reader.wg.Wait()
}

func TestProcessEvents_UnmountEvent(t *testing.T) {
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)
	mockDetector := newMockMountDetector()

	reader.detector = mockDetector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Add a token to active tokens
	reader.mu.Lock()
	reader.activeTokens["test-device-456"] = nil
	reader.mu.Unlock()

	// Start event loop
	reader.wg.Add(1)
	go reader.processEvents()

	// Inject unmount event
	mockDetector.unmountsChan <- "test-device-456"

	// Should receive nil token scan (removal)
	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.Nil(t, scan.Token, "Should receive nil token on unmount")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for unmount scan")
	}

	// Verify token was removed from active tokens
	reader.mu.RLock()
	_, exists := reader.activeTokens["test-device-456"]
	reader.mu.RUnlock()
	assert.False(t, exists, "Token should be removed from active tokens")

	// Clean up
	close(reader.stopChan)
	reader.wg.Wait()
}

func TestProcessEvents_StopChannel(t *testing.T) {
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)
	mockDetector := newMockMountDetector()

	reader.detector = mockDetector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Start event loop
	reader.wg.Add(1)
	go reader.processEvents()

	// Close stop channel - should exit cleanly
	close(reader.stopChan)

	// Wait for processEvents to exit - if it hangs here, test will timeout
	done := make(chan struct{})
	go func() {
		reader.wg.Wait()
		close(done)
	}()

	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case <-done:
		// Success - processEvents exited cleanly
	case <-ctx.Done():
		t.Fatal("processEvents did not exit when stopChan was closed")
	}
}

func TestHandleMountEvent_WithValidToken(t *testing.T) {
	// Create temp directory to simulate a mount
	tempDir := t.TempDir()

	// Create zaparoo.txt file
	tokenPath := filepath.Join(tempDir, "zaparoo.txt")
	tokenContents := "**launch.system:nes\n**launch.search:Super Mario"
	err := os.WriteFile(tokenPath, []byte(tokenContents), 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	// Simulate mount event
	event := MountEvent{
		DeviceID:    "test-device-123",
		MountPath:   tempDir,
		VolumeLabel: "TEST_USB",
		DeviceType:  "USB",
	}

	// Handle mount event (with proper WaitGroup management)
	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Check that scan was emitted
	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token)
		assert.Equal(t, TokenType, scan.Token.Type)
		assert.Contains(t, scan.Token.Text, "**launch.system:nes")
		assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for scan event")
	}

	// Check active tokens map
	reader.mu.RLock()
	token, exists := reader.activeTokens["test-device-123"]
	reader.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, token)
}

func TestHandleMountEvent_MissingFile(t *testing.T) {
	// Create temp directory without zaparoo.txt
	tempDir := t.TempDir()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	event := MountEvent{
		DeviceID:    "test-device-456",
		MountPath:   tempDir,
		VolumeLabel: "EMPTY_USB",
		DeviceType:  "USB",
	}

	// Handle mount event (with proper WaitGroup management)
	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should not emit any scan (no file found)
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file is missing")
	case <-ctx.Done():
		// Expected behavior - no event within timeout
	}

	// Check that no token was added
	reader.mu.RLock()
	_, exists := reader.activeTokens["test-device-456"]
	reader.mu.RUnlock()

	assert.False(t, exists)
}

func TestHandleMountEvent_EmptyFile(t *testing.T) {
	// Create temp directory with empty zaparoo.txt
	tempDir := t.TempDir()

	tokenPath := filepath.Join(tempDir, "zaparoo.txt")
	err := os.WriteFile(tokenPath, []byte("   \n\n  "), 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	event := MountEvent{
		DeviceID:    "test-device-789",
		MountPath:   tempDir,
		VolumeLabel: "EMPTY_FILE_USB",
		DeviceType:  "USB",
	}

	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should not emit any scan (empty file)
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file is empty")
	case <-ctx.Done():
		// Expected behavior - no event within timeout
	}
}

func TestHandleMountEvent_FileTooBig(t *testing.T) {
	// Create temp directory with oversized file
	tempDir := t.TempDir()

	tokenPath := filepath.Join(tempDir, "zaparoo.txt")
	// Create a file larger than maxFileSize (1MB)
	largeContent := make([]byte, maxFileSize+1)
	err := os.WriteFile(tokenPath, largeContent, 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	event := MountEvent{
		DeviceID:    "test-device-large",
		MountPath:   tempDir,
		VolumeLabel: "LARGE_FILE_USB",
		DeviceType:  "USB",
	}

	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should not emit any scan (file too large)
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file exceeds size limit")
	case <-ctx.Done():
		// Expected behavior - no event within timeout
	}
}

func TestHandleMountEvent_SymlinkRejected(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping symlink test when running as root")
	}

	// Create temp directories
	tempDir := t.TempDir()
	targetDir := t.TempDir()

	// Create actual target file
	targetFile := filepath.Join(targetDir, "target.txt")
	err := os.WriteFile(targetFile, []byte("malicious content"), 0o600)
	require.NoError(t, err)

	// Create symlink in mount directory
	symlinkPath := filepath.Join(tempDir, "zaparoo.txt")
	err = os.Symlink(targetFile, symlinkPath)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	event := MountEvent{
		DeviceID:    "test-device-symlink",
		MountPath:   tempDir,
		VolumeLabel: "SYMLINK_USB",
		DeviceType:  "USB",
	}

	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should not emit any scan (symlink rejected for security)
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan for symlink files")
	case <-ctx.Done():
		// Expected behavior - symlinks are rejected
	}
}

func TestHandleUnmountEvent(t *testing.T) {
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	// Add a fake active token
	reader.mu.Lock()
	reader.activeTokens["test-device-unmount"] = nil
	reader.mu.Unlock()

	// Handle unmount
	reader.handleUnmountEvent("test-device-unmount")

	// Should emit nil token scan
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.Nil(t, scan.Token, "Should emit nil token on unmount")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for unmount scan")
	}

	// Check that token was removed
	reader.mu.RLock()
	_, exists := reader.activeTokens["test-device-unmount"]
	reader.mu.RUnlock()

	assert.False(t, exists, "Token should be removed from active tokens")
}

func TestConnected(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})

	// Not connected initially
	assert.False(t, reader.Connected())

	// Connected after setting detector
	mockDetector := newMockMountDetector()
	reader.detector = mockDetector
	assert.True(t, reader.Connected())
}

func TestInfo(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})

	// Add some active tokens
	reader.activeTokens["device1"] = nil
	reader.activeTokens["device2"] = nil

	info := reader.Info()
	assert.Contains(t, info, "External Drive Reader")
	assert.Contains(t, info, "2 active devices")
}

func TestDevice(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	reader.device = config.ReadersConnect{
		Driver: DriverID,
		Path:   "/mnt/usb",
	}

	path := reader.Path()
	assert.Equal(t, "/mnt/usb", path)
}

func TestRetryConfiguration(t *testing.T) {
	t.Parallel()

	// Verify retry constants are reasonable
	assert.Equal(t, 3, maxReadRetries, "Should retry 3 times for I/O errors")
	assert.Equal(t, 500*time.Millisecond, initialRetryDelay, "Initial delay should be 500ms")

	// Verify total retry time is reasonable (within 5s context timeout)
	// Exponential backoff: 500ms + 1s + 2s = 3.5s total delay (plus read attempts)
	totalMaxDelay := initialRetryDelay + (initialRetryDelay << 1) + (initialRetryDelay << 2)
	assert.Less(t, totalMaxDelay, 4*time.Second, "Total retry delay should be under 4s")
}

func TestHandleMountEvent_CaseInsensitiveTokenFile(t *testing.T) {
	// Test that case-insensitive file matching works
	tempDir := t.TempDir()

	// Create file with uppercase name
	tokenPath := filepath.Join(tempDir, "ZAPAROO.TXT")
	tokenContents := "**launch.system:nes"
	err := os.WriteFile(tokenPath, []byte(tokenContents), 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	event := MountEvent{
		DeviceID:    "test-device-case",
		MountPath:   tempDir,
		VolumeLabel: "CASE_USB",
		DeviceType:  "USB",
	}

	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should detect token with uppercase filename
	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token, "Should detect token with case-insensitive filename")
		assert.Contains(t, scan.Token.Text, "**launch.system:nes")
		assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for scan event - case insensitive matching may be broken")
	}
}

func TestIsBlockDevicePresent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		deviceID string
		expected bool
	}{
		{
			name:     "non-device path returns true (can't validate)",
			deviceID: "UUID-1234-5678",
			expected: true,
		},
		{
			name:     "empty string returns true",
			deviceID: "",
			expected: true,
		},
		{
			name:     "non-existent block device returns false",
			deviceID: "/dev/sdZZZ999",
			expected: false,
		},
		{
			name:     "non-existent nvme device returns false",
			deviceID: "/dev/nvme99n99p1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isBlockDevicePresent(tt.deviceID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsBlockDevicePresent_RealDevice(t *testing.T) {
	// This test checks if real block devices are detected correctly
	// Skip if no /sys/block entries exist (e.g., in containers)
	entries, err := os.ReadDir("/sys/block")
	if err != nil || len(entries) == 0 {
		t.Skip("No /sys/block entries available - likely running in container")
	}

	// Find a real block device to test with
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Test that a real device returns true
		devicePath := "/dev/" + entry.Name()
		result := isBlockDevicePresent(devicePath)
		assert.True(t, result, "Real block device %s should be detected as present", devicePath)

		// Test with partition suffix (e.g., /dev/sda1)
		partitionPath := devicePath + "1"
		result = isBlockDevicePresent(partitionPath)
		assert.True(t, result, "Partition %s on real device should be detected as present", partitionPath)

		// Only test one device
		break
	}
}

func TestMountDetector_Forget(t *testing.T) {
	// Test that Forget() clears device from tracking
	detector, err := NewMountDetector()
	require.NoError(t, err, "NewMountDetector should succeed")
	require.NotNil(t, detector, "Detector should not be nil")

	err = detector.Start()
	require.NoError(t, err, "Detector Start() should succeed")

	// Call Forget with a test device ID - should not panic
	detector.Forget("test-device-to-forget")

	// Clean up
	detector.Stop()
}

func TestHandleMountEvent_StaleMountDetection(t *testing.T) {
	// This test verifies stale mount detection behavior
	// We can't easily simulate a real stale mount, but we can verify the logic
	// with a non-existent device ID

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)
	mockDetector := newMockMountDetector()
	mockCmd := helpers.NewMockCommandExecutor()

	device := config.ReadersConnect{
		Driver: DriverID,
	}

	reader.device = device
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.detector = mockDetector
	reader.cmdExecutor = mockCmd

	// Create a mount event with a device ID that doesn't exist
	// This simulates a stale mount where the block device is gone
	event := MountEvent{
		DeviceID:    "/dev/sdZZZ999", // Stable ID for tracking
		DeviceNode:  "/dev/sdZZZ999", // Device node for safety checks
		MountPath:   "/media/nonexistent",
		VolumeLabel: "STALE_USB",
		DeviceType:  "USB",
	}

	// Handle mount event
	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	// Should have called Forget on the detector
	assert.True(t, mockDetector.wasForgotten("/dev/sdZZZ999"),
		"Detector.Forget should be called for stale mount")

	// Should not emit any scan (device is stale)
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan for stale mount")
	case <-ctx.Done():
		// Expected behavior - no event for stale mount
	}
}

func TestGetBaseDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		deviceID string
		expected string
	}{
		{
			name:     "sda1 -> sda",
			deviceID: "/dev/sda1",
			expected: "/dev/sda",
		},
		{
			name:     "sda -> sda (no partition)",
			deviceID: "/dev/sda",
			expected: "/dev/sda",
		},
		{
			name:     "nvme0n1p1 -> nvme0n1",
			deviceID: "/dev/nvme0n1p1",
			expected: "/dev/nvme0n1",
		},
		{
			name:     "nvme0n1p22 -> nvme0n1",
			deviceID: "/dev/nvme0n1p22",
			expected: "/dev/nvme0n1",
		},
		{
			name:     "non-dev path unchanged",
			deviceID: "UUID-1234-5678",
			expected: "UUID-1234-5678",
		},
		{
			name:     "mmcblk0p1 -> mmcblk0",
			deviceID: "/dev/mmcblk0p1",
			expected: "/dev/mmcblk0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getBaseDevice(tt.deviceID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCanSafelyUnmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		deviceID  string
		mountPath string
		expected  bool
	}{
		// Cases that should NOT be safe to unmount
		{
			name:      "system path /etc should not unmount",
			deviceID:  "/dev/sda1",
			mountPath: "/etc",
			expected:  false,
		},
		{
			name:      "root path should not unmount",
			deviceID:  "/dev/sda1",
			mountPath: "/",
			expected:  false,
		},
		{
			name:      "home path should not unmount",
			deviceID:  "/dev/sda1",
			mountPath: "/home/user",
			expected:  false,
		},
		{
			name:      "var path should not unmount",
			deviceID:  "/dev/sda1",
			mountPath: "/var/log",
			expected:  false,
		},
		{
			name:      "loop device should not unmount",
			deviceID:  "/dev/loop0",
			mountPath: "/media/loop",
			expected:  false,
		},
		{
			name:      "mapper device should not unmount",
			deviceID:  "/dev/mapper/root",
			mountPath: "/media/mapped",
			expected:  false,
		},
		{
			name:      "nvme device should not unmount",
			deviceID:  "/dev/nvme0n1p1",
			mountPath: "/media/nvme",
			expected:  false,
		},
		{
			name:      "UUID-based ID should not unmount",
			deviceID:  "UUID-1234-5678",
			mountPath: "/media/usb",
			expected:  false,
		},
		// Cases where device still exists should NOT unmount
		// (these would need device to be physically gone to pass)
		// We can't easily test the "device gone" case in unit tests
		// as it requires actual hardware state

		// Valid removable media patterns with non-existent devices
		// These SHOULD pass all safety checks and return true (device is gone)
		{
			name:      "sd device in media - non-existent so safe to unmount",
			deviceID:  "/dev/sdZZZ999", // Non-existent device
			mountPath: "/media/usb",
			// All checks pass: valid path, valid device type, device gone
			expected: true,
		},
		{
			name:      "mmcblk device in run/media - non-existent so safe to unmount",
			deviceID:  "/dev/mmcblk999p1",
			mountPath: "/run/media/user/sdcard",
			expected:  true, // Non-existent device passes all checks
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := canSafelyUnmount(tt.deviceID, tt.mountPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCanSafelyUnmount_PathPrefixes(t *testing.T) {
	t.Parallel()

	// Test that only specific path prefixes are allowed
	// Device ID that would pass device checks (non-existent)
	deviceID := "/dev/sdZZZ999"

	allowedPrefixes := []string{"/media/", "/mnt/", "/run/media/"}
	blockedPaths := []string{"/", "/home", "/etc", "/var", "/usr", "/tmp", "/opt", "/boot"}

	for _, prefix := range allowedPrefixes {
		t.Run("allowed_prefix_"+prefix, func(t *testing.T) {
			t.Parallel()
			// Even with allowed prefix, this will fail due to device checks
			// but at least it won't be rejected on path alone
			path := prefix + "testusb"
			// We can't fully test this without mocking os.Stat
			// Just verify the function doesn't panic
			_ = canSafelyUnmount(deviceID, path)
		})
	}

	for _, path := range blockedPaths {
		t.Run("blocked_path_"+path, func(t *testing.T) {
			t.Parallel()
			result := canSafelyUnmount(deviceID, path)
			assert.False(t, result, "Path %s should be blocked", path)
		})
	}
}

func TestCanSafelyUnmount_DeviceTypes(t *testing.T) {
	t.Parallel()

	// Test that only removable device types are allowed
	mountPath := "/media/testusb"

	allowedDevices := []string{"/dev/sda1", "/dev/sdb2", "/dev/mmcblk0p1", "/dev/mmcblk1p2"}
	blockedDevices := []string{"/dev/loop0", "/dev/mapper/root", "/dev/nvme0n1p1", "/dev/dm-0", "UUID-1234"}

	for _, dev := range allowedDevices {
		t.Run("allowed_device_"+dev, func(t *testing.T) {
			t.Parallel()
			// These pass device type check but will fail because device exists/doesn't exist correctly
			// Just verify no panic
			_ = canSafelyUnmount(dev, mountPath)
		})
	}

	for _, dev := range blockedDevices {
		t.Run("blocked_device_"+dev, func(t *testing.T) {
			t.Parallel()
			result := canSafelyUnmount(dev, mountPath)
			assert.False(t, result, "Device %s should be blocked", dev)
		})
	}
}

func TestAttemptStaleUnmount_SafetyCheckFails(t *testing.T) {
	t.Parallel()

	// Test that attemptStaleUnmount doesn't call umount when safety checks fail
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	mockCmd := helpers.NewMockCommandExecutor()
	// Clear default expectations so we can verify no calls are made
	mockCmd.ExpectedCalls = nil

	reader.cmdExecutor = mockCmd

	// Try to unmount a system path - should fail safety check and NOT call umount
	reader.attemptStaleUnmount("/dev/sda1", "/etc/something")

	// Verify umount was NOT called (no expectations means no calls expected)
	mockCmd.AssertNotCalled(t, "Run", mock.Anything, "umount", mock.Anything)
}

func TestAttemptStaleUnmount_ExecutorCalled(t *testing.T) {
	t.Parallel()

	// We can't fully test the "successful unmount" path in unit tests because
	// canSafelyUnmount checks that the device node is actually gone (os.Stat).
	// However, we can verify the function structure is correct by testing
	// with a mock that would succeed if safety checks passed.

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	mockCmd := helpers.NewMockCommandExecutor()
	reader.cmdExecutor = mockCmd

	// This will fail the safety check (device type not /dev/sd* or /dev/mmcblk*)
	// so umount won't be called, but we verify the reader has the mock set up
	reader.attemptStaleUnmount("/dev/nvme0n1p1", "/media/test")

	// Should NOT call umount because /dev/nvme0n1p1 fails the device type check
	mockCmd.AssertNotCalled(t, "Run", mock.Anything, "umount", mock.Anything)
}

func TestAttemptStaleUnmount_WithValidDeviceButPresent(t *testing.T) {
	t.Parallel()

	// Test with a valid-looking device path in a valid media location
	// but the device node check will fail because the path doesn't exist

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	mockCmd := helpers.NewMockCommandExecutor()
	reader.cmdExecutor = mockCmd

	// /dev/sdZZZ999 doesn't exist as a device node (good - passes check 3)
	// /sys/block/sdZZZ doesn't exist (good - passes check 4)
	// /media/test is a valid path (good - passes check 1)
	// /dev/sd* is a valid device type (good - passes check 2)
	// ALL checks should pass, so umount SHOULD be called

	// Set up expectation for the umount call
	mockCmd.ExpectedCalls = nil
	mockCmd.On("Run", mock.Anything, "umount", []string{"-l", "/media/test"}).Return(nil)

	reader.attemptStaleUnmount("/dev/sdZZZ999", "/media/test")

	// Verify umount was called with correct arguments
	mockCmd.AssertCalled(t, "Run", mock.Anything, "umount", []string{"-l", "/media/test"})
}

func TestAttemptStaleUnmount_CommandFailure(t *testing.T) {
	t.Parallel()

	// Test that umount failures are handled gracefully (logged, not panicked)
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	mockCmd := helpers.NewMockCommandExecutor()
	reader.cmdExecutor = mockCmd

	// Set up expectation for the umount call to fail
	mockCmd.ExpectedCalls = nil
	mockCmd.On("Run", mock.Anything, "umount", []string{"-l", "/media/test"}).
		Return(errors.New("umount: /media/test: target is busy"))

	// Should not panic even when umount fails
	reader.attemptStaleUnmount("/dev/sdZZZ999", "/media/test")

	// Verify umount was attempted
	mockCmd.AssertCalled(t, "Run", mock.Anything, "umount", []string{"-l", "/media/test"})
}

func TestFindTokenFile_SiblingPartitionDiscovery(t *testing.T) {
	// Create temp directories to simulate multiple partitions
	// We'll create a primary mount without zaparoo.txt and a sibling with it
	primaryMount := t.TempDir()
	siblingMount := t.TempDir()

	// Create zaparoo.txt only on the sibling mount
	siblingTokenPath := filepath.Join(siblingMount, "zaparoo.txt")
	err := os.WriteFile(siblingTokenPath, []byte("**launch.system:nes"), 0o600)
	require.NoError(t, err)

	// findTokenFile should find the token on the sibling
	// Note: This test is limited because findTokenFile searches /media and /mnt
	// which we can't easily populate in tests. But we can verify the primary
	// mount search works correctly.
	tokenPath, foundMount := findTokenFile(primaryMount)

	// Primary mount has no token, and our temp dirs aren't in /media or /mnt
	// so nothing should be found
	assert.Empty(t, tokenPath)
	assert.Empty(t, foundMount)

	// Now test that token on primary mount IS found
	primaryTokenPath := filepath.Join(primaryMount, "zaparoo.txt")
	err = os.WriteFile(primaryTokenPath, []byte("**launch.system:snes"), 0o600)
	require.NoError(t, err)

	tokenPath, foundMount = findTokenFile(primaryMount)
	assert.Equal(t, primaryTokenPath, tokenPath)
	assert.Equal(t, primaryMount, foundMount)
}

func TestFindTokenFileInDir_DirectoryReadError(t *testing.T) {
	t.Parallel()

	// Test with non-existent directory
	result := findTokenFileInDir("/nonexistent/path/that/does/not/exist")
	assert.Empty(t, result)
}

func TestFindTokenFileInDir_SkipsDirectories(t *testing.T) {
	t.Parallel()

	// Create temp dir with a subdirectory named zaparoo.txt
	tempDir := t.TempDir()

	// Create a directory (not a file) named zaparoo.txt
	dirPath := filepath.Join(tempDir, "zaparoo.txt")
	err := os.Mkdir(dirPath, 0o750)
	require.NoError(t, err)

	// Should not find it (because it's a directory, not a file)
	result := findTokenFileInDir(tempDir)
	assert.Empty(t, result)
}

func TestHandleMountEvent_DuplicateTokenPrevention(t *testing.T) {
	// Test that a second mount from the same device doesn't create duplicate tokens
	tempDir := t.TempDir()

	// Create zaparoo.txt file
	tokenPath := filepath.Join(tempDir, "zaparoo.txt")
	err := os.WriteFile(tokenPath, []byte("**launch.system:nes"), 0o600)
	require.NoError(t, err)

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)
	mockDetector := newMockMountDetector()

	reader.detector = mockDetector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Simulate mount event
	event := MountEvent{
		DeviceID:    "/dev/sda1",
		MountPath:   tempDir,
		VolumeLabel: "TEST_USB",
		DeviceType:  "USB",
	}

	// First mount - should emit token
	reader.wg.Add(1)
	go reader.handleMountEvent(&event)
	reader.wg.Wait()

	ctx, cancel := testContext(testEventTimeout)
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token)
		assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for first scan event")
	}
	cancel()

	// Second mount from same base device - should NOT emit duplicate token
	event2 := MountEvent{
		DeviceID:    "/dev/sda2", // Different partition, same base device
		MountPath:   tempDir,
		VolumeLabel: "TEST_USB",
		DeviceType:  "USB",
	}

	reader.wg.Add(1)
	go reader.handleMountEvent(&event2)
	reader.wg.Wait()

	// Should NOT receive another token (duplicate prevention)
	ctx2, cancel2 := testContext(testNoEventTimeout)
	defer cancel2()
	select {
	case <-scanChan:
		t.Fatal("Should not emit duplicate token for same base device")
	case <-ctx2.Done():
		// Expected - no duplicate token
	}
}

func TestHandleUnmountEvent_BaseDeviceMatching(t *testing.T) {
	// Test that unmount uses base device for matching (e.g., /dev/sda for /dev/sda1)
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Add token using base device (as handleMountEvent would)
	baseDevice := "/dev/sda"
	reader.mu.Lock()
	reader.activeTokens[baseDevice] = nil
	reader.mu.Unlock()

	// Unmount with partition ID should still match via getBaseDevice
	reader.handleUnmountEvent("/dev/sda1")

	// Should emit nil token scan
	ctx, cancel := testContext(testEventTimeout)
	defer cancel()
	select {
	case scan := <-scanChan:
		assert.Nil(t, scan.Token, "Should emit nil token on unmount")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for unmount scan")
	}

	// Token should be removed
	reader.mu.RLock()
	_, exists := reader.activeTokens[baseDevice]
	reader.mu.RUnlock()
	assert.False(t, exists, "Token should be removed from active tokens")
}

func TestHandleUnmountEvent_NoMatchingToken(t *testing.T) {
	t.Parallel()

	// Test that unmount for unknown device doesn't emit anything
	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanChan := make(chan readers.Scan, 10)

	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}

	// Don't add any active tokens

	// Unmount for unknown device
	reader.handleUnmountEvent("/dev/sdz99")

	// Should NOT emit any scan
	ctx, cancel := testContext(testNoEventTimeout)
	defer cancel()
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan for unknown device")
	case <-ctx.Done():
		// Expected - no event
	}
}
