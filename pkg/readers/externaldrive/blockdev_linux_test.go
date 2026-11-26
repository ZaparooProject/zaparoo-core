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

package externaldrive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBlockDev is one block device in a fake sysfs tree. parent is empty for
// a whole disk.
type fakeBlockDev struct {
	name   string
	parent string
	devNum string
}

// newFakeSysfs builds a sysfs tree the way the kernel lays it out: device
// directories under devices/, a "partition" file in each partition, and the
// class/block/<name> and dev/block/<MAJ:MIN> symlinks pointing at them.
func newFakeSysfs(t *testing.T, devs ...fakeBlockDev) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class", "block"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dev", "block"), 0o750))
	for _, d := range devs {
		dir := filepath.Join(root, "devices", "virtual", "block", d.name)
		if d.parent != "" {
			dir = filepath.Join(root, "devices", "virtual", "block", d.parent, d.name)
		}
		require.NoError(t, os.MkdirAll(dir, 0o750))
		if d.parent != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "partition"), []byte("1\n"), 0o600))
		}
		require.NoError(t, os.Symlink(dir, filepath.Join(root, "class", "block", d.name)))
		require.NoError(t, os.Symlink(dir, filepath.Join(root, "dev", "block", d.devNum)))
	}
	return root
}

// removeFakeBlockDev removes a device from a fake sysfs tree, as the kernel
// does when a USB stick is pulled.
func removeFakeBlockDev(t *testing.T, root string, d fakeBlockDev) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(root, "class", "block", d.name)))
	require.NoError(t, os.Remove(filepath.Join(root, "dev", "block", d.devNum)))
}

var (
	fakeMmcblk0   = fakeBlockDev{name: "mmcblk0", devNum: "179:0"}
	fakeMmcblk0p1 = fakeBlockDev{name: "mmcblk0p1", parent: "mmcblk0", devNum: "179:1"}
	fakeLoop1     = fakeBlockDev{name: "loop1", devNum: "7:1"}
	fakeSda       = fakeBlockDev{name: "sda", devNum: "8:0"}
	fakeSda1      = fakeBlockDev{name: "sda1", parent: "sda", devNum: "8:1"}
	fakeSda2      = fakeBlockDev{name: "sda2", parent: "sda", devNum: "8:2"}
)

func newMiSTerFakeSysfs(t *testing.T) string {
	t.Helper()
	return newFakeSysfs(t, fakeMmcblk0, fakeMmcblk0p1, fakeLoop1, fakeSda, fakeSda1, fakeSda2)
}

func TestIsBlockDevicePresent(t *testing.T) {
	t.Parallel()
	sys := newMiSTerFakeSysfs(t)

	tests := []struct {
		name     string
		event    MountEvent
		expected bool
	}{
		{
			// The #1898 case: MiSTer's SD card shows up in the mount
			// table as /dev/root, which has no sysfs entry by name.
			name:     "/dev/root with a live device number",
			event:    MountEvent{DeviceNode: "/dev/root", DevNum: "179:1"},
			expected: true,
		},
		{
			name:     "/dev/root whose device number is gone",
			event:    MountEvent{DeviceNode: "/dev/root", DevNum: "179:9"},
			expected: false,
		},
		{
			name:     "loop device by name",
			event:    MountEvent{DeviceNode: "/dev/loop1"},
			expected: true,
		},
		{
			name:     "loop device by device number",
			event:    MountEvent{DeviceNode: "/dev/loop1", DevNum: "7:1"},
			expected: true,
		},
		{
			name:     "partition by name",
			event:    MountEvent{DeviceNode: "/dev/mmcblk0p1"},
			expected: true,
		},
		{
			name:     "yanked partition by device number",
			event:    MountEvent{DeviceNode: "/dev/sdZZZ1", DevNum: "8:17"},
			expected: false,
		},
		{
			name:     "yanked partition by name",
			event:    MountEvent{DeviceNode: "/dev/sdZZZ999"},
			expected: false,
		},
		{
			name:     "anonymous device number falls back to name",
			event:    MountEvent{DeviceNode: "/dev/sda1", DevNum: "0:41"},
			expected: true,
		},
		{
			name:     "malformed device number falls back to name",
			event:    MountEvent{DeviceNode: "/dev/sda1", DevNum: "../../8:1"},
			expected: true,
		},
		{
			name:     "non-device node can't be checked",
			event:    MountEvent{DeviceNode: "UUID-1234-5678"},
			expected: true,
		},
		{
			name:     "nothing to check",
			event:    MountEvent{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isBlockDevicePresent(sys, &tt.event))
		})
	}
}

// A stat error other than "not found" says nothing about the device, so it
// must not get a live mount forgotten and unmounted.
func TestIsBlockDevicePresent_StatErrorIsNotMissing(t *testing.T) {
	t.Parallel()
	sys := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sys, "dev"), 0o750))
	// A file where a directory is expected makes stat fail with ENOTDIR.
	require.NoError(t, os.WriteFile(filepath.Join(sys, "dev", "block"), nil, 0o600))

	assert.True(t, isBlockDevicePresent(sys, &MountEvent{DeviceNode: "/dev/sda1", DevNum: "8:1"}))
}

