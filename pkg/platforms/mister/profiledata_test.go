//go:build linux

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
	"encoding/binary"
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMounter simulates the kernel mount table: binds resolve their source
// through the current table (so a bind of a path inside a cifs mount
// carries the cifs identity, exactly like the kernel), and unmount removes
// the topmost entry at a target.
type fakeMounter struct {
	bindErr       error
	mounts        []mountEntry
	binds         int
	bindAttempts  int
	failBindAtTry int
}

type failMkdirFS struct {
	afero.Fs
	path string
}

func (f failMkdirFS) MkdirAll(path string, perm iofs.FileMode) error {
	if strings.Contains(path, f.path) {
		return errors.New("injected mkdir failure")
	}
	if err := f.Fs.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}
	return nil
}

func (f *fakeMounter) Mounts() ([]mountEntry, error) {
	out := make([]mountEntry, len(f.mounts))
	copy(out, f.mounts)
	return out, nil
}

func (f *fakeMounter) BindMount(source, target string) (mountEntry, error) {
	f.bindAttempts++
	if f.bindErr != nil && (f.failBindAtTry == 0 || f.bindAttempts == f.failBindAtTry) {
		return mountEntry{}, f.bindErr
	}
	f.binds++

	// Resolve source through the deepest, most recent mount covering it.
	entry := mountEntry{
		Root: source, Mountpoint: target, FSType: "exfat", Source: "/dev/mmcblk0p1",
	}
	bestLen := -1
	for _, m := range f.mounts {
		if source == m.Mountpoint || strings.HasPrefix(source, m.Mountpoint+"/") {
			if len(m.Mountpoint) >= bestLen {
				bestLen = len(m.Mountpoint)
				rel := strings.TrimPrefix(source, m.Mountpoint)
				entry.Root = strings.TrimSuffix(m.Root, "/") + rel
				entry.FSType = m.FSType
				entry.Source = m.Source
			}
		}
	}
	f.mounts = append(f.mounts, entry)
	return entry, nil
}

func (f *fakeMounter) Unmount(target string) error {
	for i := len(f.mounts) - 1; i >= 0; i-- {
		if f.mounts[i].Mountpoint == target {
			f.mounts = append(f.mounts[:i], f.mounts[i+1:]...)
			return nil
		}
	}
	return errors.New("not mounted")
}

func newTestManager(m *fakeMounter) (*profileDataManager, afero.Fs) {
	fs := afero.NewMemMapFs()
	return &profileDataManager{
		fs:     fs,
		m:      m,
		ledger: loadMountLedger(fs, mountLedgerPath),
	}, fs
}

func writeDeviceBin(t *testing.T, fs afero.Fs, dev uint32) {
	t.Helper()
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, dev)
	require.NoError(t, afero.WriteFile(fs, deviceBinPath, data, 0o644))
}

func kidA() platforms.ProfileRef {
	return platforms.ProfileRef{ID: "11111111-aaaa-bbbb-cccc-000000000001", Name: "Kid A"}
}

func kidB() platforms.ProfileRef {
	return platforms.ProfileRef{ID: "22222222-aaaa-bbbb-cccc-000000000002", Name: "Kid B"}
}

func allItems() []string {
	return []string{profileDataItemSaves, profileDataItemSavestates}
}

func TestParseMountInfo(t *testing.T) {
	t.Parallel()
	data := `21 26 179:1 / /media/fat rw,noatime - exfat /dev/mmcblk0p1 rw,iocharset=utf8
36 21 0:41 /saves /media/fat/saves rw,relatime shared:1 - cifs //10.0.0.3/MiSTer rw,username=x
40 21 179:1 /System\040Volume\040Information /media/fat/svi rw - exfat /dev/mmcblk0p1 rw
garbage line
`
	entries := parseMountInfo(data)
	require.Len(t, entries, 3)
	assert.Equal(t, mountEntry{
		Root: "/", Mountpoint: "/media/fat", FSType: "exfat", Source: "/dev/mmcblk0p1",
	}, entries[0])
	assert.Equal(t, mountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves", FSType: "cifs", Source: "//10.0.0.3/MiSTer",
	}, entries[1])
	assert.Equal(t, "/System Volume Information", entries[2].Root, "octal escapes decoded")
}

