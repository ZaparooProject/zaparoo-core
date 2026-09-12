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

package api

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestHTTPResponseWriteLogLevel(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		want zerolog.Level
	}{
		"broken pipe": {syscall.EPIPE, zerolog.WarnLevel},
		"reset":       {syscall.ECONNRESET, zerolog.WarnLevel},
		"wrapped socket": {
			&net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)},
			zerolog.WarnLevel,
		},
		"wrapped reset":      {fmt.Errorf("response: %w", syscall.ECONNRESET), zerolog.WarnLevel},
		"joined disconnects": {errors.Join(syscall.EPIPE, syscall.ECONNRESET), zerolog.WarnLevel},
		"mixed failure":      {errors.Join(syscall.EPIPE, syscall.EIO), zerolog.ErrorLevel},
		"I/O":                {syscall.EIO, zerolog.ErrorLevel},
		"timeout":            {os.ErrDeadlineExceeded, zerolog.ErrorLevel},
		"protocol":           {errors.New("invalid protocol"), zerolog.ErrorLevel},
		"text only":          {errors.New("broken pipe"), zerolog.ErrorLevel},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, httpResponseWriteLogLevel(tc.err))
		})
	}
}