func TestIsWholeFilesystemMount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		entry    helpers.MountEntry
		expected bool
	}{
		{name: "filesystem root", entry: helpers.MountEntry{Root: "/", FSType: "vfat"}, expected: true},
		{name: "bind mount of a directory", entry: helpers.MountEntry{Root: "/saves", FSType: "exfat"}},
		{
			name:     "Btrfs subvolume",
			entry:    helpers.MountEntry{Root: "/@data", FSType: "btrfs", SuperOptions: "rw,subvol=/@data"},
			expected: true,
		},
		{
			name:  "bind mount inside a Btrfs subvolume",
			entry: helpers.MountEntry{Root: "/@data/roms", FSType: "btrfs", SuperOptions: "rw,subvol=/@data"},
		},
		{name: "Btrfs without subvol option", entry: helpers.MountEntry{Root: "/@data", FSType: "btrfs"}},
		{
			name:  "subvol option on another filesystem",
			entry: helpers.MountEntry{Root: "/@data", FSType: "ext4", SuperOptions: "subvol=/@data"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isWholeFilesystemMount(&tt.entry))
		})
	}
}

func TestParentDisk(t *testing.T) {
	t.Parallel()
	sys := newMiSTerFakeSysfs(t)

	tests := []struct {
		name       string
		devNum     string
		deviceNode string
		expected   string
	}{
		{name: "/dev/root partition by device number", devNum: "179:1", deviceNode: "/dev/root", expected: "mmcblk0"},
		{name: "whole loop device", devNum: "7:1", deviceNode: "/dev/loop1", expected: "loop1"},
		{name: "partition by name", deviceNode: "/dev/sda2", expected: "sda"},
		{name: "whole disk by name", deviceNode: "/dev/sda", expected: "sda"},
		{name: "device number wins over name", devNum: "8:1", deviceNode: "/dev/root", expected: "sda"},
		{name: "unknown device", deviceNode: "/dev/sdq1", expected: ""},
		{name: "mapper path", deviceNode: "/dev/mapper/usb", expected: ""},
		{name: "traversal in name", deviceNode: "/dev/..", expected: ""},
		{name: "nothing", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, parentDisk(sys, tt.devNum, tt.deviceNode))
		})
	}
}

func TestSiblingMountsFrom(t *testing.T) {
	t.Parallel()
	sys := newMiSTerFakeSysfs(t)

	entries := []helpers.MountEntry{
		{Root: "/", Mountpoint: "/", Source: "/dev/loop1", DevNum: "7:1"},
		{Root: "/", Mountpoint: "/media/fat", Source: "/dev/root", DevNum: "179:1"},
		{Root: "/saves", Mountpoint: "/media/fat/saves", Source: "/dev/root", DevNum: "179:1"},
		{Root: "/", Mountpoint: "/media/usb0", Source: "/dev/sda1", DevNum: "8:1"},
		{Root: "/", Mountpoint: "/media/usb1", Source: "/dev/sda2", DevNum: "8:2"},
		{Root: "/", Mountpoint: "/srv/usb", Source: "/dev/sda2", DevNum: "8:2"},
		{
			Root: "/@data", Mountpoint: "/media/usb2", FSType: "btrfs", Source: "/dev/sda2", DevNum: "0:50",
			SuperOptions: "rw,subvol=/@data",
		},
		{
			Root: "/@data/roms", Mountpoint: "/media/usb3", FSType: "btrfs", Source: "/dev/sda2", DevNum: "0:50",
			SuperOptions: "rw,subvol=/@data",
		},
	}

	// The SD card has no other partitions mounted; the USB stick and the
	// bind mount of a subdirectory are not its siblings.
	assert.Empty(t, siblingMountsFrom(sys, "mmcblk0", "/media/fat", entries))

	// Other partitions of the stick are, but only in removable locations,
	// and a Btrfs subvolume counts while a bind mount inside it doesn't.
	assert.Equal(t, []string{"/media/usb1", "/media/usb2"}, siblingMountsFrom(sys, "sda", "/media/usb0", entries))

	assert.Empty(t, siblingMountsFrom(sys, "", "/media/usb0", entries))
}

func newTestReaderWithSysfs(t *testing.T, sys string, mounts []helpers.MountEntry) (
	*Reader, chan readers.Scan, *mockMountDetector,
) {
	t.Helper()
	reader := NewReader(&config.Instance{})
	scanChan := make(chan readers.Scan, 10)
	detector := newMockMountDetector()
	reader.detector = detector
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})
	reader.device = config.ReadersConnect{Driver: DriverID}
	reader.sysRoot = sys
	reader.readMounts = func() ([]helpers.MountEntry, error) { return mounts, nil }
	return reader, scanChan, detector
}

