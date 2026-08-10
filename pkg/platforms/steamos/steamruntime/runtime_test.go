//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareCommandAddsProtocolIdentity(t *testing.T) {
	t.Parallel()

	prepared, err := prepareCommand(&Command{Executable: "/bin/true"})
	require.NoError(t, err)
	assert.Equal(t, protocolVersion, prepared.Version)
	assert.Len(t, prepared.LaunchID, 32)
}

func TestPrepareCommandRejectsOversizedMessage(t *testing.T) {
	t.Parallel()

	_, err := prepareCommand(&Command{
		Executable: "/bin/true", Args: []string{strings.Repeat("x", protocolLimit)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestValidateResultRejectsWrongLaunch(t *testing.T) {
	t.Parallel()

	err := validateResult(&commandResult{Version: protocolVersion, LaunchID: "wrong"}, "expected")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

func TestCommandEnvironmentAppliesOverrides(t *testing.T) {
	t.Setenv("ZAPAROO_RUNTIME_ENV_TEST", "inherited")

	env := commandEnvironment([]string{"ZAPAROO_RUNTIME_ENV_TEST=override"})

	assert.Contains(t, env, "ZAPAROO_RUNTIME_ENV_TEST=override")
	assert.NotContains(t, env, "ZAPAROO_RUNTIME_ENV_TEST=inherited")
}

func TestRunRuntimeWithoutPendingLaunchExitsCleanly(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	require.NoError(t, Run(t.Context()))
}

func TestPrepareCommandRequiresExecutable(t *testing.T) {
	t.Parallel()

	_, err := prepareCommand(&Command{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executable is required")
}
