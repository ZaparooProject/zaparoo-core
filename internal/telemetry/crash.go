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
	"bufio"
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
	"github.com/getsentry/sentry-go"
)

var (
	crashVersionRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`)
	crashFunctionRe = regexp.MustCompile(
		`^((?:github\.com/ZaparooProject/zaparoo-core/v2/[A-Za-z0-9_./]+|main|runtime)\.[A-Za-z0-9_.*()]+)\(`,
	)
)

// ReportCrash makes one best-effort report when a crash is rotated. Retention
// does not depend on telemetry consent or delivery; the raw file stays local.
func ReportCrash(report *crashdump.Report) {
	if !Enabled() || report == nil {
		return
	}
	if event := crashEvent(report); event != nil {
		sentry.CaptureEvent(event)
	}
}

func crashEvent(report *crashdump.Report) *sentry.Event {
	data := report.Data
	if len(data) > crashdump.MaxReportBytes {
		data = data[:crashdump.MaxReportBytes]
	}
	kind := crashKind(data)
	if kind == "" {
		return nil
	}

	// Deliberately exclude panic text, paths, arguments and register values.
	// Only code symbols from Core, main and the runtime are eligible. Restrict
	// collection to the first goroutine; other workers are not the crash site.
	frames := make([]sentry.Frame, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	inStack := false
	for scanner.Scan() {
		line := scanner.Text()
		if !inStack {
			inStack = strings.HasPrefix(line, "goroutine ") && strings.HasSuffix(line, "]:")
			continue
		}
		if line == "" || strings.HasPrefix(line, "created by ") || len(frames) == 32 {
			break
		}
		if match := crashFunctionRe.FindStringSubmatch(line); match != nil {
			frames = append(frames, sentry.Frame{
				Function: match[1],
				InApp: strings.HasPrefix(match[1], "github.com/ZaparooProject/zaparoo-core/v2/") ||
					strings.HasPrefix(match[1], "main."),
			})
		}
	}
	slices.Reverse(frames) // Sentry orders frames from caller to crash site.
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Timestamp = report.Time
	event.Message = "Previous service run crashed: " + kind
	event.Tags["crash_capture"] = "runtime"
	// An unknown historical release must not inherit the current SDK release.
	event.Release = "zaparoo-core@unknown"
	if crashVersionRe.MatchString(report.Version) {
		event.Release = "zaparoo-core@" + report.Version
	}
	event.Exception = []sentry.Exception{{
		Type: kind, Value: "Crash details retained locally",
		Stacktrace: &sentry.Stacktrace{Frames: frames},
	}}
	return event
}

func crashKind(data []byte) string {
	// Runtime diagnostics can precede "fatal error:" (for example stack overflow).
	for line := range bytes.Lines(data) {
		switch {
		case bytes.HasPrefix(line, []byte("goroutine ")):
			return ""
		case bytes.HasPrefix(line, []byte("panic: ")):
			return "Unhandled Go panic"
		case bytes.HasPrefix(line, []byte("fatal error: ")):
			return "Go runtime fatal error"
		default:
			for _, signal := range []string{"SIGABRT", "SIGSEGV", "SIGBUS", "SIGILL", "SIGFPE", "SIGQUIT"} {
				if bytes.HasPrefix(line, []byte(signal+":")) {
					return signal
				}
			}
		}
	}
	return ""
}
