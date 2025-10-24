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

package externaldrive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMountDetector implements MountDetector for testing
type mockMountDetector struct {
	eventsChan   chan MountEvent
	unmountsChan chan string
	started      bool
	stopped      bool
}

func newMockMountDetector() *mockMountDetector {
	return &mockMountDetector{
		eventsChan:   make(chan MountEvent, 10),
		unmountsChan: make(chan string, 10),
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

	assert.Equal(t, "external_drive", metadata.ID)
	assert.Equal(t, "External drive reader (USB sticks, SD cards, external HDDs)", metadata.Description)
	assert.False(t, metadata.DefaultEnabled, "Should be opt-in only")
	assert.True(t, metadata.DefaultAutoDetect, "Should automatically detect mounted devices")
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	require.Len(t, ids, 1)
	assert.Equal(t, DriverID, ids[0])
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "externaldrive reader has no special capabilities")
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

	// Should return DriverID if platform supports mount detection
	// (will return empty string on platforms without detector implementation)
	if result != "" {
		assert.Equal(t, DriverID, result)
	}
}

func TestDetect_PlatformSupport(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{})

	// On supported platforms (Linux, macOS, Windows), Detect() should succeed
	// and return the driver ID, verifying that the platform-specific detector
	// can be instantiated successfully.
	assert.NotEmpty(t, result, "Detect() should return DriverID on supported platforms")
	assert.Equal(t, DriverID, result, "Detect() should return the correct driver ID")
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
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token, "Should receive token from mount event")
		assert.Equal(t, TokenType, scan.Token.Type)
		assert.Contains(t, scan.Token.Text, tokenContents)
	case <-time.After(2 * time.Second):
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
	select {
	case scan := <-scanChan:
		assert.Nil(t, scan.Token, "Should receive nil token on unmount")
	case <-time.After(2 * time.Second):
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

	select {
	case <-done:
		// Success - processEvents exited cleanly
	case <-time.After(1 * time.Second):
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
	go reader.handleMountEvent(event)
	reader.wg.Wait()

	// Check that scan was emitted
	select {
	case scan := <-scanChan:
		assert.NotNil(t, scan.Token)
		assert.Equal(t, TokenType, scan.Token.Type)
		assert.Contains(t, scan.Token.Text, "**launch.system:nes")
	case <-time.After(2 * time.Second):
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
	go reader.handleMountEvent(event)
	reader.wg.Wait()

	// Should not emit any scan (no file found)
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file is missing")
	case <-time.After(200 * time.Millisecond):
		// Expected behavior
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
	go reader.handleMountEvent(event)
	reader.wg.Wait()

	// Should not emit any scan (empty file)
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file is empty")
	case <-time.After(200 * time.Millisecond):
		// Expected behavior
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
	go reader.handleMountEvent(event)
	reader.wg.Wait()

	// Should not emit any scan (file too large)
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan when file exceeds size limit")
	case <-time.After(200 * time.Millisecond):
		// Expected behavior
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
	go reader.handleMountEvent(event)
	reader.wg.Wait()

	// Should not emit any scan (symlink rejected for security)
	select {
	case <-scanChan:
		t.Fatal("Should not emit scan for symlink files")
	case <-time.After(200 * time.Millisecond):
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
	select {
	case scan := <-scanChan:
		assert.Nil(t, scan.Token, "Should emit nil token on unmount")
	case <-time.After(200 * time.Millisecond):
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
	}

	device := reader.Device()
	assert.Contains(t, device, DriverID)
}