// A drive whose mount table name has no sysfs entry, like MiSTer's /dev/root,
// is read like any other drive instead of being dropped as stale (#1898).
func TestHandleMountEvent_DevRootIsNotStale(t *testing.T) {
	t.Parallel()
	sys := newMiSTerFakeSysfs(t)
	mountDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(mountDir, "zaparoo.txt"), []byte("**launch.system:nes"), 0o600))

	reader, scanChan, detector := newTestReaderWithSysfs(t, sys, nil)
	event := MountEvent{DeviceID: "/dev/root", DeviceNode: "/dev/root", DevNum: "179:1", MountPath: mountDir}

	reader.wg.Add(1)
	reader.handleMountEvent(&event)

	assert.False(t, detector.wasForgotten("/dev/root"))
	select {
	case scan := <-scanChan:
		require.NotNil(t, scan.Token)
		assert.Equal(t, "**launch.system:nes", scan.Token.Text)
	default:
		t.Fatal("expected a scan for the /dev/root drive")
	}

	reader.mu.RLock()
	_, keyed := reader.activeTokens["/dev/mmcblk0"]
	reader.mu.RUnlock()
	assert.True(t, keyed, "token keyed by the physical disk")
}

// Partitions of one disk share a token key even when their IDs are
// unrelated UUIDs, and the unmount of a yanked device still finds that key.
func TestHandleMountEvent_KeyedByPhysicalDisk(t *testing.T) {
	t.Parallel()
	sys := newMiSTerFakeSysfs(t)
	part1 := t.TempDir()
	part2 := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(part1, "zaparoo.txt"), []byte("**launch.system:snes"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(part2, "zaparoo.txt"), []byte("**launch.system:snes"), 0o600))

	reader, scanChan, _ := newTestReaderWithSysfs(t, sys, nil)

	first := MountEvent{DeviceID: "AAAA-1111", DeviceNode: "/dev/sda1", DevNum: "8:1", MountPath: part1}
	second := MountEvent{DeviceID: "BBBB-2222", DeviceNode: "/dev/sda2", DevNum: "8:2", MountPath: part2}

	reader.wg.Add(2)
	reader.handleMountEvent(&first)
	reader.handleMountEvent(&second)

	require.Len(t, scanChan, 1, "one tap for one physical drive")
	<-scanChan

	// The stick is pulled: sysfs entries vanish before the unmount arrives.
	removeFakeBlockDev(t, sys, fakeSda1)
	removeFakeBlockDev(t, sys, fakeSda2)
	removeFakeBlockDev(t, sys, fakeSda)

	reader.handleUnmountEvent("AAAA-1111")
	require.Len(t, scanChan, 1)
	assert.Nil(t, (<-scanChan).Token, "removal scan")

	reader.mu.RLock()
	defer reader.mu.RUnlock()
	assert.Empty(t, reader.activeTokens)
	assert.NotContains(t, reader.deviceKeys, "AAAA-1111")
}

// Only the partition that stored a token removes it. A sibling's unmount that
// arrives after a replacement drive has stored its own token under the same
// disk key leaves that token alone, whether partitions are identified by UUID
// or by device node.
func TestHandleUnmountEvent_LateSiblingKeepsReplacementToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		first, second, replaced string
	}{
		{name: "UUID IDs", first: "AAAA-1111", second: "BBBB-2222", replaced: "CCCC-3333"},
		{name: "device node IDs", first: "/dev/sda1", second: "/dev/sda2", replaced: "/dev/sda1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sys := newMiSTerFakeSysfs(t)
			part1 := t.TempDir()
			part2 := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(part1, "zaparoo.txt"), []byte("**echo:old"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(part2, "zaparoo.txt"), []byte("**echo:old"), 0o600))
			reader, scanChan, _ := newTestReaderWithSysfs(t, sys, nil)

			first := MountEvent{DeviceID: tt.first, DeviceNode: "/dev/sda1", DevNum: "8:1", MountPath: part1}
			second := MountEvent{DeviceID: tt.second, DeviceNode: "/dev/sda2", DevNum: "8:2", MountPath: part2}
			reader.wg.Add(2)
			reader.handleMountEvent(&first)
			reader.handleMountEvent(&second)
			require.Len(t, scanChan, 1)
			<-scanChan

			// The stick is pulled and only the first unmount is handled.
			reader.handleUnmountEvent(tt.first)
			require.Len(t, scanChan, 1)
			assert.Nil(t, (<-scanChan).Token)

			// A replacement stick comes up as sda and stores its token.
			newPart := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(newPart, "zaparoo.txt"), []byte("**echo:new"), 0o600))
			replacement := MountEvent{DeviceID: tt.replaced, DeviceNode: "/dev/sda1", DevNum: "8:1", MountPath: newPart}
			reader.wg.Add(1)
			reader.handleMountEvent(&replacement)
			require.Len(t, scanChan, 1)
			<-scanChan

			// The old stick's second unmount arrives late.
			reader.handleUnmountEvent(tt.second)
			assert.Empty(t, scanChan, "no removal scan for the replacement's token")
			reader.mu.RLock()
			token := reader.activeTokens["/dev/sda"]
			reader.mu.RUnlock()
			require.NotNil(t, token)
			assert.Equal(t, "**echo:new", token.Text)
		})
	}
}
