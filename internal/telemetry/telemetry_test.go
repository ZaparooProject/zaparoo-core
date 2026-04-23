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

package telemetry

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSanitizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no username in path",
			input:    "/usr/local/bin/zaparoo",
			expected: "/usr/local/bin/zaparoo",
		},
		{
			name:     "linux home path",
			input:    "/home/callan/dev/zaparoo-core/pkg/config/config.go",
			expected: "/home/<user>/dev/zaparoo-core/pkg/config/config.go",
		},
		{
			name:     "linux home path uppercase",
			input:    "/Home/Callan/dev/zaparoo-core/pkg/config/config.go",
			expected: "/home/<user>/dev/zaparoo-core/pkg/config/config.go",
		},
		{
			name:     "macos users path",
			input:    "/Users/callan/Documents/zaparoo/config.toml",
			expected: "/Users/<user>/Documents/zaparoo/config.toml",
		},
		{
			name:     "macos users path lowercase",
			input:    "/users/callan/Documents/zaparoo/config.toml",
			expected: "/Users/<user>/Documents/zaparoo/config.toml",
		},
		{
			name:     "windows path",
			input:    "C:\\Users\\callan\\AppData\\Local\\zaparoo\\config.toml",
			expected: "C:\\<user>\\AppData\\Local\\zaparoo\\config.toml",
		},
		{
			name:     "windows path lowercase drive",
			input:    "c:\\Users\\JohnDoe\\Documents\\zaparoo",
			expected: "c:\\<user>\\Documents\\zaparoo",
		},
		{
			name:     "windows path different drive",
			input:    "D:\\Users\\admin\\zaparoo\\logs",
			expected: "D:\\<user>\\zaparoo\\logs",
		},
		{
			name:     "error message with path",
			input:    "failed to open file: /home/user123/config.toml: no such file",
			expected: "failed to open file: /home/<user>/config.toml: no such file",
		},
		{
			name:     "multiple paths in message",
			input:    "copying /home/alice/src to /home/bob/dst",
			expected: "copying /home/<user>/src to /home/<user>/dst",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnabled(t *testing.T) {
	t.Parallel()

	// enabled starts as false
	assert.False(t, Enabled(), "telemetry should be disabled by default")
}

func TestCloseWhenDisabled(t *testing.T) {
	t.Parallel()

	// Should not panic when called while disabled
	Close()
}

func TestFlushWhenDisabled(t *testing.T) {
	t.Parallel()

	// Should not panic when called while disabled
	Flush()
}

func TestThresholdWriter(t *testing.T) {
	t.Parallel()

	msg := []byte("test message")

	t.Run("Debug is dropped when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.DebugLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.DebugLevel, msg)
	})

	t.Run("Info is dropped when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.InfoLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.InfoLevel, msg)
	})

	t.Run("Warn is dropped when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.WarnLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.WarnLevel, msg)
	})

	t.Run("Error passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.ErrorLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.ErrorLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("Fatal passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.FatalLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.FatalLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("Panic passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.PanicLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.PanicLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("NoLevel is always dropped", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.NoLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.NoLevel, msg)
	})

	t.Run("Disabled is always dropped", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.Disabled, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.Disabled, msg)
	})

	t.Run("Write is always passed through", func(t *testing.T) {
		mockWriter := &mockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("Write", msg).Return(len(msg), nil).Once()
		n, err := tw.Write(msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})
}

type mockLevelWriter struct {
	mock.Mock
}

func (m *mockLevelWriter) Write(p []byte) (int, error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *mockLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	args := m.Called(level, p)
	return args.Int(0), args.Error(1)
}
