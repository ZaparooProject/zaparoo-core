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

//go:build linux

package opticaldrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/sys/unix"
)

var (
	discReadRetryDelay = 10 * time.Millisecond
	unixPread          = unix.Pread
	unixClose          = unix.Close
)

type unixDiscDeviceReader struct {
	fd int
}

func openDiscDevice(devicePath string) (contextReaderAtCloser, error) {
	fd, err := unix.Open(devicePath, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open optical device: %w", err)
	}
	return &unixDiscDeviceReader{fd: fd}, nil
}

func (r *unixDiscDeviceReader) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, err := unixPread(r.fd, p[total:], off+int64(total))
		if n > 0 {
			total += n
		}
		if err == nil {
			if n == 0 {
				return total, io.EOF
			}
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
			if waitDiscReadRetry(ctx) != nil {
				return total, fmt.Errorf("wait for optical device read: %w", ctx.Err())
			}
			continue
		}
		return total, fmt.Errorf("read optical device: %w", err)
	}
	return total, nil
}

func (r *unixDiscDeviceReader) Close() error {
	if err := unixClose(r.fd); err != nil {
		return fmt.Errorf("close optical device: %w", err)
	}
	return nil
}

func waitDiscReadRetry(ctx context.Context) error {
	timer := time.NewTimer(discReadRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
