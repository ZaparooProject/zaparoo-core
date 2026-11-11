//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const runMainEnv = "ZAPAROO_TEST_RUN_FIRMWARE_MAIN"

// The self-update probe runs a staged binary with -version and compares its
// output to config.VersionLine, so the line must name this platform. main()
// exits the process, so it runs in a child copy of the test binary. The child
// writes its coverage into the parent's coverage directory when there is one.
func TestVersionFlagReportsFirmwarePlatform(t *testing.T) {
	if os.Getenv(runMainEnv) == "1" {
		os.Args = []string{"zaparoo", "-version"}
		main()
		return
	}

	t.Parallel()

	//nolint:gosec // re-runs this test binary with a fixed test filter
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestVersionFlagReportsFirmwarePlatform$")
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+f.Value.String())
	}

	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, config.VersionLine(config.AppVersion, platformids.Firmware),
		strings.TrimSpace(string(out)))
}
