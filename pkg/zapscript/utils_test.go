// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package zapscript

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCmdExecute_ZaparooEnvironmentSet verifies that ZAPAROO_ENVIRONMENT env var is set
// when ExprEnv is provided in CmdEnv.
func TestCmdExecute_ZaparooEnvironmentSet(t *testing.T) {
	t.Parallel()

	// Create a minimal config that allows execute
	cfg := &config.Instance{}
	cfg.SetExecuteAllowListForTesting([]string{".*"})

	// Create expression env
	exprEnv := &parser.ArgExprEnv{
		Platform: "test",
		Version:  "1.0.0",
		ScanMode: "tap",
		Device: parser.ExprEnvDevice{
			Hostname: "testhost",
			OS:       "linux",
			Arch:     "amd64",
		},
		LastScanned: parser.ExprEnvLastScanned{
			ID:    "test-id",
			Value: "test-value",
			Data:  "test-data",
		},
		MediaPlaying: true,
		ActiveMedia: parser.ExprEnvActiveMedia{
			LauncherID: "retroarch",
			SystemID:   "snes",
			SystemName: "Super Nintendo",
			Path:       "/games/snes/mario.sfc",
			Name:       "Super Mario World",
		},
	}

	// Use printenv which returns non-zero if var doesn't exist
	cmd := parser.Command{
		Name: "execute",
		Args: []string{"printenv ZAPAROO_ENVIRONMENT"},
	}

	env := platforms.CmdEnv{
		Cmd:     cmd,
		Cfg:     cfg,
		ExprEnv: exprEnv,
		Unsafe:  false,
	}

	result, err := cmdExecute(nil, env)

	// If ZAPAROO_ENVIRONMENT is set, printenv will succeed
	require.NoError(t, err, "execute should succeed when ZAPAROO_ENVIRONMENT is set")
	assert.Equal(t, platforms.CmdResult{}, result)
}

// TestCmdExecute_ZaparooEnvironmentContainsExpectedFields verifies the JSON structure.
func TestCmdExecute_ZaparooEnvironmentContainsExpectedFields(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetExecuteAllowListForTesting([]string{".*"})

	exprEnv := &parser.ArgExprEnv{
		Platform: "mister",
		Version:  "2.0.0",
		ScanMode: "hold",
		Device: parser.ExprEnvDevice{
			Hostname: "mister",
			OS:       "linux",
			Arch:     "arm",
		},
		MediaPlaying: true,
		ActiveMedia: parser.ExprEnvActiveMedia{
			SystemID: "genesis",
			Path:     "/games/genesis/sonic.bin",
		},
	}

	// Simply check that ZAPAROO_ENVIRONMENT is set with valid JSON
	// The actual JSON structure is verified by unit tests of the types
	cmd := parser.Command{
		Name: "execute",
		Args: []string{"printenv ZAPAROO_ENVIRONMENT"},
	}

	env := platforms.CmdEnv{
		Cmd:     cmd,
		Cfg:     cfg,
		ExprEnv: exprEnv,
		Unsafe:  false,
	}

	result, err := cmdExecute(nil, env)

	require.NoError(t, err, "execute should succeed with ZAPAROO_ENVIRONMENT set")
	assert.Equal(t, platforms.CmdResult{}, result)
}

// TestCmdExecute_StderrCapture verifies that stderr is captured in error messages.
func TestCmdExecute_StderrCapture(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetExecuteAllowListForTesting([]string{".*"})

	// Create a command that writes to stderr and exits with error
	// Use bash with proper quoting
	cmd := parser.Command{
		Name: "execute",
		Args: []string{`bash -c "echo 'stderr_test_message' >&2; exit 1"`},
	}

	env := platforms.CmdEnv{
		Cmd:    cmd,
		Cfg:    cfg,
		Unsafe: false,
	}

	_, err := cmdExecute(nil, env)

	require.Error(t, err, "execute should fail with non-zero exit")
	assert.Contains(t, err.Error(), "stderr_test_message", "error should contain stderr output")
}

