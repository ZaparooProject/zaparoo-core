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

// Package dbtemplate gives tests a freshly migrated database without running
// the migrations for every test.
//
// Every new database runs every migration, and migrations take a lock shared
// by the whole test binary, so parallel tests that each open a database queue
// behind each other. Seed builds the database once per test binary, keeps the
// resulting files in memory, and writes a copy into each test's directory.
// The copy keeps the original modification times, so the schema-version
// sidecar written next to the database still matches and the opener skips
// goose.
package dbtemplate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

type file struct {
	modTime time.Time
	rel     string
	data    []byte
	mode    fs.FileMode
}

type template struct {
	err   error
	files []file
	once  sync.Once
}

var (
	templatesMu syncutil.Mutex
	templates   = map[string]*template{}
)

// Seed writes the template named key into dir, building it first if this
// test binary has not built it yet. build receives an empty directory and
// must leave the finished database files in it, closed, so no write-ahead
// log is still pending. Every file under that directory becomes part of the
// template.
//
// key names one way of building a database. Two callers that build
// differently must use different keys.
func Seed(tb testing.TB, key, dir string, build func(dir string) error) {
	tb.Helper()

	templatesMu.Lock()
	tpl, ok := templates[key]
	if !ok {
		tpl = &template{}
		templates[key] = tpl
	}
	templatesMu.Unlock()

	tpl.once.Do(func() {
		tpl.files, tpl.err = buildTemplate(build)
	})
	if tpl.err != nil {
		tb.Fatalf("building %s database template: %v", key, tpl.err)
	}

	for _, f := range tpl.files {
		if err := writeFile(dir, f); err != nil {
			tb.Fatalf("seeding %s database template: %v", key, err)
		}
	}
}

func buildTemplate(build func(dir string) error) (files []file, err error) {
	dir, err := os.MkdirTemp("", "zaparoo-dbtemplate-*")
	if err != nil {
		return nil, fmt.Errorf("creating template directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil && err == nil {
			err = fmt.Errorf("removing template directory: %w", removeErr)
		}
	}()

	if buildErr := build(dir); buildErr != nil {
		return nil, buildErr
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("reading %s: %w", path, infoErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // path is under our own temp directory
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return fmt.Errorf("relative path of %s: %w", path, relErr)
		}
		files = append(files, file{
			rel:     rel,
			data:    data,
			mode:    info.Mode().Perm(),
			modTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading template files: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("build left no files")
	}
	return files, nil
}

func writeFile(dir string, f file) error {
	path := filepath.Join(dir, f.rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating directory for %s: %w", f.rel, err)
	}
	if err := os.WriteFile(path, f.data, f.mode); err != nil {
		return fmt.Errorf("writing %s: %w", f.rel, err)
	}
	if err := os.Chtimes(path, f.modTime, f.modTime); err != nil {
		return fmt.Errorf("setting times on %s: %w", f.rel, err)
	}
	return nil
}
