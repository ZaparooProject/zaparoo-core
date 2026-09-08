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
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrashEventPrivacy(t *testing.T) {
	t.Parallel()
	report := &crashdump.Report{
		Version: "2.4.0",
		Time:    time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
		Data: []byte("panic: secret-token /home/private/roms/game.zip\n\n" +
			"goroutine 12 [running]:\n" +
			"github.com/ZaparooProject/zaparoo-core/v2/pkg/service.(*Service).Run(0xdeadbeef)\n" +
			"\t/home/private/source/pkg/service/service.go:42 +0x123\n" +
			"main.main()\n\tC:\\Users\\private\\main.go:9\n\n" +
			"goroutine 13 [running]:\nruntime.otherWorker(0xbad)\n"),
	}
	event := crashEvent(report)
	require.NotNil(t, event)
	assert.Equal(t, "zaparoo-core@2.4.0", event.Release)
	assert.Equal(t, report.Time, event.Timestamp)
	require.Len(t, event.Exception, 1)
	frames := event.Exception[0].Stacktrace.Frames
	require.Len(t, frames, 2)
	assert.Equal(t, "main.main", frames[0].Function)
	assert.Equal(t, "github.com/ZaparooProject/zaparoo-core/v2/pkg/service.(*Service).Run", frames[1].Function)
	serialized, err := json.Marshal(event)
	require.NoError(t, err)
	for _, private := range []string{
		"secret-token", "private", "roms", "game.zip", "deadbeef", "0x123", "otherWorker",
	} {
		assert.NotContains(t, string(serialized), private)
	}
}

func TestCrashEventKinds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ input, kind string }{
		{"panic: boom\n", "Unhandled Go panic"},
		{"fatal error: concurrent map writes\n", "Go runtime fatal error"},
		{"runtime: goroutine stack exceeds limit\nfatal error: stack overflow\n", "Go runtime fatal error"},
		{"SIGABRT: abort\n", "SIGABRT"},
		{"SIGSEGV: segmentation violation\n", "SIGSEGV"},
		{"SIGBUS: bus error\n", "SIGBUS"},
		{"http: response.WriteHeader on hijacked connection\n", ""},
		{"Panic: already recovered and logged\n", ""},
		{"=== service start ===\n", ""},
		{"", ""},
	} {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			event := crashEvent(&crashdump.Report{Data: []byte(tt.input), Version: "/home/private/secret"})
			if tt.kind == "" {
				assert.Nil(t, event)
				return
			}
			require.NotNil(t, event)
			assert.Equal(t, tt.kind, event.Exception[0].Type)
			assert.Equal(t, "zaparoo-core@unknown", event.Release)
		})
	}
}

func TestReportCrashHonorsConsent(t *testing.T) {
	originalEnabled := enabled
	hub := sentry.CurrentHub()
	originalClient := hub.Client()
	var events []*sentry.Event
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://key@example.invalid/1",
		Release:          "zaparoo-core@new-version",
		AttachStacktrace: true,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			events = append(events, sanitizeEvent(event))
			return nil // no test event leaves the process
		},
	})
	require.NoError(t, err)
	hub.BindClient(client)
	t.Cleanup(func() {
		enabled = originalEnabled
		hub.BindClient(originalClient)
		client.Transport.Close()
	})
	report := &crashdump.Report{Version: "1.0.0", Data: []byte("panic: secret\n")}
	enabled = false
	ReportCrash(report)
	assert.Empty(t, events)
	enabled = true
	ReportCrash(nil)
	ReportCrash(report)
	require.Len(t, events, 1)
	assert.Equal(t, "zaparoo-core@1.0.0", events[0].Release)
	assert.NotContains(t, events[0].Message, "secret")
}

//nolint:gosec // Only builds a repository fixture and runs it against test-owned temporary files.
func TestCrashCaptureSubprocess(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix native-abort probe; Windows runtime execution is left to native CI")
	}
	binary := filepath.Join(t.TempDir(), "crash-probe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./testdata/crash")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	for _, tt := range []struct{ mode, signature string }{
		{"panic", "panic: probe panic"},
		{"abort", "SIGABRT: abort"},
		{"native-exit", ""},
		{"clean", ""},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd := exec.CommandContext(t.Context(), binary, dir, tt.mode)
			cmd.Env = append(os.Environ(), "GOTRACEBACK=all")
			output, err := cmd.CombinedOutput()
			if tt.mode == "clean" {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err)
			}
			require.Contains(t, string(output), "ordinary stderr chatter")
			path := filepath.Join(dir, crashdump.CurrentFile)
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(original), "ordinary stderr chatter")
			if tt.signature == "" {
				assert.Equal(t, "zaparoo-core@1.2.3\n", string(original))
			} else {
				assert.Contains(t, string(original), tt.signature)
			}
			cmd = exec.CommandContext(t.Context(), binary, dir, "clean")
			output, err = cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			if tt.signature != "" {
				retained, readErr := os.ReadFile(filepath.Join(dir, crashdump.PreviousFile))
				require.NoError(t, readErr)
				assert.Equal(t, original, retained)
			}
		})
	}
}

func FuzzCrashEvent(f *testing.F) {
	f.Add("panic: secret\n\ngoroutine 1 [running]:\nmain.main()\n\t/private/main.go:1\n", "1.0.0")
	f.Add("SIGABRT: abort\n", "bad/version")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, data, version string) {
		event := crashEvent(&crashdump.Report{Data: []byte(data), Version: version})
		if event == nil {
			return
		}
		assert.LessOrEqual(t, len(event.Release), len("zaparoo-core@")+128)
		for i := range event.Exception[0].Stacktrace.Frames {
			frame := &event.Exception[0].Stacktrace.Frames[i]
			assert.Empty(t, frame.AbsPath)
			assert.Empty(t, frame.Filename)
			assert.NotContains(t, frame.Function, "\\")
			assert.False(t, strings.ContainsAny(frame.Function, "\n\r\t"))
		}
	})
}
