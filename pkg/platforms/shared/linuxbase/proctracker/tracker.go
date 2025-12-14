//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

// Package proctracker provides process exit tracking using pidfd_open on Linux 5.3+.
// Falls back to polling when pidfd is unavailable.
package proctracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// ErrProcessNotFound is returned when a process doesn't exist.
var ErrProcessNotFound = errors.New("process not found")

// PollInterval is the default interval for fallback polling.
const PollInterval = 2 * time.Second

// ExitCallback is called when a tracked process exits.
type ExitCallback func(pid int)

// Tracker monitors processes and calls callbacks when they exit.
type Tracker struct {
	tracked  map[int]*trackedProcess
	done     chan struct{}
	wg       sync.WaitGroup
	mu       syncutil.Mutex
	usePidfd bool
}

type trackedProcess struct {
	callback ExitCallback
	cancel   context.CancelFunc
	pid      int
	pidfd    int
}

// New creates a new process tracker.
// It automatically detects whether pidfd_open is available.
func New() *Tracker {
	t := &Tracker{
		tracked:  make(map[int]*trackedProcess),
		usePidfd: checkPidfdSupport(),
		done:     make(chan struct{}),
	}
	if t.usePidfd {
		log.Debug().Msg("proctracker: using pidfd_open for process tracking")
	} else {
		log.Debug().Msg("proctracker: pidfd_open unavailable, using poll fallback")
	}
	return t
}

// Track starts monitoring a process and calls the callback when it exits.
// Returns ErrProcessNotFound if the process doesn't exist.
func (t *Tracker) Track(pid int, callback ExitCallback) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already tracking this PID
	if _, exists := t.tracked[pid]; exists {
		return nil
	}

	// Verify process exists
	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return ErrProcessNotFound
		}
		return fmt.Errorf("check process %d: %w", pid, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tp := &trackedProcess{
		pid:      pid,
		pidfd:    -1,
		callback: callback,
		cancel:   cancel,
	}

	if t.usePidfd {
		// Try pidfd_open
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			// Process may have exited between check and pidfd_open
			if errors.Is(err, unix.ESRCH) {
				cancel()
				return ErrProcessNotFound
			}
			// Fall back to polling for this process
			log.Debug().Err(err).Int("pid", pid).Msg("pidfd_open failed, using poll fallback")
		} else {
			tp.pidfd = fd
		}
	}

	t.tracked[pid] = tp

	t.wg.Add(1)
	if tp.pidfd >= 0 {
		go t.watchPidfd(ctx, tp)
	} else {
		go t.watchPoll(ctx, tp)
	}

	return nil
}

// Untrack stops monitoring a process.
func (t *Tracker) Untrack(pid int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if tp, exists := t.tracked[pid]; exists {
		tp.cancel()
		if tp.pidfd >= 0 {
			_ = unix.Close(tp.pidfd)
		}
		delete(t.tracked, pid)
	}
}

// Stop stops all tracking and waits for goroutines to finish.
func (t *Tracker) Stop() {
	close(t.done)

	t.mu.Lock()
	for _, tp := range t.tracked {
		tp.cancel()
		if tp.pidfd >= 0 {
			_ = unix.Close(tp.pidfd)
		}
	}
	t.tracked = make(map[int]*trackedProcess)
	t.mu.Unlock()

	t.wg.Wait()
}

// watchPidfd uses pidfd + poll() for efficient exit notification.
func (t *Tracker) watchPidfd(ctx context.Context, tp *trackedProcess) {
	defer t.wg.Done()

	pollFds := []unix.PollFd{
		{Fd: int32(tp.pidfd), Events: unix.POLLIN}, //nolint:gosec // pidfd is always small
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		default:
		}

		// Poll with 100ms timeout to allow context cancellation checks
		n, err := unix.Poll(pollFds, 100)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			log.Warn().Err(err).Int("pid", tp.pid).Msg("poll error on pidfd")
			return
		}

		if n > 0 && pollFds[0].Revents&unix.POLLIN != 0 {
			// Process exited
			t.handleExit(tp)
			return
		}
	}
}

// watchPoll uses periodic kill(pid, 0) as fallback.
func (t *Tracker) watchPoll(ctx context.Context, tp *trackedProcess) {
	defer t.wg.Done()

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		case <-ticker.C:
			if err := syscall.Kill(tp.pid, 0); err != nil {
				if errors.Is(err, syscall.ESRCH) {
					// Process exited
					t.handleExit(tp)
					return
				}
				log.Warn().Err(err).Int("pid", tp.pid).Msg("kill(0) error")
			}
		}
	}
}

// handleExit cleans up and calls the exit callback.
func (t *Tracker) handleExit(tp *trackedProcess) {
	t.mu.Lock()
	// Check if still tracking this pid (Untrack might have been called)
	if _, exists := t.tracked[tp.pid]; !exists {
		t.mu.Unlock()
		return
	}
	tp.cancel()
	if tp.pidfd >= 0 {
		_ = unix.Close(tp.pidfd)
	}
	delete(t.tracked, tp.pid)
	t.mu.Unlock()

	log.Debug().Int("pid", tp.pid).Msg("process exited")
	if tp.callback != nil {
		tp.callback(tp.pid)
	}
}

// checkPidfdSupport tests if pidfd_open is available.
func checkPidfdSupport() bool {
	// Try to open pidfd for our own process
	fd, err := unix.PidfdOpen(syscall.Getpid(), 0)
	if err != nil {
		return false
	}
	_ = unix.Close(fd)
	return true
}