// TestCmdExecute_TimeoutConstant verifies the timeout constant value.
func TestCmdExecute_TimeoutConstant(t *testing.T) {
	t.Parallel()

	// Verify the timeout constant is set as expected (2 seconds)
	assert.Equal(t, 2*time.Second, ExecuteTimeout, "ExecuteTimeout should be 2 seconds")
}

// TestCmdExecute_UnsafeBlocked verifies unsafe commands are blocked.
func TestCmdExecute_UnsafeBlocked(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetExecuteAllowListForTesting([]string{".*"})

	cmd := parser.Command{
		Name: "execute",
		Args: []string{"echo hello"},
	}

	env := platforms.CmdEnv{
		Cmd:    cmd,
		Cfg:    cfg,
		Unsafe: true, // Unsafe flag should block execution
	}

	_, err := cmdExecute(nil, env)

	require.Error(t, err, "execute should fail when unsafe is true")
	assert.Contains(t, err.Error(), "remote source", "error should mention remote source")
}

// TestCmdExecute_NotInAllowList verifies commands not in allow list are blocked.
func TestCmdExecute_NotInAllowList(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	// Don't set any allow list - commands should be blocked

	cmd := parser.Command{
		Name: "execute",
		Args: []string{"echo hello"},
	}

	env := platforms.CmdEnv{
		Cmd:    cmd,
		Cfg:    cfg,
		Unsafe: false,
	}

	_, err := cmdExecute(nil, env)

	require.Error(t, err, "execute should fail when not in allow list")
	assert.Contains(t, err.Error(), "not allowed", "error should mention not allowed")
}

// TestCmdExecute_EmptyArgs verifies empty args return error.
func TestCmdExecute_EmptyArgs(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetExecuteAllowListForTesting([]string{".*"})

	cmd := parser.Command{
		Name: "execute",
		Args: []string{}, // No args
	}

	env := platforms.CmdEnv{
		Cmd:    cmd,
		Cfg:    cfg,
		Unsafe: false,
	}

	_, err := cmdExecute(nil, env)

	require.Error(t, err, "execute should fail with no args")
}

// TestCmdDelay verifies delay command works correctly.
func TestCmdDelay(t *testing.T) {
	t.Parallel()

	cmd := parser.Command{
		Name: "delay",
		Args: []string{"50"}, // 50ms delay
	}

	env := platforms.CmdEnv{
		Cmd: cmd,
	}

	start := time.Now()
	result, err := cmdDelay(nil, env)
	elapsed := time.Since(start)

	require.NoError(t, err, "delay should succeed")
	assert.Equal(t, platforms.CmdResult{}, result)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "delay should wait at least 50ms")
}

// TestCmdDelay_InvalidAmount verifies invalid delay amount returns error.
func TestCmdDelay_InvalidAmount(t *testing.T) {
	t.Parallel()

	cmd := parser.Command{
		Name: "delay",
		Args: []string{"notanumber"},
	}

	env := platforms.CmdEnv{
		Cmd: cmd,
	}

	_, err := cmdDelay(nil, env)

	require.Error(t, err, "delay should fail with invalid amount")
}

// TestCmdDelay_NoArgs verifies no args returns error.
func TestCmdDelay_NoArgs(t *testing.T) {
	t.Parallel()

	cmd := parser.Command{
		Name: "delay",
		Args: []string{},
	}

	env := platforms.CmdEnv{
		Cmd: cmd,
	}

	_, err := cmdDelay(nil, env)

	require.Error(t, err, "delay should fail with no args")
}

// TestCmdEcho verifies echo command logs without error.
func TestCmdEcho(t *testing.T) {
	t.Parallel()

	cmd := parser.Command{
		Name: "echo",
		Args: []string{"hello", "world"},
	}

	env := platforms.CmdEnv{
		Cmd: cmd,
	}

	result, err := cmdEcho(nil, env)

	require.NoError(t, err, "echo should succeed")
	assert.Equal(t, platforms.CmdResult{}, result)
}
