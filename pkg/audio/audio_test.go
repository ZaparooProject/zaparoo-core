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

package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadCloser is a mock implementation of io.ReadCloser for testing
type mockReadCloser struct {
	reader    io.Reader
	closeErr  error
	readErr   error
	readCount int
	closed    bool
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	m.readCount++
	if m.readErr != nil {
		return 0, m.readErr
	}
	n, err = m.reader.Read(p)
	//nolint:wrapcheck // Mock pass-through in test code doesn't need error wrapping
	return n, err
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return m.closeErr
}

// validWAVHeader returns a minimal valid WAV file header
func validWAVHeader() []byte {
	// Minimal WAV file: RIFF header + fmt chunk + data chunk (44 bytes header + some silence)
	wav := []byte{
		// RIFF header
		'R', 'I', 'F', 'F',
		36, 0, 0, 0, // File size - 8
		'W', 'A', 'V', 'E',
		// fmt chunk
		'f', 'm', 't', ' ',
		16, 0, 0, 0, // fmt chunk size
		1, 0, // Audio format (PCM)
		1, 0, // Number of channels (mono)
		0x44, 0xAC, 0, 0, // Sample rate (44100)
		0x88, 0x58, 0x01, 0, // Byte rate
		2, 0, // Block align
		16, 0, // Bits per sample
		// data chunk
		'd', 'a', 't', 'a',
		0, 0, 0, 0, // Data size (empty)
	}
	return wav
}

func TestPlayWAV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock func() io.ReadCloser
		checkMock func(t *testing.T, mock *mockReadCloser)
		name      string
		wantErr   bool
	}{
		{
			name: "valid WAV data",
			setupMock: func() io.ReadCloser {
				return &mockReadCloser{
					reader: bytes.NewReader(validWAVHeader()),
				}
			},
			wantErr: false,
			// Note: We don't verify async callback cleanup here because:
			// 1. It would require sleep-based timing (flaky, slow)
			// 2. Callback execution is handled by beep library
			// 3. We trust beep's Seq() to call our cleanup callback
			checkMock: nil,
		},
		{
			name: "invalid WAV data",
			setupMock: func() io.ReadCloser {
				return &mockReadCloser{
					reader: bytes.NewReader([]byte("not a wav file")),
				}
			},
			wantErr: true,
			checkMock: func(t *testing.T, mock *mockReadCloser) {
				// Reader should be closed on error
				assert.True(t, mock.closed, "reader should be closed on error")
			},
		},
		{
			name: "empty data",
			setupMock: func() io.ReadCloser {
				return &mockReadCloser{
					reader: bytes.NewReader([]byte{}),
				}
			},
			wantErr: true,
			checkMock: func(t *testing.T, mock *mockReadCloser) {
				assert.True(t, mock.closed, "reader should be closed on error")
			},
		},
		{
			name: "close error is handled gracefully",
			setupMock: func() io.ReadCloser {
				return &mockReadCloser{
					reader:   bytes.NewReader([]byte("invalid")),
					closeErr: errors.New("close error"),
				}
			},
			wantErr: true,
			checkMock: func(t *testing.T, mock *mockReadCloser) {
				assert.True(t, mock.closed, "close should be attempted")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//nolint:forcetypeassert,revive // Test code with known mock type
			mock := tt.setupMock().(*mockReadCloser)
			err := PlayWAV(mock)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.checkMock != nil {
				tt.checkMock(t, mock)
			}
		})
	}
}

func TestPlayWAVBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "valid WAV bytes",
			data:    validWAVHeader(),
			wantErr: false,
		},
		{
			name:    "invalid WAV bytes",
			data:    []byte("not a wav file"),
			wantErr: true,
		},
		{
			name:    "empty bytes",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "nil bytes",
			data:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PlayWAVBytes(tt.data)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPlayWAVFile(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a valid WAV file
	validWAVPath := filepath.Join(tmpDir, "valid.wav")
	err := os.WriteFile(validWAVPath, validWAVHeader(), 0o600)
	require.NoError(t, err)

	// Create an invalid WAV file
	invalidWAVPath := filepath.Join(tmpDir, "invalid.wav")
	err = os.WriteFile(invalidWAVPath, []byte("not a wav file"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid WAV file",
			path:    validWAVPath,
			wantErr: false,
		},
		{
			name:    "invalid WAV file",
			path:    invalidWAVPath,
			wantErr: true,
		},
		{
			name:    "non-existent file",
			path:    filepath.Join(tmpDir, "nonexistent.wav"),
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PlayWAVFile(tt.path)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Note: We don't have a TestPlayWAVFile_ResourceCleanup test because:
// 1. The cleanup callback runs asynchronously after audio finishes
// 2. Testing it would require sleep-based timing which is flaky and slow
// 3. The callback mechanism is provided by beep.Seq() which we trust
// 4. We test the error path cleanup in TestPlayWAV (immediate Close on error)
