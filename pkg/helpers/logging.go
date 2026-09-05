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
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// EnsureDirectories creates the necessary directories for the application.
// This should be called early during startup, before InitLogging.
func EnsureDirectories(pl platforms.Platform) error {
	// Create temp directory for PID files and other temporary files
	err := os.MkdirAll(pl.Settings().TempDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create log directory for persistent log files
	err = os.MkdirAll(pl.Settings().LogDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	return nil
}

var (
	logMu         syncutil.RWMutex
	logFileWriter *lumberjack.Logger
	logWriter     io.Writer
)

func InitLogging(pl platforms.Platform, writers []io.Writer) error {
	if err := CloseLogging(); err != nil {
		return fmt.Errorf("failed to close previous log file: %w", err)
	}

	logMu.Lock()
	defer logMu.Unlock()

	logFileWriter = &lumberjack.Logger{
		Filename:   filepath.Join(pl.Settings().LogDir, config.LogFile),
		MaxSize:    1,
		MaxBackups: 2,
	}
	logWriters := []io.Writer{logFileWriter}

	if len(writers) > 0 {
		logWriters = append(logWriters, writers...)
	}

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	logWriter = io.MultiWriter(logWriters...)
	log.Logger = log.Output(logWriter)

	return nil
}

// LogWriter returns the underlying io.Writer used by the logger.
// This is useful for adding additional writers (e.g., telemetry) after initialization.
func LogWriter() io.Writer {
	logMu.RLock()
	defer logMu.RUnlock()

	return logWriter
}

// CloseLogging closes the active file logger so tests and shutdown paths can
// safely remove the log directory on Windows.
// ReadLogBundle returns the log file's contents with the service's captured
// stderr appended when it holds anything, trimmed to fit maxBytes.
//
// Panics and Go runtime fatal errors never reach zerolog, so they exist only in
// the stderr file. Every path that hands a log to a user ships a single file,
// so a crash has to travel inside that file or it does not travel at all.
//
// maxBytes of zero or less means no limit. Otherwise the capture is budgeted
// first and the log takes what is left: the log rotates at 1 MB, which is
// already the whole upload budget, so budgeting the other way round loses the
// crash to a full log in exactly the case worth reporting.
func ReadLogBundle(pl platforms.Platform, maxBytes int) ([]byte, error) {
	logPath := filepath.Join(pl.Settings().LogDir, config.LogFile)
	//nolint:gosec // Path is derived from platform settings and a fixed filename.
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	stderrPath := filepath.Join(pl.Settings().LogDir, config.StderrFile)
	//nolint:gosec // Path is derived from platform settings and a fixed filename.
	stderrData, stderrErr := os.ReadFile(stderrPath)
	if stderrErr != nil {
		// A missing or unreadable stderr file is the normal case: it only
		// exists once the service has been started by the daemon, and it is
		// empty unless something crashed. The log itself is still worth
		// returning, so this is not an error for the caller.
		stderrData = nil //nolint:nilerr // the log is usable without the stderr capture
	}

	unlimited := maxBytes <= 0
	if len(bytes.TrimSpace(stderrData)) == 0 {
		if unlimited {
			return data, nil
		}
		return trimLines(data, maxBytes), nil
	}

	separator := fmt.Sprintf("\n===== %s =====\n", config.StderrFile)
	if !unlimited {
		// One byte held back for the newline the log may need before the
		// separator, so the assembled bundle cannot overshoot by one.
		captureBudget := maxBytes - len(separator) - 1
		if captureBudget <= 0 {
			// No room for the capture and its label; the log alone is all that
			// can be delivered within the limit.
			return trimLines(data, maxBytes), nil
		}
		// Trimmed by bytes rather than lines: this is free text, not JSON, and
		// half a panic is worth more than none. A capture that is one long line
		// would otherwise be discarded whole.
		stderrData = trimBytes(stderrData, captureBudget)
		data = trimLines(data, maxBytes-len(separator)-len(stderrData)-1)
	}

	var buf bytes.Buffer
	_, _ = buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		_ = buf.WriteByte('\n')
	}
	_, _ = buf.WriteString(separator)
	_, _ = buf.Write(stderrData)
	return buf.Bytes(), nil
}

// trimBytes keeps the last maxBytes bytes of data. A non-positive budget keeps
// nothing: callers use it for a computed remainder, where zero means no room
// rather than no limit.
func trimBytes(data []byte, maxBytes int) []byte {
	if maxBytes <= 0 {
		return nil
	}
	if len(data) <= maxBytes {
		return data
	}
	return data[len(data)-maxBytes:]
}

// trimLines drops whole lines from the start of data until it fits maxBytes,
// and says how much it dropped. A non-positive budget keeps nothing.
//
// Cutting mid-line would leave a broken JSON entry that readers and parsers
// trip over, so a budget that lands inside a single line keeps none of it.
func trimLines(data []byte, maxBytes int) []byte {
	if maxBytes <= 0 {
		return nil
	}
	if len(data) <= maxBytes {
		return data
	}

	notice := fmt.Sprintf("... %d earlier bytes trimmed to fit the upload limit ...\n",
		len(data)-maxBytes)
	if len(notice) > maxBytes {
		// Too small to even say what happened, let alone carry content.
		return nil
	}

	tail := data[len(data)-(maxBytes-len(notice)):]
	idx := bytes.IndexByte(tail, '\n')
	if idx < 0 || idx+1 >= len(tail) {
		// The budget lands inside one line; keeping any of it emits a fragment.
		return []byte(notice)
	}
	return append([]byte(notice), tail[idx+1:]...)
}

func CloseLogging() error {
	logMu.Lock()
	defer logMu.Unlock()

	if logFileWriter == nil {
		return nil
	}

	err := logFileWriter.Close()
	logFileWriter = nil
	logWriter = nil
	if err != nil {
		return fmt.Errorf("failed to close log file writer: %w", err)
	}
	return nil
}
