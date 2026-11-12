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

const runMainEnv = "ZAPAROO_TEST_RUN_KNULLI_MAIN"

// runMain runs main() with args in a child copy of the test binary, because
// main exits the process. The child writes its coverage into the parent's
// coverage directory when there is one.
func runMain(t *testing.T, test string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	//nolint:gosec // re-runs this test binary with a fixed test filter
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+test+"$")
	cmd.Env = append(os.Environ(), runMainEnv+"="+strings.Join(args, "\x1f"))
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+f.Value.String())
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// childMain runs main() when this process is the child runMain started.
func childMain() bool {
	args, ok := os.LookupEnv(runMainEnv)
	if !ok {
		return false
	}
	os.Args = append([]string{"zaparoo"}, strings.Split(args, "\x1f")...)
	main()
	return true
}

// The self-update probe runs a staged binary with -version and compares its
// output to config.VersionLine, so the line must name this platform.
func TestVersionFlagReportsPlatform(t *testing.T) {
	if childMain() {
		return
	}
	t.Parallel()

	stdout, _, err := runMain(t, "TestVersionFlagReportsPlatform", "-version")
	require.NoError(t, err)
	assert.Equal(t, config.VersionLine(config.AppVersion, platformids.Knulli),
		strings.TrimSpace(stdout))
}

// Installing from anywhere but the path the service runs would leave a
// service pointing at a binary that is not there.
func TestInstallRefusesBinaryOutsideServicePath(t *testing.T) {
	if childMain() {
		return
	}
	t.Parallel()

	_, stderr, err := runMain(t, "TestInstallRefusesBinaryOutsideServicePath", "-install")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, stderr, "error installing service")
	assert.Contains(t, stderr, "the service runs /userdata/system/zaparoo")
}

// knulli-services starts enabled services at boot, so the service file must
// be enabled as well as started, and stopped before it is replaced.
func TestServiceInstallerUsesKnulliServices(t *testing.T) {
	t.Parallel()

	inst := serviceInstaller()
	assert.Equal(t, "/userdata/system/services/zaparoo_service", inst.Path)
	assert.Equal(t, os.FileMode(0o755), inst.Mode)
	assert.Contains(t, inst.Content, "/userdata/system/zaparoo -service")
	assert.Equal(t, [][]string{{"knulli-services", "stop", "zaparoo_service"}}, inst.Stop)
	assert.Equal(t, [][]string{
		{"knulli-services", "enable", "zaparoo_service"},
		{"knulli-services", "start", "zaparoo_service"},
	}, inst.Enable)
	assert.Equal(t, [][]string{
		{"knulli-services", "stop", "zaparoo_service"},
		{"knulli-services", "disable", "zaparoo_service"},
	}, inst.Disable)
}
