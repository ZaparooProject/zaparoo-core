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
// maxBytes of zero or less means no limit. Otherwise the stderr capture is kept
// whole and the log is trimmed from the front to make room: the log rotates at
// 1 MB, which is already the whole upload budget, so appending anything without
// trimming would push the payload over and lose the upload entirely — taking
// the crash report with it. Trimming the front keeps the most recent entries,
// which are the ones next to the crash.
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
	if len(bytes.TrimSpace(stderrData)) == 0 {
		return trimLogFront(data, maxBytes), nil
	}

	separator := fmt.Sprintf("\n===== %s =====\n", config.StderrFile)
	if maxBytes > 0 {
		// The capture is the reason this function exists, so it gets its space
		// first and the log takes what is left.
		stderrData = trimLogFront(stderrData, maxBytes-len(separator))
		data = trimLogFront(data, maxBytes-len(separator)-len(stderrData))
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

// trimLogFront drops whole lines from the start of data until it fits maxBytes,
// and says how much it dropped. Cutting mid-line would leave a broken JSON
// entry at the top of the log, which readers and any parser trip over.
func trimLogFront(data []byte, maxBytes int) []byte {
	if maxBytes <= 0 || len(data) <= maxBytes {
		return data
	}

	notice := fmt.Sprintf("... %d earlier bytes trimmed to fit the upload limit ...\n",
		len(data)-maxBytes)
	budget := maxBytes - len(notice)
	if budget <= 0 {
		return []byte(notice)
	}

	tail := data[len(data)-budget:]
	if idx := bytes.IndexByte(tail, '\n'); idx >= 0 && idx+1 < len(tail) {
		tail = tail[idx+1:]
	}
	return append([]byte(notice), tail...)
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
