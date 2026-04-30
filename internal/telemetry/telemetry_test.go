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
	"io"
	"sync"
	"testing"

	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	th "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
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
			expected: "C:\\Users\\<user>\\AppData\\Local\\zaparoo\\config.toml",
		},
		{
			name:     "windows path lowercase drive",
			input:    "c:\\Users\\JohnDoe\\Documents\\zaparoo",
			expected: "c:\\Users\\<user>\\Documents\\zaparoo",
		},
		{
			name:     "windows path different drive",
			input:    "D:\\Users\\admin\\zaparoo\\logs",
			expected: "D:\\Users\\<user>\\zaparoo\\logs",
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

func TestInitConfiguresSentryPrivacyOptions(t *testing.T) {
	originalLogger := log.Logger
	logDir := t.TempDir()
	t.Cleanup(func() {
		Close()
		closeErr := corehelpers.CloseLogging()
		log.Logger = originalLogger
		enabled = false
		sentryWriter = nil
		closeOnce = sync.Once{}
		sentry.CurrentHub().BindClient(nil)
		require.NoError(t, closeErr)
	})

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{LogDir: logDir})
	require.NoError(t, corehelpers.InitLogging(mockPlatform, []io.Writer{io.Discard}))

	require.NoError(t, Init(true, "device-id", "1.2.3", "linux"))
	assert.True(t, Enabled())

	client := sentry.CurrentHub().Client()
	require.NotNil(t, client)
	options := client.Options()

	assert.Equal(t, sentryDSN, options.Dsn)
	assert.Equal(t, "zaparoo-core@1.2.3", options.Release)
	assert.Equal(t, "linux", options.Environment)
	assert.True(t, options.AttachStacktrace)
	assert.True(t, options.DisableTelemetryBuffer)
	assert.False(t, options.SendDefaultPII)
	assert.Empty(t, options.ServerName)
	assert.Zero(t, options.MaxBreadcrumbs)
	require.NotNil(t, options.BeforeSend)
	require.NotNil(t, options.HTTPClient)
	assert.IsType(t, &tunnelTransport{}, options.HTTPClient.Transport)
}

func TestSanitizeEvent(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{
		ServerName: "host.local",
		Message:    "failed to open /home/callan/.zaparoo/config.toml",
		Tags: map[string]string{
			"config_path": "C:\\Users\\callan\\AppData\\Local\\zaparoo\\config.toml",
			"platform":    "linux",
		},
		Exception: []sentry.Exception{
			{
				Stacktrace: &sentry.Stacktrace{
					Frames: []sentry.Frame{
						{
							AbsPath:  "/Users/callan/dev/zaparoo-core/internal/telemetry/telemetry.go",
							Filename: "/home/callan/dev/zaparoo-core/internal/telemetry/telemetry.go",
						},
					},
				},
			},
		},
	}

	sanitized := sanitizeEvent(event)

	assert.Empty(t, sanitized.ServerName)
	assert.Equal(t, "failed to open /home/<user>/.zaparoo/config.toml", sanitized.Message)
	assert.Equal(t, "C:\\Users\\<user>\\AppData\\Local\\zaparoo\\config.toml", sanitized.Tags["config_path"])
	assert.Equal(t, "linux", sanitized.Tags["platform"])
	require.Len(t, sanitized.Exception, 1)
	require.NotNil(t, sanitized.Exception[0].Stacktrace)
	require.Len(t, sanitized.Exception[0].Stacktrace.Frames, 1)
	assert.Equal(t, "/Users/<user>/dev/zaparoo-core/internal/telemetry/telemetry.go",
		sanitized.Exception[0].Stacktrace.Frames[0].AbsPath)
	assert.Equal(t, "/home/<user>/dev/zaparoo-core/internal/telemetry/telemetry.go",
		sanitized.Exception[0].Stacktrace.Frames[0].Filename)
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
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.DebugLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.DebugLevel, msg)
	})

	t.Run("Info is dropped when threshold is Error", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.InfoLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.InfoLevel, msg)
	})

	t.Run("Warn is dropped when threshold is Error", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.WarnLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.WarnLevel, msg)
	})

	t.Run("Error passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.ErrorLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.ErrorLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("Fatal passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.FatalLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.FatalLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("Panic passes through when threshold is Error", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("WriteLevel", zerolog.PanicLevel, msg).Return(len(msg), nil).Once()
		n, err := tw.WriteLevel(zerolog.PanicLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})

	t.Run("NoLevel is always dropped", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.NoLevel, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.NoLevel, msg)
	})

	t.Run("Disabled is always dropped", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		n, err := tw.WriteLevel(zerolog.Disabled, msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertNotCalled(t, "WriteLevel", zerolog.Disabled, msg)
	})

	t.Run("Write is always passed through", func(t *testing.T) {
		mockWriter := &th.MockLevelWriter{}
		tw := &thresholdWriter{inner: mockWriter, threshold: zerolog.ErrorLevel}
		mockWriter.On("Write", msg).Return(len(msg), nil).Once()
		n, err := tw.Write(msg)
		assert.Equal(t, len(msg), n)
		require.NoError(t, err)
		mockWriter.AssertExpectations(t)
	})
}
