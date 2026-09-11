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

package crashdump

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareRetainsCrashAcrossHealthyStarts(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dir := t.TempDir()
	current := filepath.Join(dir, CurrentFile)
	previous := filepath.Join(dir, PreviousFile)

	f, report, err := prepare(fs, dir, "1.0.0")
	require.NoError(t, err)
	assert.Nil(t, report)
	_, err = f.WriteString("panic: private diagnostic\nstack trace\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	original, err := afero.ReadFile(fs, current)
	require.NoError(t, err)

	f, report, err = prepare(fs, dir, "2.0.0")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NotNil(t, report)
	assert.Equal(t, "1.0.0", report.Version)
	assert.Equal(t, "panic: private diagnostic\nstack trace\n", string(report.Data))
	assert.False(t, report.Time.IsZero())

	for range 3 {
		f, report, err = prepare(fs, dir, "2.0.0")
		require.NoError(t, err)
		require.NoError(t, f.Close())
		assert.Nil(t, report, "retained evidence must not trigger another report")
		retained, readErr := afero.ReadFile(fs, previous)
		require.NoError(t, readErr)
		assert.Equal(t, original, retained, "healthy boots must not replace crash evidence")
	}

	require.NoError(t, afero.WriteFile(fs, current, []byte("zaparoo-core@2.0.0\nfatal error: second crash\n"), 0o600))
	f, report, err = prepare(fs, dir, "3.0.0")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NotNil(t, report)
	assert.Equal(t, "2.0.0", report.Version)
	retained, err := afero.ReadFile(fs, previous)
	require.NoError(t, err)
	assert.Contains(t, string(retained), "second crash")
}

func TestPrepareBoundsReportButPreservesWholeFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dir := t.TempDir()
	original := "zaparoo-core@1.0.0\npanic: crash\n" + strings.Repeat("x", MaxReportBytes*2)
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, CurrentFile), []byte(original), 0o600))
	f, report, err := prepare(fs, dir, "2.0.0")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NotNil(t, report)
	assert.LessOrEqual(t, len(report.Data), MaxReportBytes)
	retained, err := afero.ReadFile(fs, filepath.Join(dir, PreviousFile))
	require.NoError(t, err)
	assert.Equal(t, original, string(retained))
}

type renameFailureFS struct{ *afero.MemMapFs }

func (*renameFailureFS) Rename(string, string) error { return os.ErrPermission }

func TestPrepareDoesNotTruncateWhenRotationFails(t *testing.T) {
	t.Parallel()
	fs := &renameFailureFS{afero.NewMemMapFs().(*afero.MemMapFs)}
	dir := t.TempDir()
	path := filepath.Join(dir, CurrentFile)
	require.NoError(t, fs.MkdirAll(dir, 0o750))
	original := []byte("zaparoo-core@1.0.0\npanic: keep this\n")
	require.NoError(t, afero.WriteFile(fs, path, original, 0o600))
	f, report, err := prepare(fs, dir, "2.0.0")
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Nil(t, f)
	assert.Nil(t, report)
	retained, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, original, retained)
}

func TestPrepareRejectsUnsafeCapture(t *testing.T) {
	t.Parallel()
	_, _, err := prepare(afero.NewMemMapFs(), "", "1.0.0")
	require.ErrorContains(t, err, "no crash directory")
	_, _, err = prepare(afero.NewMemMapFs(), t.TempDir(), "1.0.0\npanic: fake")
	require.ErrorContains(t, err, "invalid crash version")

	dir := t.TempDir()
	target := filepath.Join(dir, "evidence")
	require.NoError(t, os.WriteFile(target, []byte("keep me"), 0o600))
	if symlinkErr := os.Symlink(target, filepath.Join(dir, CurrentFile)); symlinkErr != nil {
		if errors.Is(symlinkErr, os.ErrPermission) {
			t.Skip("symlink creation requires additional privileges")
		}
		require.NoError(t, symlinkErr)
	}
	_, _, err = prepare(afero.NewOsFs(), dir, "1.0.0")
	require.ErrorContains(t, err, "not a regular file")
	//nolint:gosec // Test-owned path inside t.TempDir.
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(data))
}
