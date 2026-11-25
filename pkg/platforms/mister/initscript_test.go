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
	"errors"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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