func TestResolveStorageRoot(t *testing.T) {
	t.Parallel()

	t.Run("no device.bin means SD", func(t *testing.T) {
		t.Parallel()
		d, _ := newTestManager(&fakeMounter{})
		root, err := d.resolveStorageRoot(nil)
		require.NoError(t, err)
		assert.Equal(t, "/media/fat", root)
	})

	t.Run("device.bin zero means SD", func(t *testing.T) {
		t.Parallel()
		d, fs := newTestManager(&fakeMounter{})
		writeDeviceBin(t, fs, 0)
		root, err := d.resolveStorageRoot(nil)
		require.NoError(t, err)
		assert.Equal(t, "/media/fat", root)
	})

	t.Run("USB root picks first mounted non-ext drive", func(t *testing.T) {
		t.Parallel()
		d, fs := newTestManager(&fakeMounter{})
		writeDeviceBin(t, fs, 1)
		mounts := []mountEntry{
			{Root: "/", Mountpoint: "/media/usb0", FSType: "ext4", Source: "/dev/sda1"},
			{Root: "/", Mountpoint: "/media/usb1", FSType: "vfat", Source: "/dev/sdb1"},
		}
		root, err := d.resolveStorageRoot(mounts)
		require.NoError(t, err)
		// usb0 is skipped: main's isPathMounted treats ext-formatted drives
		// as not-a-storage-root.
		assert.Equal(t, "/media/usb1", root)
	})

	t.Run("USB root with no drive is unavailable", func(t *testing.T) {
		t.Parallel()
		d, fs := newTestManager(&fakeMounter{})
		writeDeviceBin(t, fs, 1)
		_, err := d.resolveStorageRoot(nil)
		require.ErrorIs(t, err, platforms.ErrProfileDataUnavailable)
	})
}

func TestApply_SDPlainDir(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, fs := newTestManager(m)

	require.NoError(t, d.apply(kidA(), allItems()))

	// Both items bind their pool dirs over the live dirs.
	saves := mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, saves, 1)
	assert.Equal(t, "/media/fat/zaparoo/profiles/"+kidA().ID+"/saves", saves[0].Root)
	states := mountsAt(m.mounts, "/media/fat/savestates")
	require.Len(t, states, 1)

	// Pool dirs and the human-readable label exist.
	poolDir := "/media/fat/zaparoo/profiles/" + kidA().ID
	name, err := afero.ReadFile(fs, filepath.Join(poolDir, "name.txt"))
	require.NoError(t, err)
	assert.Equal(t, "Kid A\n", string(name))
	exists, err := afero.DirExists(fs, filepath.Join(poolDir, "saves"))
	require.NoError(t, err)
	assert.True(t, exists)

	// Idempotent: reapplying the same profile does not churn mounts.
	binds := m.binds
	require.NoError(t, d.apply(kidA(), allItems()))
	assert.Equal(t, binds, m.binds)
	assert.Len(t, mountsAt(m.mounts, "/media/fat/saves"), 1)

	// Deactivating removes our binds and only ours.
	require.NoError(t, d.apply(platforms.ProfileRef{}, allItems()))
	assert.Empty(t, mountsAt(m.mounts, "/media/fat/saves"))
	assert.Empty(t, mountsAt(m.mounts, "/media/fat/savestates"))
	assert.Empty(t, d.ledger.entries)
}

func TestApply_SwitchBetweenProfiles(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, _ := newTestManager(m)

	require.NoError(t, d.apply(kidA(), allItems()))
	require.NoError(t, d.apply(kidB(), allItems()))

	saves := mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, saves, 1, "old profile's bind is removed, not stacked under")
	assert.Contains(t, saves[0].Root, kidB().ID)
}

