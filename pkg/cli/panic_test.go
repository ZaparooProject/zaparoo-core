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

package cli

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const panicChildEnv = "ZAPAROO_TEST_EXIT_ON_PANIC"

func TestReportPanicWritesValueAndStack(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reportPanic(&out, "boom")

	assert.Contains(t, out.String(), "Panic: boom\n")
	assert.Contains(t, out.String(), "TestReportPanicWritesValueAndStack",
		"the stack must show where the panic happened")
}

func TestExitOnPanicWithoutPanicReturns(t *testing.T) {
	t.Parallel()

	func() {
		defer ExitOnPanic()
	}()
}

// ExitOnPanic exits the process, so the panic runs in a child copy of the
// test binary. The child writes its coverage into the parent's coverage
// directory when there is one.
func TestExitOnPanicExitsWithStatusOne(t *testing.T) {
	if os.Getenv(panicChildEnv) == "1" {
		defer ExitOnPanic()
		panic("boom")
	}

	t.Parallel()

	//nolint:gosec // re-runs this test binary with a fixed test filter
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExitOnPanicExitsWithStatusOne$")
	cmd.Env = append(os.Environ(), panicChildEnv+"=1")
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+f.Value.String())
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "Panic: boom")
}
