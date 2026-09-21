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

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
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

// ReadLogBundle includes persistent crash evidence and captured stderr with
// the log. Crash files get budget first, then stderr, then routine logs.
// maxBytes of zero or less means no limit.
func ReadLogBundle(pl platforms.Platform, maxBytes int) ([]byte, error) {
	logPath := filepath.Join(pl.Settings().LogDir, config.LogFile)
	//nolint:gosec // Path is derived from platform settings and a fixed filename.
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	unlimited := maxBytes <= 0
	remaining := maxBytes
	var captures bytes.Buffer
	for _, capture := range []struct {
		dir  string
		name string
		tail bool
	}{
		{DataDir(pl), crashdump.CurrentFile, false},
		{DataDir(pl), crashdump.PreviousFile, false},
		{pl.Settings().LogDir, config.StderrFile, true},
	} {
		if capture.dir == "" {
			continue
		}
		label := fmt.Sprintf("\n===== %s =====\n", capture.name)
		budget := remaining - len(label) - 1 // reserve the log's final newline
		if !unlimited && budget <= 0 {
			continue
		}
		if unlimited {
			budget = 0
		}
		body := readCapture(filepath.Join(capture.dir, capture.name), budget, capture.tail)
		if len(bytes.TrimSpace(body)) == 0 {
			continue
		}
		if !capture.tail && crashCaptureHeaderOnly(body) {
			continue
		}
		_, _ = captures.WriteString(label)
		_, _ = captures.Write(body)
		remaining -= len(label) + len(body)
	}
	if !unlimited {
		if captures.Len() > 0 {
			remaining--
		}
		data = trimLines(data, remaining)
	}
	if captures.Len() == 0 {
		return data, nil
	}
	var buf bytes.Buffer
	_, _ = buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		_ = buf.WriteByte('\n')
	}
	_, _ = buf.Write(captures.Bytes())
	return buf.Bytes(), nil
}

// crashCaptureHeaderOnly reports whether a crash capture holds only the version
// header written when the file is opened, and so records no crash.
//
// A budget too small to reach the header's newline yields a fragment, which is
// what the smallest bundles hand this: the fragment says nothing, but left in it
// spends the budget the previous crash file needs. A fragment shorter than the
// prefix does not even start with it, so both directions have to be checked.
func crashCaptureHeaderOnly(body []byte) bool {
	prefix := []byte(crashdump.VersionPrefix)
	header, rest, ok := bytes.Cut(body, []byte("\n"))
	if !ok {
		return bytes.HasPrefix(body, prefix) || bytes.HasPrefix(prefix, body)
	}
	return bytes.HasPrefix(header, prefix) && len(bytes.TrimSpace(rest)) == 0
}

// readCapture bounds reads as well as output. Preserve the crash header and
// first stack, but keep the newest entries of append-only stderr capture.
func readCapture(path string, maxBytes int, tail bool) []byte {
	//nolint:gosec // Path is a platform directory plus a fixed capture filename.
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	var reader io.Reader = f
	if maxBytes > 0 {
		if tail && info.Size() > int64(maxBytes) {
			if _, seekErr := f.Seek(-int64(maxBytes), io.SeekEnd); seekErr != nil {
				return nil
			}
		}
		reader = io.LimitReader(f, int64(maxBytes))
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil
	}
	return data
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

// CloseLogging closes the active file logger so tests and shutdown paths can
// safely remove the log directory on Windows.
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

// LogPath is where the live log file is written for this platform.
func LogPath(pl platforms.Platform) string {
	return filepath.Join(pl.Settings().LogDir, config.LogFile)
}

// PersistLog copies the log bundle into the platform's persistent data
// directory and returns the path a user should be pointed at.
//
// MiSTer and Mistex write their log to a tmpfs so that routine logging does
// not hit the SD card during gameplay, which means the one file support asks
// for does not survive a reboot. Callers use this at the moments the log is
// about to matter — entering the failed state, and clean shutdown — so the
// evidence outlives the boot that produced it.
//
// Platforms whose log directory is already persistent do nothing and get the
// live path back. Pointing LogDir at TempDir is what makes a log volatile, and
// MiSTer and Mistex are the only platforms that do it; everywhere else LogDir
// is a persistent directory of its own, so copying the bundle would leave a
// second, immediately stale core.log beside the live one on every stop.
func PersistLog(pl platforms.Platform) string {
	settings := pl.Settings()
	livePath := LogPath(pl)

	dataDir := DataDir(pl)
	if dataDir == "" || settings.LogDir == "" || settings.LogDir != settings.TempDir {
		return livePath
	}

	destPath := filepath.Join(dataDir, config.LogFile)
	data, err := ReadLogBundle(pl, 0)
	if err != nil {
		log.Warn().Err(err).Msg("could not read log bundle to persist it")
		return livePath
	}
	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		log.Warn().Err(err).Str("path", destPath).Msg("could not persist log file")
		return livePath
	}

	log.Info().Str("path", destPath).Msg("copied log to persistent storage")
	return destPath
}