func TestApply_PreparationFailureLeavesPreviousProfileUntouched(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, _ := newTestManager(m)

	require.NoError(t, d.apply(kidA(), allItems()))
	bindsBefore := m.binds
	d.fs = failMkdirFS{Fs: d.fs, path: filepath.Join(kidB().ID, profileDataItemSavestates)}

	err := d.apply(kidB(), allItems())
	require.Error(t, err)
	assert.Equal(t, bindsBefore, m.binds, "preparation failure must not change mounts")
	for _, item := range allItems() {
		stack := mountsAt(m.mounts, filepath.Join(misterconfig.SDRootDir, item))
		require.Len(t, stack, 1)
		assert.Contains(t, stack[0].Root, kidA().ID)
	}
}

func TestApply_SecondItemFailureRestoresPreviousProfile(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, _ := newTestManager(m)

	require.NoError(t, d.apply(kidA(), allItems()))
	// Kid B's saves bind succeeds, then its savestates bind fails. Both
	// items must return to Kid A before ApplyProfile reports the failure.
	m.bindErr = errors.New("mount: I/O error")
	m.failBindAtTry = m.bindAttempts + 2

	err := d.apply(kidB(), allItems())
	require.Error(t, err)

	for _, item := range allItems() {
		stack := mountsAt(m.mounts, filepath.Join(misterconfig.SDRootDir, item))
		require.Len(t, stack, 1)
		assert.Contains(t, stack[0].Root, kidA().ID, "%s should be restored to Kid A", item)
	}
	require.Len(t, d.ledger.entries, 2)
	for _, entry := range d.ledger.entries {
		assert.Equal(t, kidA().ID, entry.ProfileID)
	}
}

func TestApply_LedgerFailureRollsBackNewBind(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, fs := newTestManager(m)
	// Pool creation remains writable, but durable ownership recording does
	// not. Apply must remove the already-created bind before returning.
	d.ledger.fs = afero.NewReadOnlyFs(fs)

	err := d.apply(kidA(), []string{profileDataItemSaves})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to record profile bind")
	assert.Empty(t, mountsAt(m.mounts, "/media/fat/saves"))
	assert.Empty(t, d.ledger.entries)
}

func TestApply_USBRoot(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{mounts: []mountEntry{
		{Root: "/", Mountpoint: "/media/usb0", FSType: "vfat", Source: "/dev/sda1"},
	}}
	d, fs := newTestManager(m)
	writeDeviceBin(t, fs, 1)

	require.NoError(t, d.apply(kidA(), allItems()))

	// Targets and pool both follow the USB storage root.
	saves := mountsAt(m.mounts, "/media/usb0/saves")
	require.Len(t, saves, 1)
	assert.Equal(t, "/zaparoo/profiles/"+kidA().ID+"/saves", saves[0].Root)
	assert.Equal(t, "/dev/sda1", saves[0].Source)
	assert.Empty(t, mountsAt(m.mounts, "/media/fat/saves"))
}

func TestApply_NASSavesStackOnForeignMount(t *testing.T) {
	t.Parallel()
	// A cifs saves mount (RetroNAS-style) already owns the target.
	nas := mountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves",
		FSType: "cifs", Source: "//10.0.0.3/MiSTer",
	}
	m := &fakeMounter{mounts: []mountEntry{nas}}
	d, fs := newTestManager(m)

	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))

	stack := mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, stack, 2, "our bind stacks on top; the NAS mount is untouched")
	assert.Equal(t, nas, stack[0])
	// The pool lives inside the share, so saves stay on the NAS.
	assert.Equal(t, "cifs", stack[1].FSType)
	assert.Equal(t, "//10.0.0.3/MiSTer", stack[1].Source)
	assert.Equal(t, "/saves/"+nasPoolDirName+"/"+kidA().ID+"/saves", stack[1].Root)

	// The pool directory was created through the (still-mounted) share.
	exists, err := afero.DirExists(fs,
		"/media/fat/saves/"+nasPoolDirName+"/"+kidA().ID+"/saves")
	require.NoError(t, err)
	assert.True(t, exists)

	// Deactivating removes only our layer; the NAS mount remains.
	require.NoError(t, d.apply(platforms.ProfileRef{}, []string{profileDataItemSaves}))
	stack = mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, stack, 1)
	assert.Equal(t, nas, stack[0])
}

