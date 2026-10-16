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

package inputactivity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// resyncInterval is how often the device list is re-read even without a
// hotplug event, because nodes and /proc entries do not appear atomically.
const resyncInterval = 10 * time.Second

const (
	readBufSize     = 4096
	maxReadsPerWake = 16
)

type pollDev struct {
	handle  func(p []byte)
	path    string
	fd      int
	removed bool
}

// pollSet blocks on many device fds at once with no CPU use while they are
// silent. Every fd is read non-blocking and a wake pipe interrupts the wait,
// so cancelling or changing the device set never leaves a read stuck.
//
// Only the run goroutine closes fds. add and remove just edit the set and wake
// the loop, so a descriptor number can never be reused while a read is in
// flight on it.
type pollSet struct {
	devs    map[string]*pollDev
	retired []int
	mu      syncutil.Mutex
	wakeR   int
	wakeW   int
	closed  bool
}

func newPollSet() (*pollSet, error) {
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
		return nil, fmt.Errorf("create wake pipe: %w", err)
	}
	return &pollSet{devs: make(map[string]*pollDev), wakeR: fds[0], wakeW: fds[1]}, nil
}

// add registers an open fd. The set owns the fd from here on. It reports
// false, and takes nothing, when path is already present.
func (s *pollSet) add(path string, fd int, handle func(p []byte)) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	if dev, ok := s.devs[path]; ok {
		if !dev.removed {
			s.mu.Unlock()
			return false
		}
		// Replacing an entry the loop has not reaped yet. The loop may be
		// polling its descriptor right now, so it is parked and closed by the
		// loop (or closeAll) rather than here.
		s.retired = append(s.retired, dev.fd)
	}
	s.devs[path] = &pollDev{path: path, fd: fd, handle: handle}
	s.mu.Unlock()
	s.wake()
	return true
}

// has reports whether path is registered and live.
func (s *pollSet) has(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, ok := s.devs[path]
	return ok && !dev.removed
}

// paths returns the live registered paths.
func (s *pollSet) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.devs))
	for p, dev := range s.devs {
		if !dev.removed {
			out = append(out, p)
		}
	}
	return out
}

// remove schedules path to be closed by the run loop.
func (s *pollSet) remove(path string) {
	s.mu.Lock()
	if dev, ok := s.devs[path]; ok {
		dev.removed = true
	}
	s.mu.Unlock()
	s.wake()
}

// wake interrupts the poll loop. It is safe to call after the set is closed:
// the pipe's descriptor numbers may already belong to something else by then,
// so nothing is written once closeAll has run.
func (s *pollSet) wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	_, _ = unix.Write(s.wakeW, []byte{1})
}

// snapshot reaps removed devices and returns the pollfd list, wake pipe first.
func (s *pollSet) snapshot() (fds []unix.PollFd, devs []*pollDev) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, fd := range s.retired {
		_ = unix.Close(fd)
	}
	s.retired = nil
	fds = make([]unix.PollFd, 1, len(s.devs)+1)
	fds[0] = unix.PollFd{Fd: int32(s.wakeR), Events: unix.POLLIN} //nolint:gosec // fds are small
	devs = make([]*pollDev, 0, len(s.devs))
	for p, dev := range s.devs {
		if dev.removed {
			_ = unix.Close(dev.fd)
			delete(s.devs, p)
			continue
		}
		fds = append(fds, unix.PollFd{Fd: int32(dev.fd), Events: unix.POLLIN}) //nolint:gosec // fds are small
		devs = append(devs, dev)
	}
	return fds, devs
}

// run waits on every registered fd until ctx is cancelled, then closes
// everything the set owns. It returns nil on cancellation.
func (s *pollSet) run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, s.wake)
	defer stop()
	defer s.closeAll()

	buf := make([]byte, readBufSize)
	for ctx.Err() == nil {
		fds, devs := s.snapshot()
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("poll input devices: %w", err)
		}
		if fds[0].Revents != 0 {
			drainPipe(s.wakeR, buf)
		}
		for i, dev := range devs {
			revents := fds[i+1].Revents
			if revents == 0 {
				continue
			}
			if revents&unix.POLLIN != 0 {
				if !readAvailable(dev, buf) {
					s.remove(dev.path)
				}
				continue
			}
			// Hangup or error with nothing left to read: the device went away.
			s.remove(dev.path)
		}
	}
	return nil
}

