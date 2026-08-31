//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestValidateResultRejectsInvalidResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		match  string
		result commandResult
	}{
		{
			name: "wrong launch",
			result: commandResult{
				Version: protocolVersion, LaunchID: "wrong",
			},
			match: "mismatch",
		},
		{
			name: "wrong version",
			result: commandResult{
				Version: protocolVersion + 1, LaunchID: "expected",
			},
			match: "version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateResult(&tt.result, "expected")
			require.ErrorContains(t, err, tt.match)
		})
	}
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

func TestRuntimeDirectoryRejectsRelativeXDGPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "relative")

	_, err := runtimeDirectory()
	require.ErrorContains(t, err, "must be absolute")
}

func TestIsInvocation(t *testing.T) {
	t.Parallel()

	assert.True(t, IsInvocation(filepath.Join("usr", "bin", runtimeExecutableName)))
	assert.False(t, IsInvocation(filepath.Join("usr", "bin", "zaparoo")))
}

func unixConnectionPair(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.sock")
	listener, err := listenSocket(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, acceptError := listener.AcceptUnix()
		if acceptError != nil {
			acceptErr <- acceptError
			return
		}
		accepted <- conn
	}()
	client, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	require.NoError(t, err)
	select {
	case server = <-accepted:
	case err = <-acceptErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out accepting Unix test connection")
	}
	t.Cleanup(func() { _ = server.Close() })
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestVerifySocketPeerUsesKernelCredentials(t *testing.T) {
	t.Parallel()
	server, _ := unixConnectionPair(t)

	pid, err := verifySocketPeer(server)

	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestExecuteCommandReportsLifecycle(t *testing.T) {
	t.Parallel()
	server, client := unixConnectionPair(t)
	spec := &Command{
		Executable: "true", LaunchID: "launch", Version: protocolVersion,
	}
	done := make(chan error, 1)
	go func() { done <- executeCommand(context.Background(), server, spec) }()
	decoder := json.NewDecoder(client)
	var started commandResult
	require.NoError(t, decoder.Decode(&started))
	assert.Equal(t, phaseStarted, started.Phase)
	assert.Positive(t, started.PID)
	var exited commandResult
	require.NoError(t, decoder.Decode(&exited))
	assert.Equal(t, phaseExited, exited.Phase)
	assert.Zero(t, exited.ExitCode)
	assert.Empty(t, exited.Error)
	require.NoError(t, <-done)
}

func TestExecuteCommandReportsChildFailure(t *testing.T) {
	t.Parallel()
	server, client := unixConnectionPair(t)
	spec := &Command{
		Executable: "false", LaunchID: "launch", Version: protocolVersion,
	}
	done := make(chan error, 1)
	go func() { done <- executeCommand(context.Background(), server, spec) }()
	decoder := json.NewDecoder(client)
	var started commandResult
	require.NoError(t, decoder.Decode(&started))
	assert.Equal(t, phaseStarted, started.Phase)
	var exited commandResult
	require.NoError(t, decoder.Decode(&exited))
	assert.Equal(t, phaseExited, exited.Phase)
	assert.NotZero(t, exited.ExitCode)
	assert.NotEmpty(t, exited.Error)
	require.NoError(t, <-done)
}

func TestExecuteCommandReportsMissingExecutable(t *testing.T) {
	t.Parallel()
	server, client := unixConnectionPair(t)
	spec := &Command{
		Executable: filepath.Join(t.TempDir(), "missing"), LaunchID: "launch", Version: protocolVersion,
	}
	done := make(chan error, 1)
	go func() { done <- executeCommand(context.Background(), server, spec) }()
	var result commandResult
	require.NoError(t, json.NewDecoder(client).Decode(&result))
	assert.Equal(t, phaseError, result.Phase)
	assert.NotEmpty(t, result.Error)
	require.ErrorContains(t, <-done, "find runtime executable")
}

func TestPrepareCommandRequiresExecutable(t *testing.T) {
	t.Parallel()

	_, err := prepareCommand(&Command{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executable is required")
}
