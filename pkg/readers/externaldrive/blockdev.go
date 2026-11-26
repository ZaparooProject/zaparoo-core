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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

// Only the Linux detectors fill DeviceNode and DevNum, so on other platforms
// every function here takes its "can't check" path.

const defaultSysRoot = "/sys"

var (
	// devNumRegex matches a MAJ:MIN device number as written in mountinfo.
	devNumRegex = regexp.MustCompile(`^(\d+):(\d+)$`)

	// Kernel partition naming: a disk whose name ends in a letter gets the
	// partition number appended (sda -> sda1); one whose name ends in a digit
	// gets "p" and the number (mmcblk0 -> mmcblk0p1, loop1 -> loop1p2).
	letterDiskPartitionRegex = regexp.MustCompile(`^((?:sd|vd|hd|xvd)[a-z]+)\d+$`)
	digitDiskPartitionRegex  = regexp.MustCompile(`^(.*\d)p\d+$`)
)

// realDevNum returns devNum when it is a well-formed MAJ:MIN with a non-zero
// major. Major 0 is the anonymous range (btrfs subvolumes, some FUSE mounts),
// which has no /sys/dev/block entry.
func realDevNum(devNum string) (string, bool) {
	m := devNumRegex.FindStringSubmatch(devNum)
	if len(m) != 3 || m[1] == "0" {
		return "", false
	}
	return devNum, true
}

// kernelDevName returns the name under /dev for a plain kernel device node
// such as /dev/sda1, or false for anything else (UUIDs, /dev/mapper/x).
func kernelDevName(deviceNode string) (string, bool) {
	name, ok := strings.CutPrefix(deviceNode, "/dev/")
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", false
	}
	return name, true
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pathMissing reports whether path is known not to exist. Other stat errors,
// such as permission or I/O errors, leave it unknown and return false.
func pathMissing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// isBlockDevicePresent reports whether the block device behind a mount still
// exists. It returns false only on evidence the device is gone, and true when
// nothing can be checked.
//
// The mount's device number is preferred because it is what the kernel
// actually mounted: /dev/root, loop devices and device-mapper nodes all have
// one, while their names may not match any sysfs entry.
func isBlockDevicePresent(sysRoot string, event *MountEvent) bool {
	if devNum, ok := realDevNum(event.DevNum); ok {
		return !pathMissing(filepath.Join(sysRoot, "dev", "block", devNum))
	}
	if !strings.HasPrefix(event.DeviceNode, "/dev/") {
		return true
	}
	if name, ok := kernelDevName(event.DeviceNode); ok &&
		!pathMissing(filepath.Join(sysRoot, "class", "block", name)) {
		return true
	}
	return !pathMissing(event.DeviceNode)
}

// parentDisk returns the kernel name of the whole disk that holds the
// mount's block device (sda for sda1, mmcblk0 for mmcblk0p1, loop1 for loop1),
// or "" when sysfs can't tell.
func parentDisk(sysRoot, devNum, deviceNode string) string {
	var link string
	if dn, ok := realDevNum(devNum); ok {
		link = filepath.Join(sysRoot, "dev", "block", dn)
	} else if name, ok := kernelDevName(deviceNode); ok {
		link = filepath.Join(sysRoot, "class", "block", name)
	} else {
		return ""
	}

	// Both links point at .../block/<disk> or .../block/<disk>/<partition>.
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		return ""
	}
	if pathExists(filepath.Join(target, "partition")) {
		return filepath.Base(filepath.Dir(target))
	}
	return filepath.Base(target)
}

// getBaseDevice extracts the whole-disk device path from a partition device
// path: /dev/sda1 -> /dev/sda, /dev/mmcblk0p1 -> /dev/mmcblk0. Whole-disk
// names such as /dev/loop1 or /dev/mmcblk0, and anything not under /dev/,
// are returned unchanged.
func getBaseDevice(deviceID string) string {
	name, ok := kernelDevName(deviceID)
	if !ok {
		return deviceID
	}
	if m := letterDiskPartitionRegex.FindStringSubmatch(name); m != nil {
		return "/dev/" + m[1]
	}
	if m := digitDiskPartitionRegex.FindStringSubmatch(name); m != nil {
		return "/dev/" + m[1]
	}
	return deviceID
}

// siblingMountsFrom returns the mount paths of other partitions on the same
// physical disk as the event, from the given mount table. Only whole-
// filesystem mounts under removable media locations are returned; bind
// mounts of subdirectories are skipped so they aren't searched as drives.
func siblingMountsFrom(sysRoot, disk, primary string, entries []helpers.MountEntry) []string {
	if disk == "" {
		return nil
	}
	var mounts []string
	seen := map[string]bool{primary: true}
	for i := range entries {
		e := &entries[i]
		if !isWholeFilesystemMount(e) || seen[e.Mountpoint] || !isRemovableMountPath(e.Mountpoint) {
			continue
		}
		if parentDisk(sysRoot, e.DevNum, e.Source) != disk {
			continue
		}
		seen[e.Mountpoint] = true
		mounts = append(mounts, e.Mountpoint)
	}
	return mounts
}

// isWholeFilesystemMount reports whether a mount shows a filesystem from its
// top rather than being a bind mount of a subdirectory. A Btrfs subvolume
// mount's root is the subvolume path, which its subvol= option repeats; a bind
// mount of a directory inside it has a longer root.
func isWholeFilesystemMount(e *helpers.MountEntry) bool {
	if e.Root == "/" {
		return true
	}
	if e.FSType != "btrfs" {
		return false
	}
	for opt := range strings.SplitSeq(e.SuperOptions, ",") {
		if subvol, ok := strings.CutPrefix(opt, "subvol="); ok {
			return subvol == e.Root
		}
	}
	return false
}

// isRemovableMountPath reports whether a mount path is in a location used for
// removable media.
func isRemovableMountPath(mountPath string) bool {
	return strings.HasPrefix(mountPath, "/media/") ||
		strings.HasPrefix(mountPath, "/mnt/") ||
		strings.HasPrefix(mountPath, "/run/media/")
}
