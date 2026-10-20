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

// Package gamehandoff tells when a launched game has been handed to the
// emulator or core that runs it. The process that loads a game opens its file,
// reads it and usually closes it, and Core can watch those events without any
// cooperation from the emulator: MiSTer's Main streams the game into the FPGA
// core, and RetroArch under Batocera reads it as it starts the content.
package gamehandoff

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// Kind is what happened to the watched file.
type Kind int

const (
	// Open means a process opened the file.
	Open Kind = iota
	// Close means a process closed an open of the file.
	Close
	// Access means a process read from the file.
	Access
)

const (
	watchMask = unix.IN_OPEN | unix.IN_CLOSE_NOWRITE | unix.IN_CLOSE_WRITE | unix.IN_ACCESS
	// eventHeaderSize is the size of struct inotify_event without its name,
	// which a watch on a file never carries.
	eventHeaderSize = unix.SizeofInotifyEvent
)

// Watch reports opens, reads and closes of the file at path until ctx ends, then
// closes the returned channel. A full channel drops no events: the watcher
// blocks, and the kernel queues events meanwhile.
func Watch(ctx context.Context, path string) (<-chan Kind, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("init inotify: %w", err)
	}
	if _, err := unix.InotifyAddWatch(fd, path, watchMask); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("watch %s: %w", path, err)
	}

	// A non-blocking descriptor handed to os.NewFile joins the runtime poller,
	// so Close wakes a pending Read.
	file := os.NewFile(uintptr(fd), "inotify")
	events := make(chan Kind, 16)

	var once sync.Once
	closeFile := func() { once.Do(func() { _ = file.Close() }) }
	stop := context.AfterFunc(ctx, closeFile)

	go func() {
		defer close(events)
		defer stop()
		defer closeFile()

		buf := make([]byte, 4096)
		for {
			n, err := file.Read(buf)
			if err != nil {
				// Closed by the context: nothing more will arrive.
				return
			}
			for off := 0; off+eventHeaderSize <= n; {
				mask := binary.NativeEndian.Uint32(buf[off+4 : off+8])
				size := binary.NativeEndian.Uint32(buf[off+12 : off+16])
				off += eventHeaderSize + int(size)

				var kind Kind
				switch {
				case mask&(unix.IN_IGNORED|unix.IN_UNMOUNT) != 0:
					// The kernel dropped the watch, as when the file was
					// removed or its filesystem unmounted. The descriptor
					// stays open and reads would block forever.
					return
				case mask&unix.IN_OPEN != 0:
					kind = Open
				case mask&(unix.IN_CLOSE_NOWRITE|unix.IN_CLOSE_WRITE) != 0:
					kind = Close
				case mask&unix.IN_ACCESS != 0:
					kind = Access
				default:
					continue
				}
				select {
				case events <- kind:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, nil
}
