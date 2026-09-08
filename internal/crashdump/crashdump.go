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

// Package crashdump preserves runtime crash output independently of routine logs.
package crashdump

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/afero"
)

const (
	CurrentFile    = "core.crash.log"
	PreviousFile   = "core.crash.previous.log"
	MaxReportBytes = 64 * 1024
	versionPrefix  = "zaparoo-core@"
)

// Report is a bounded view of the retained file, not a replacement for it.
type Report struct {
	Time    time.Time
	Version string
	Data    []byte
}

// Start rotates a previous crash before opening fresh runtime-only capture.
// Call only from the service process, before starting its workers. The runtime
// retains its own descriptor until process exit, including during shutdown.
// Native code that exits without Go's fatal handler cannot be captured here.
func Start(dir, version string) (*Report, error) {
	f, report, err := prepare(afero.NewOsFs(), dir, version)
	if err != nil {
		return report, err
	}
	defer func() { _ = f.Close() }()

	file, ok := f.(*os.File)
	if !ok {
		return report, errors.New("crash capture has no file descriptor")
	}
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		return report, fmt.Errorf("registering crash output: %w", err)
	}
	return report, nil
}

func prepare(fs afero.Fs, dir, version string) (afero.File, *Report, error) {
	if dir == "" {
		return nil, nil, errors.New("no crash directory configured")
	}
	if strings.ContainsAny(version, "\r\n") {
		return nil, nil, errors.New("invalid crash version")
	}
	if err := fs.MkdirAll(dir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("creating crash directory: %w", err)
	}
	path := filepath.Join(dir, CurrentFile)
	report, err := readPrevious(fs, path)
	if err != nil {
		return nil, nil, err
	}
	if report != nil {
		if renameErr := fs.Rename(path, filepath.Join(dir, PreviousFile)); renameErr != nil {
			// Do not truncate evidence when rotation fails.
			return nil, nil, fmt.Errorf("retaining previous crash: %w", renameErr)
		}
	} else if removeErr := fs.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("removing empty crash capture: %w", removeErr)
	}

	// Exclusive creation cannot follow a substituted symlink. Synchronous
	// writes keep crash output from waiting in the page cache until reboot.
	f, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_SYNC, 0o600)
	if err != nil {
		return nil, report, fmt.Errorf("opening crash capture: %w", err)
	}
	// Version is persisted before a crash: next boot may run a different binary.
	if _, writeErr := f.WriteString(versionPrefix + version + "\n"); writeErr != nil {
		_ = f.Close()
		return nil, report, fmt.Errorf("writing crash version: %w", writeErr)
	}
	return f, report, nil
}

//nolint:nilnil // Missing or header-only capture means no previous crash, not an error.
func readPrevious(fs afero.Fs, path string) (*Report, error) {
	lstater, ok := fs.(afero.Lstater)
	if !ok {
		return nil, errors.New("crash filesystem does not support lstat")
	}
	info, _, err := lstater.LstatIfPossible(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checking previous crash: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("crash capture is not a regular file")
	}
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading previous crash: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxReportBytes))
	if err != nil {
		return nil, fmt.Errorf("reading previous crash: %w", err)
	}
	version := ""
	if header, rest, ok := bytes.Cut(data, []byte("\n")); ok && bytes.HasPrefix(header, []byte(versionPrefix)) {
		version = strings.TrimPrefix(string(header), versionPrefix)
		data = rest
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	return &Report{Time: info.ModTime(), Version: version, Data: data}, nil
}