func TestApply_ForeignMountLandsOnTopOfOurs(t *testing.T) {
	t.Parallel()
	// Our bind is active, then a late cifs boot script mounts the NAS
	// share on top of it (mount order decides visibility).
	m := &fakeMounter{}
	d, _ := newTestManager(m)
	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))

	nas := mountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves",
		FSType: "cifs", Source: "//10.0.0.3/MiSTer",
	}
	m.mounts = append(m.mounts, nas)

	// Reconcile re-stacks a fresh bind sourced inside the NAS mount; the
	// buried old bind and the NAS mount are both left alone.
	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))
	stack := mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, stack, 3)
	assert.Equal(t, "cifs", stack[2].FSType)
	assert.Equal(t, "/saves/"+nasPoolDirName+"/"+kidA().ID+"/saves", stack[2].Root)
}

func TestApply_SharedNeverTouchesForeignMounts(t *testing.T) {
	t.Parallel()
	nas := mountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves",
		FSType: "cifs", Source: "//10.0.0.3/MiSTer",
	}
	m := &fakeMounter{mounts: []mountEntry{nas}}
	d, _ := newTestManager(m)

	require.NoError(t, d.apply(platforms.ProfileRef{}, []string{profileDataItemSaves}))
	stack := mountsAt(m.mounts, "/media/fat/saves")
	require.Len(t, stack, 1, "shared profile leaves the user's NAS mount in place")
	assert.Equal(t, nas, stack[0])
}

func TestApply_BindFailureReportsError(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{bindErr: errors.New("mount: permission denied")}
	d, _ := newTestManager(m)

	err := d.apply(kidA(), []string{profileDataItemSaves})
	require.Error(t, err)
	assert.NotErrorIs(t, err, platforms.ErrProfileDataUnavailable)
}

func TestApply_ReadOnlyShareIsUnavailable(t *testing.T) {
	t.Parallel()
	nas := mountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves",
		FSType: "cifs", Source: "//10.0.0.3/MiSTer",
	}
	m := &fakeMounter{mounts: []mountEntry{nas}}
	fs := afero.NewMemMapFs()
	d := &profileDataManager{
		fs:     afero.NewReadOnlyFs(fs),
		m:      m,
		ledger: &mountLedger{fs: fs, path: mountLedgerPath},
	}

	err := d.apply(kidA(), []string{profileDataItemSaves})
	require.ErrorIs(t, err, platforms.ErrProfileDataUnavailable,
		"a share we cannot write to makes the swap unavailable, not broken")
	assert.Len(t, mountsAt(m.mounts, "/media/fat/saves"), 1, "no bind was attempted")
}

func TestLedger_PersistsAcrossReload(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, fs := newTestManager(m)
	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))

	// A restarted service (fresh manager, same tmpfs) still owns its bind
	// and can cleanly deactivate it.
	d2 := &profileDataManager{fs: fs, m: m, ledger: loadMountLedger(fs, mountLedgerPath)}
	require.Len(t, d2.ledger.entries, 1)
	require.NoError(t, d2.apply(platforms.ProfileRef{}, []string{profileDataItemSaves}))
	assert.Empty(t, mountsAt(m.mounts, "/media/fat/saves"))
}

func TestLedger_PrunesVanishedMounts(t *testing.T) {
	t.Parallel()
	m := &fakeMounter{}
	d, _ := newTestManager(m)
	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))

	// Someone manually unmounted our bind behind our back.
	require.NoError(t, m.Unmount("/media/fat/saves"))
	require.NoError(t, d.apply(kidA(), []string{profileDataItemSaves}))

	require.Len(t, d.ledger.entries, 1, "stale entry pruned, fresh bind recorded")
	assert.Len(t, mountsAt(m.mounts, "/media/fat/saves"), 1)
}
