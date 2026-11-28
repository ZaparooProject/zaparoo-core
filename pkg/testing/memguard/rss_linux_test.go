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

package memguard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatm(t *testing.T) {
	t.Parallel()

	rss, err := parseStatm("123456 2500 300 1 0 4000 0\n", 4096)
	require.NoError(t, err)
	assert.Equal(t, uint64(2500*4096), rss)

	_, err = parseStatm("123456", 4096)
	require.Error(t, err)
	_, err = parseStatm("1 x 3", 4096)
	require.Error(t, err)
}

const runawayEnv = "ZAPAROO_MEMGUARD_RUNAWAY"

// TestRunawayIsStopped runs this test binary again as a child that starts the
// guard with a small limit and then keeps allocating, and checks the guard
// stops it with its message instead of letting it grow.
func TestRunawayIsStopped(t *testing.T) {
	t.Parallel()

	if os.Getenv(runawayEnv) == "1" {
		runaway()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	//nolint:gosec // re-runs this test binary
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunawayIsStopped$")
	cmd.Env = append(os.Environ(), runawayEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the child must be stopped, got output: %s", stderr.String())
	require.NoError(t, ctx.Err(), "the child ran until the timeout instead of being stopped")
	assert.Contains(t, stderr.String(), "memguard: "+filepath.Base(os.Args[0])+" resident memory reached")
	assert.Contains(t, stderr.String(), "over the 256 MiB limit")
}

// runaway touches memory in 16 MiB steps, well past the limit, and never
// returns while the guard works.
func runaway() {
	Start(256 << 20)
	held := make([][]byte, 0, 1024)
	for range 1024 {
		chunk := make([]byte, 16<<20)
		for i := range chunk {
			chunk[i] = 1
		}
		held = append(held, chunk)
		time.Sleep(5 * time.Millisecond)
	}
	runtime.KeepAlive(held)
	panic(errors.New("memguard did not stop a 16 GiB runaway"))
}
