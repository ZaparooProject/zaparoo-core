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

package helpers

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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

var logWriter io.Writer

func InitLogging(pl platforms.Platform, writers []io.Writer) error {
	logWriters := []io.Writer{&lumberjack.Logger{
		Filename:   filepath.Join(pl.Settings().LogDir, config.LogFile),
		MaxSize:    1,
		MaxBackups: 2,
	}}

	if len(writers) > 0 {
		logWriters = append(logWriters, writers...)
	}

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	logWriter = io.MultiWriter(logWriters...)
	log.Logger = log.Output(logWriter).With().Caller().Logger()

	return nil
}

// LogWriter returns the underlying io.Writer used by the logger.
// This is useful for adding additional writers (e.g., telemetry) after initialization.
func LogWriter() io.Writer {
	return logWriter
}
