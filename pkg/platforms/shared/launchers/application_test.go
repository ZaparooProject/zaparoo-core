//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputApplicationCommand(t *testing.T) {
	t.Parallel()

	shell, err := exec.LookPath("sh")
	require.NoError(t, err)

	t.Run("captures output and merges environment", func(t *testing.T) {
		t.Parallel()

		output, outputErr := outputApplicationCommand(t.Context(), &platforms.LaunchCommand{
			Executable: shell,
			Args:       []string{"-c", `printf %s "$ZAPAROO_APPLICATION_TEST"`},
			Env:        []string{"ZAPAROO_APPLICATION_TEST=expected"},
		}, 64)
		require.NoError(t, outputErr)
		assert.Equal(t, "expected", string(output))
	})

	t.Run("rejects oversized output", func(t *testing.T) {
		t.Parallel()

		_, outputErr := outputApplicationCommand(t.Context(), &platforms.LaunchCommand{
			Executable: shell, Args: []string{"-c", "printf 12345"},
		}, 4)
		require.ErrorContains(t, outputErr, "application output exceeds size limit")
	})

	t.Run("reports command failure", func(t *testing.T) {
		t.Parallel()

		_, outputErr := outputApplicationCommand(t.Context(), &platforms.LaunchCommand{
			Executable: shell, Args: []string{"-c", "exit 7"},
		}, 64)
		require.ErrorContains(t, outputErr, "wait for application command")
	})

	t.Run("rejects empty command", func(t *testing.T) {
		t.Parallel()

		_, outputErr := outputApplicationCommand(t.Context(), nil, 64)
		require.ErrorContains(t, outputErr, "application command is empty")
	})
}

func TestStartTrackedApplicationCommand(t *testing.T) {
	t.Parallel()

	shell, err := exec.LookPath("sh")
	require.NoError(t, err)
	process, err := startTrackedApplicationCommand(&platforms.LaunchCommand{
		Executable: shell, Args: []string{"-c", "exit 0"},
	})
	require.NoError(t, err)
	state, err := process.Wait()
	require.NoError(t, err)
	assert.True(t, state.Success())

	_, err = startTrackedApplicationCommand(nil)
	require.ErrorContains(t, err, "application command is empty")
	_, err = startTrackedApplicationCommand(&platforms.LaunchCommand{Executable: "/missing/zaparoo-command"})
	require.ErrorContains(t, err, "start application command")
}

func TestApplicationResolverRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	t.Run("native", func(t *testing.T) {
		t.Parallel()

		calls := 0
		resolve := newApplicationResolver("example", "com.example.App", applicationResolverOptions{
			lookPath: func(name string) (string, error) {
				calls++
				if calls == 1 {
					return "", errors.New("temporarily unavailable")
				}
				return "/usr/bin/" + name, nil
			},
		})

		_, err := resolve()
		require.Error(t, err)
		installation, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, applicationInstallation{executable: "/usr/bin/example"}, installation)
		cached, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, installation, cached)
		assert.Equal(t, 2, calls)
	})

	t.Run("flatpak", func(t *testing.T) {
		t.Parallel()

		flatpakCalls := 0
		resolve := newApplicationResolver("example", "com.example.App", applicationResolverOptions{
			checkFlatpak:       true,
			isFlatpakInstalled: func(string) bool { return true },
			lookPath: func(name string) (string, error) {
				if name != "flatpak" {
					return "", errors.New("not installed")
				}
				flatpakCalls++
				if flatpakCalls == 1 {
					return "", errors.New("temporarily unavailable")
				}
				return "/usr/bin/flatpak", nil
			},
		})

		_, err := resolve()
		require.Error(t, err)
		installation, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, applicationInstallation{
			executable: "/usr/bin/flatpak",
			argsPrefix: []string{"run", "com.example.App"},
			flatpak:    true,
		}, installation)
		_, err = resolve()
		require.NoError(t, err)
		assert.Equal(t, 2, flatpakCalls)
	})
}
