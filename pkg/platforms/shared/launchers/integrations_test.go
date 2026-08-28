//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func flatpakOnlyResolver(appID string) applicationResolverOptions {
	return applicationResolverOptions{
		checkFlatpak: true,
		lookPath: func(name string) (string, error) {
			if name == "flatpak" {
				return "/usr/bin/flatpak", nil
			}
			return "", os.ErrNotExist
		},
		isFlatpakInstalled: func(id string) bool { return id == appID },
	}
}

func TestFaugusScannerAndCommand(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	library := filepath.Join(
		home, ".var", "app", FlatpakFaugusID, "data", "faugus-launcher", "games.json",
	)
	require.NoError(t, os.MkdirAll(filepath.Dir(library), 0o750))
	require.NoError(t, os.WriteFile(library, []byte(
		`[{"gameid":"game-1","title":"Game One"},{"gameid":"","title":"Invalid"}]`,
	), 0o600))
	launcher := buildFaugusLauncher(faugusOptions{
		homeDir: home, resolver: flatpakOnlyResolver(FlatpakFaugusID),
		launchEnv: func() []string { return []string{"DISPLAY=:1"} },
	})
	results, err := launcher.Scanner(context.Background(), nil, "", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Game One", results[0].Name)

	command, err := launcher.BuildLaunchCommand(nil, results[0].Path, nil)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/flatpak", command.Executable)
	assert.Equal(t, []string{
		"run", "--die-with-parent", FlatpakFaugusID, "--game", "game-1",
	}, command.Args)
	assert.Equal(t, []string{"DISPLAY=:1"}, command.Env)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
}

func TestMoonlightTargetsAndCommand(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.moonlight")
	require.NoError(t, os.WriteFile(path, []byte(`{"host":"gaming-pc","app":"Game One"}`), 0o600))
	launcher := buildMoonlightLauncher(moonlightOptions{
		resolver:  flatpakOnlyResolver(FlatpakMoonlightID),
		launchEnv: func() []string { return []string{"WAYLAND_DISPLAY=wayland-1"} },
	})
	command, err := launcher.BuildLaunchCommand(nil, path, nil)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/flatpak", command.Executable)
	assert.Equal(t, []string{
		"run", "--die-with-parent", FlatpakMoonlightID, "stream", "gaming-pc", "Game One",
	}, command.Args)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)

	linePath := filepath.Join(t.TempDir(), "game.moonlight")
	require.NoError(t, os.WriteFile(linePath, []byte("gaming-pc\nGame Two\n"), 0o600))
	target, err := readMoonlightTarget(linePath)
	require.NoError(t, err)
	assert.Equal(t, moonlightTarget{Host: "gaming-pc", App: "Game Two"}, target)
}

func TestMoonlightRejectsOptionInjection(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.moonlight")
	require.NoError(t, os.WriteFile(path, []byte("--help\nGame\n"), 0o600))
	_, err := readMoonlightTarget(path)
	require.Error(t, err)
}

func TestBottlesScannerAndCommand(t *testing.T) {
	t.Parallel()

	var commands [][]string
	output := func(
		_ context.Context,
		command *platforms.LaunchCommand,
		_ int64,
	) ([]byte, error) {
		commands = append(commands, append([]string(nil), command.Args...))
		if command.Args[len(command.Args)-2] == "list" {
			return []byte(`{"Gaming":{}}`), nil
		}
		return []byte(`[{"id":"program-1","name":"Game One","removed":false}]`), nil
	}
	launcher := buildBottlesLauncher(bottlesOptions{
		resolver: flatpakOnlyResolver(FlatpakBottlesID), output: output,
		launchEnv: func() []string { return []string{"DISPLAY=:1"} },
	})
	results, err := launcher.Scanner(context.Background(), nil, "", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Game One", results[0].Name)
	require.Len(t, commands, 2)
	assert.Equal(t, []string{
		"run", "--die-with-parent", "--command=bottles-cli", FlatpakBottlesID,
		"--json", "list", "bottles",
	}, commands[0])

	command, err := launcher.BuildLaunchCommand(nil, results[0].Path, nil)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/flatpak", command.Executable)
	assert.Equal(t, []string{
		"run", "--die-with-parent", "--command=bottles-cli", FlatpakBottlesID,
		"run", "-b", "Gaming", "--program-id", "program-1",
	}, command.Args)
}

func TestBottlesUsesBlockingFlatpakProcess(t *testing.T) {
	t.Parallel()

	var started *platforms.LaunchCommand
	launcher := buildBottlesLauncher(bottlesOptions{
		resolver: flatpakOnlyResolver(FlatpakBottlesID),
		start: func(command *platforms.LaunchCommand) (*os.Process, error) {
			started = command
			return new(os.Process), nil
		},
	})
	results, err := parseBottlesPrograms("Gaming", []byte(`[
		{"id":"program-1","name":"Game One","removed":false}
	]`))
	require.NoError(t, err)
	require.Len(t, results, 1)

	process, err := launcher.Launch(nil, results[0].Path, nil)
	require.NoError(t, err)
	assert.NotNil(t, process)
	require.NotNil(t, started)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	assert.Nil(t, launcher.Kill)
	assert.Equal(t, []string{
		"run", "--die-with-parent", "--command=bottles-cli", FlatpakBottlesID,
		"run", "-b", "Gaming", "--program-id", "program-1",
	}, started.Args)
}

func TestParseBottlesListFiltersInvalidNames(t *testing.T) {
	t.Parallel()

	names, err := parseBottlesList([]byte(`["Gaming","--invalid",""]`))
	require.NoError(t, err)
	assert.Equal(t, []string{"Gaming"}, names)
}
