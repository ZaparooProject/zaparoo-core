//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareCommandAddsProtocolIdentity(t *testing.T) {
	t.Parallel()

	prepared, err := prepareCommand(&Command{Executable: "true"})
	require.NoError(t, err)
	assert.Equal(t, protocolVersion, prepared.Version)
	assert.Len(t, prepared.LaunchID, 32)
}

func TestPrepareCommandRejectsOversizedMessage(t *testing.T) {
	t.Parallel()

	_, err := prepareCommand(&Command{
		Executable: "true", Args: []string{strings.Repeat("x", protocolLimit)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestValidateCommandRejectsInvalidSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate func(*Command)
		name   string
	}{
		{
			name: "wrong version",
			mutate: func(command *Command) {
				command.Version = protocolVersion + 1
			},
		},
		{
			name: "missing launch ID",
			mutate: func(command *Command) {
				command.LaunchID = ""
			},
		},
		{
			name: "blank executable",
			mutate: func(command *Command) {
				command.Executable = "  "
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			command := Command{Version: protocolVersion, LaunchID: "launch", Executable: "true"}
			tt.mutate(&command)
			require.Error(t, validateCommand(&command))
		})
	}
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

func TestRuntimeDirectoryRestrictsExistingDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "zaparoo")
	require.NoError(t, os.Mkdir(dir, 0o750))
	t.Setenv("XDG_RUNTIME_DIR", root)

	actual, err := runtimeDirectory()

	require.NoError(t, err)
	assert.Equal(t, dir, actual)
	info, err := os.Stat(actual)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
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