func (s *pollSet) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for p, dev := range s.devs {
		_ = unix.Close(dev.fd)
		delete(s.devs, p)
	}
	if s.closed {
		return
	}
	for _, fd := range s.retired {
		_ = unix.Close(fd)
	}
	s.retired = nil
	s.closed = true
	_ = unix.Close(s.wakeR)
	_ = unix.Close(s.wakeW)
}

func drainPipe(fd int, buf []byte) {
	for {
		n, err := unix.Read(fd, buf)
		if n <= 0 || err != nil {
			return
		}
	}
}

// readAvailable hands everything currently readable to the device. It
// reports false when the device is gone and should be dropped.
func readAvailable(dev *pollDev, buf []byte) bool {
	for range maxReadsPerWake {
		n, err := unix.Read(dev.fd, buf)
		switch {
		case err == nil && n > 0:
			dev.handle(buf[:n])
		case err == nil:
			return false
		case errors.Is(err, unix.EAGAIN):
			return true
		case errors.Is(err, unix.EINTR):
			continue
		default:
			return false
		}
	}
	return true
}

// openNonblock opens a device node for reading without ever blocking.
func openNonblock(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return fd, nil
}

// readNonblocking opens path, reads everything buffered right now and closes
// it again. A device that has nothing buffered yields no bytes.
func readNonblocking(path string) ([]byte, error) {
	fd, err := openNonblock(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()

	var out []byte
	buf := make([]byte, readBufSize)
	for len(out) < 1<<20 {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		switch {
		case err == nil && n > 0:
			continue
		case err == nil, errors.Is(err, unix.EAGAIN), errors.Is(err, io.EOF):
			return out, nil
		case errors.Is(err, unix.EINTR):
			continue
		default:
			return out, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return out, nil
}

// hotplug reports changes to device nodes. It returns a channel that gets a
// token whenever a matching node appears or disappears, and a stop function
// that releases the watcher and waits for its goroutine. When no watch can be
// made the channel never fires and callers rely on their periodic resync.
func hotplug(dirs []string, match func(name string) bool) (events <-chan struct{}, stop func()) {
	ch := make(chan struct{}, 1)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Debug().Err(err).Msg("input activity: no hotplug watcher")
		return ch, func() {}
	}
	watching := 0
	for _, dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			log.Debug().Err(err).Str("dir", dir).Msg("input activity: cannot watch directory")
			continue
		}
		watching++
	}
	if watching == 0 {
		_ = watcher.Close()
		return ch, func() {}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if !match(filepath.Base(ev.Name)) {
					continue
				}
				select {
				case ch <- struct{}{}:
				default:
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return ch, func() {
		_ = watcher.Close()
		<-done
	}
}

// managed runs a pollSet together with the loop that keeps its device list
// and any sampling work current. It returns when ctx is cancelled and every
// goroutine it started has finished.
type managed struct {
	set *pollSet
	// resync brings the set in line with the devices that exist now.
	resync func()
	// sample runs every sampleEvery when non-nil.
	sample      func()
	match       func(name string) bool
	watchDirs   []string
	sampleEvery time.Duration
}

func (m *managed) run(ctx context.Context) error {
	runErr := make(chan error, 1)
	go func() { runErr <- m.set.run(ctx) }()

	events, stopHotplug := hotplug(m.watchDirs, m.match)
	defer stopHotplug()

	m.resync()
	resync := time.NewTicker(resyncInterval)
	defer resync.Stop()

	var sampleC <-chan time.Time
	if m.sample != nil {
		sampler := time.NewTicker(m.sampleEvery)
		defer sampler.Stop()
		sampleC = sampler.C
	}

	for {
		select {
		case <-ctx.Done():
			return <-runErr
		case err := <-runErr:
			return err
		case <-events:
			m.resync()
		case <-resync.C:
			m.resync()
		case <-sampleC:
			m.sample()
		}
	}
}
