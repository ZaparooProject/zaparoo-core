//go:build linux && !android

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

package mister

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type fakeRootMount struct {
	flagsErr  error
	remountFn func(flags uintptr) error
	remounts  []uintptr
	flags     int64
}

func (m *fakeRootMount) Flags(string) (int64, error) {
	return m.flags, m.flagsErr
}

func (m *fakeRootMount) Remount(_ string, flags uintptr) error {
	m.remounts = append(m.remounts, flags)
	if m.remountFn != nil {
		return m.remountFn(flags)
	}
	return nil
}

// failCreateFs fails every file creation, as a full root filesystem does.
type failCreateFs struct{ afero.Fs }

func (f failCreateFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_CREATE != 0 {
		return nil, errors.New("no space left on device")
	}
	return f.Fs.OpenFile(name, flag, perm) //nolint:wrapcheck // test passthrough
}

// failOpFs fails one filesystem operation, for the write steps after the
// temporary file exists.
type failOpFs struct {
	afero.Fs
	op string
}

func (f failOpFs) Stat(name string) (os.FileInfo, error) {
	if f.op == "stat" && name == initRCSPath {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(name) //nolint:wrapcheck // test passthrough
}

func (f failOpFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.op == "read" && name == initScriptPath {
		return nil, os.ErrPermission
	}
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err //nolint:wrapcheck // test passthrough
	}
	if flag&os.O_CREATE != 0 {
		return failOpFile{File: file, op: f.op}, nil
	}
	return file, nil
}

func (f failOpFs) Open(name string) (afero.File, error) {
	return f.OpenFile(name, os.O_RDONLY, 0)
}

// failOpFile fails one step of writing the temporary init script.
type failOpFile struct {
	afero.File
	op string
}

func (f failOpFile) WriteString(s string) (int, error) {
	if f.op == "write" {
		return 0, errors.New("no space left on device")
	}
	return f.File.WriteString(s) //nolint:wrapcheck // test passthrough
}

func (f failOpFile) Sync() error {
	if f.op == "sync" {
		return errors.New("input/output error")
	}
	return f.File.Sync() //nolint:wrapcheck // test passthrough
}

func (f failOpFile) Close() error {
	err := f.File.Close()
	if f.op == "close" {
		return errors.New("input/output error")
	}
	return err //nolint:wrapcheck // test passthrough
}

func (f failOpFs) Chmod(name string, mode os.FileMode) error {
	if f.op == "chmod" {
		return errors.New("operation not permitted")
	}
	return f.Fs.Chmod(name, mode) //nolint:wrapcheck // test passthrough
}

func (f failOpFs) Rename(oldname, newname string) error {
	if f.op == "rename" {
		return errors.New("input/output error")
	}
	return f.Fs.Rename(oldname, newname) //nolint:wrapcheck // test passthrough
}

const testZaparooPath = "/media/fat/Scripts/zaparoo.sh"

func initFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, initRCSPath, []byte("#!/bin/sh\n"), 0o755))
	return fs
}

