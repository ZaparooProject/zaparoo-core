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

package methods

import (
	"os"
	"path/filepath"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

var procGetDiskFreeSpaceW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")

// thumbCacheClusterBytes is the allocation unit of the volume holding dir, or
// 0 when it cannot be read. Windows reports no per-file allocation in a stat,
// so file sizes are rounded up to it instead.
func thumbCacheClusterBytes(dir string) int64 {
	root := filepath.VolumeName(dir) + string(filepath.Separator)
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	var sectorsPerCluster, bytesPerSector, freeClusters, totalClusters uint32
	//nolint:gosec // G103: pointers are to locals the call fills in
	ok, _, callErr := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)),
		uintptr(unsafe.Pointer(&freeClusters)),
		uintptr(unsafe.Pointer(&totalClusters)),
	)
	if ok == 0 {
		log.Debug().Err(callErr).Str("volume", root).Msg("media.image: thumb cache: cluster size unavailable")
		return 0
	}
	return int64(sectorsPerCluster) * int64(bytesPerSector)
}

// thumbFileDiskBytes is the space a cache file takes on disk: its size rounded
// up to whole clusters. See the Unix version.
func thumbFileDiskBytes(info os.FileInfo, clusterBytes int64) int64 {
	size := info.Size()
	if clusterBytes <= 0 || size == 0 {
		return size
	}
	return (size + clusterBytes - 1) / clusterBytes * clusterBytes
}
