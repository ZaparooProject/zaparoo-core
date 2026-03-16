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

package mediascanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// ErrFsTimeout is returned when a filesystem operation exceeds the timeout.
var ErrFsTimeout = errors.New("filesystem operation timed out")

// defaultFsTimeout is the maximum time to wait for a single filesystem
// operation before giving up. This prevents indefinite hangs on stale
// network mounts (CIFS, NFS with hard mounts).
//
// Limitation: when a timeout or context cancellation fires, the underlying
// goroutine performing the syscall (os.Stat, os.ReadDir, etc.) cannot be
// interrupted and will remain blocked until the kernel returns. On a stale
// NFS/CIFS mount this may take minutes. The number of leaked goroutines is
// bounded by the number of distinct paths checked during a scan (typically
// <50 root/system folders), after which context cancellation prevents
// further operations from being started.
const defaultFsTimeout = 10 * time.Second

// doWithTimeout runs fn in a goroutine and waits for it to complete, the
// context to be cancelled, or the defaultFsTimeout to elapse — whichever
// comes first. The op and path parameters are used for logging and error
// messages when the timeout fires.
func doWithTimeout[T any](ctx context.Context, fn func() (T, error), op, path string) (T, error) {
	if ctx.Err() != nil {
		var zero T
		return zero, ctx.Err()
	}

	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		val, err := fn()
		ch <- result{val, err}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, defaultFsTimeout)
	defer cancel()

	select {
	case r := <-ch:
		return r.val, r.err
	case <-timeoutCtx.Done():
		var zero T
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		log.Warn().Str("path", path).Dur("timeout", defaultFsTimeout).
			Msgf("filesystem %s timed out (possible stale mount)", op)
		return zero, fmt.Errorf("%w: %s %s", ErrFsTimeout, op, path)
	}
}

// statWithContext runs os.Stat with a timeout. If the context is cancelled
// or the timeout elapses, it returns immediately. The underlying goroutine
// may continue running if the kernel call is blocked (e.g., on a stale
// mount), but will eventually unblock when the mount times out or recovers.
func statWithContext(ctx context.Context, path string) (os.FileInfo, error) {
	return doWithTimeout(ctx, func() (os.FileInfo, error) {
		return os.Stat(path)
	}, "stat", path)
}

// readDirWithContext runs os.ReadDir with a timeout. Same timeout and
// cancellation semantics as statWithContext.
func readDirWithContext(ctx context.Context, path string) ([]os.DirEntry, error) {
	return doWithTimeout(ctx, func() ([]os.DirEntry, error) {
		return os.ReadDir(path)
	}, "readdir", path)
}

// lstatWithContext runs os.Lstat with a timeout. Same timeout and
// cancellation semantics as statWithContext.
func lstatWithContext(ctx context.Context, path string) (os.FileInfo, error) {
	return doWithTimeout(ctx, func() (os.FileInfo, error) {
		return os.Lstat(path)
	}, "lstat", path)
}

// evalSymlinksWithContext runs filepath.EvalSymlinks with a timeout. Same
// timeout and cancellation semantics as statWithContext.
func evalSymlinksWithContext(ctx context.Context, path string) (string, error) {
	return doWithTimeout(ctx, func() (string, error) {
		return filepath.EvalSymlinks(path)
	}, "evalsymlinks", path)
}