func TestEnsureInitScript_WritesMissingScript(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	mount := &fakeRootMount{}
	content := initScriptContent(testZaparooPath)

	written, err := ensureInitScript(fs, mount, content)
	require.NoError(t, err)
	assert.True(t, written)

	got, err := afero.ReadFile(fs, initScriptPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
	info, err := fs.Stat(initScriptPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	assert.Empty(t, mount.remounts, "a writable root must not be remounted")

	entries, err := afero.ReadDir(fs, "/etc/init.d")
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no temporary file may be left behind")
}

func TestEnsureInitScript_UpToDateScriptIsLeftAlone(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	content := initScriptContent(testZaparooPath)
	require.NoError(t, afero.WriteFile(fs, initScriptPath, []byte(content), 0o755))
	mount := &fakeRootMount{flags: unix.ST_RDONLY}

	written, err := ensureInitScript(fs, mount, content)
	require.NoError(t, err)
	assert.False(t, written)
	assert.Empty(t, mount.remounts, "an up to date script must not remount the root filesystem")
}

func TestEnsureInitScript_RewritesStaleOrNonExecutableScript(t *testing.T) {
	t.Parallel()
	content := initScriptContent(testZaparooPath)
	for name, tc := range map[string]struct {
		body string
		mode os.FileMode
	}{
		"documented synchronous script": {
			body: "#!/bin/sh\n/media/fat/Scripts/zaparoo.sh -service start\n",
			mode: 0o755,
		},
		"same content, not executable": {body: content, mode: 0o644},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs := initFs(t)
			require.NoError(t, afero.WriteFile(fs, initScriptPath, []byte(tc.body), tc.mode))

			written, err := ensureInitScript(fs, &fakeRootMount{}, content)
			require.NoError(t, err)
			assert.True(t, written)
			got, err := afero.ReadFile(fs, initScriptPath)
			require.NoError(t, err)
			assert.Equal(t, content, string(got))
			info, err := fs.Stat(initScriptPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
		})
	}
}

func TestEnsureInitScript_NoRCSDoesNothing(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	mount := &fakeRootMount{flags: unix.ST_RDONLY}

	written, err := ensureInitScript(fs, mount, initScriptContent(testZaparooPath))
	require.NoError(t, err)
	assert.False(t, written)
	assert.Empty(t, mount.remounts)
	_, statErr := fs.Stat(initScriptPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestEnsureInitScript_ReadOnlyRootIsRemountedAndRestored(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	const stFlags = unix.ST_RDONLY | unix.ST_NOATIME | unix.ST_NODIRATIME
	mount := &fakeRootMount{flags: stFlags}
	content := initScriptContent(testZaparooPath)

	written, err := ensureInitScript(fs, mount, content)
	require.NoError(t, err)
	assert.True(t, written)

	kept := uintptr(unix.MS_REMOUNT | unix.MS_NOATIME | unix.MS_NODIRATIME)
	assert.Equal(t, []uintptr{kept, kept | unix.MS_RDONLY}, mount.remounts,
		"the root must go read-write keeping its flags, then back to read-only")
}

func TestEnsureInitScript_WriteFailureRestoresReadOnly(t *testing.T) {
	t.Parallel()
	fs := failCreateFs{Fs: initFs(t)}
	mount := &fakeRootMount{flags: unix.ST_RDONLY}

	written, err := ensureInitScript(fs, mount, initScriptContent(testZaparooPath))
	require.Error(t, err)
	assert.False(t, written)
	require.Len(t, mount.remounts, 2)
	assert.NotZero(t, mount.remounts[1]&unix.MS_RDONLY, "read-only must be restored after a failed write")
}

func TestEnsureInitScript_RemountFailureWritesNothing(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	mount := &fakeRootMount{
		flags:     unix.ST_RDONLY,
		remountFn: func(uintptr) error { return errors.New("permission denied") },
	}

	written, err := ensureInitScript(fs, mount, initScriptContent(testZaparooPath))
	require.Error(t, err)
	assert.False(t, written)
	assert.Len(t, mount.remounts, 1, "nothing to restore when the root never became writable")
	_, statErr := fs.Stat(initScriptPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestInitScriptContent(t *testing.T) {
	t.Parallel()
	got := initScriptContent(testZaparooPath)
	assert.Contains(t, got, "ZAPAROO=/media/fat/Scripts/zaparoo.sh\n")
	assert.Contains(t, got, `start) "$ZAPAROO" -service start >/dev/null 2>&1 & ;;`,
		"the start must be backgrounded so rcS is not held up")

	quoted := initScriptContent("/media/fat/My Scripts/it's.sh")
	assert.Contains(t, quoted, `ZAPAROO='/media/fat/My Scripts/it'\''s.sh'`)
}

func TestInitScriptSource(t *testing.T) {
	t.Setenv(config.AppEnv, "/media/usb0/Scripts/zaparoo.sh")
	assert.Equal(t, "/media/usb0/Scripts/zaparoo.sh", initScriptSource())

	t.Setenv(config.AppEnv, "")
	assert.Equal(t, testZaparooPath, initScriptSource())

	t.Setenv(config.AppEnv, "relative/zaparoo.sh")
	assert.Equal(t, testZaparooPath, initScriptSource())
}

func TestEnsureInitScript_FailedStepLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"write", "sync", "close", "chmod", "rename"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			base := initFs(t)
			mount := &fakeRootMount{flags: unix.ST_RDONLY}

			written, err := ensureInitScript(failOpFs{Fs: base, op: op}, mount, initScriptContent(testZaparooPath))
			require.Error(t, err)
			assert.False(t, written)

			entries, err := afero.ReadDir(base, "/etc/init.d")
			require.NoError(t, err)
			require.Len(t, entries, 1, "only rcS may remain")
			assert.Equal(t, "rcS", entries[0].Name())
			require.Len(t, mount.remounts, 2)
			assert.NotZero(t, mount.remounts[1]&unix.MS_RDONLY)
		})
	}
}

// TestEnsureInitScript_RestoreFailureIsReported covers a write that worked
// while the root could not be put back to read-only: the script is in place,
// and the caller still hears that the root was left writable.
func TestEnsureInitScript_RestoreFailureIsReported(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	mount := &fakeRootMount{
		flags: unix.ST_RDONLY,
		remountFn: func(flags uintptr) error {
			if flags&unix.MS_RDONLY != 0 {
				return errors.New("device or resource busy")
			}
			return nil
		},
	}
	content := initScriptContent(testZaparooPath)

	_, err := ensureInitScript(fs, mount, content)
	require.ErrorContains(t, err, "restoring read-only root filesystem")
	got, readErr := afero.ReadFile(fs, initScriptPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(got))
}

func TestEnsureInitScript_MountFlagsErrorWritesNothing(t *testing.T) {
	t.Parallel()
	fs := initFs(t)
	mount := &fakeRootMount{flagsErr: errors.New("statfs failed")}

	written, err := ensureInitScript(fs, mount, initScriptContent(testZaparooPath))
	require.ErrorContains(t, err, "reading root mount flags")
	assert.False(t, written)
	assert.Empty(t, mount.remounts)
}

func TestSysRootMountFlags(t *testing.T) {
	t.Parallel()
	_, err := sysRootMount{}.Flags(t.TempDir())
	require.NoError(t, err)
	_, err = sysRootMount{}.Flags(t.TempDir() + "/missing")
	require.Error(t, err)
}

//nolint:paralleltest // replaces the global logger
func TestInstallInitScriptWith_LogsOutcome(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = prev })

	content := initScriptContent(testZaparooPath)
	fs := initFs(t)

	installInitScriptWith(context.Background(), fs, &fakeRootMount{}, content)
	assert.Contains(t, buf.String(), "installed early start init script")

	buf.Reset()
	installInitScriptWith(context.Background(), fs, &fakeRootMount{}, content)
	assert.Contains(t, buf.String(), "early start init script is up to date")

	buf.Reset()
	installInitScriptWith(context.Background(), failCreateFs{Fs: initFs(t)}, &fakeRootMount{}, content)
	assert.Contains(t, buf.String(), "could not install early start init script")

	buf.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledFs := initFs(t)
	installInitScriptWith(ctx, cancelledFs, &fakeRootMount{}, content)
	assert.Empty(t, buf.String())
	_, statErr := cancelledFs.Stat(initScriptPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a cancelled start must not write")
}

// TestEnsureInitScript_ReadErrorsWriteNothing covers checks that fail before
// any write: neither the root nor the script may be touched.
func TestEnsureInitScript_ReadErrorsWriteNothing(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"stat", "read"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			base := initFs(t)
			mount := &fakeRootMount{flags: unix.ST_RDONLY}

			written, err := ensureInitScript(failOpFs{Fs: base, op: op}, mount, initScriptContent(testZaparooPath))
			require.ErrorIs(t, err, os.ErrPermission)
			assert.False(t, written)
			assert.Empty(t, mount.remounts)
			_, statErr := base.Stat(initScriptPath)
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}
