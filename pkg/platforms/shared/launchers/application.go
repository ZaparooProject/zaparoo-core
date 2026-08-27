//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
)

type applicationInstallation struct {
	executable string
	argsPrefix []string
	flatpak    bool
}

type applicationResolverOptions struct {
	lookPath           func(string) (string, error)
	isFlatpakInstalled func(string) bool
	checkFlatpak       bool
}

func newApplicationResolver(
	nativeExecutable, flatpakID string,
	options applicationResolverOptions,
) func() (applicationInstallation, error) {
	if options.lookPath == nil {
		options.lookPath = exec.LookPath
	}
	if options.isFlatpakInstalled == nil {
		options.isFlatpakInstalled = IsFlatpakInstalled
	}

	var once sync.Once
	var installation applicationInstallation
	var resolveErr error
	return func() (applicationInstallation, error) {
		once.Do(func() {
			if path, err := options.lookPath(nativeExecutable); err == nil {
				installation = applicationInstallation{executable: path}
				return
			}
			if options.checkFlatpak && options.isFlatpakInstalled(flatpakID) {
				flatpakPath, err := options.lookPath("flatpak")
				if err != nil {
					resolveErr = fmt.Errorf("resolve flatpak executable: %w", err)
					return
				}
				installation = applicationInstallation{
					executable: flatpakPath,
					argsPrefix: []string{"run", flatpakID},
					flatpak:    true,
				}
				return
			}
			resolveErr = fmt.Errorf("%s is not installed", nativeExecutable)
		})
		return installation, resolveErr
	}
}

func withFlatpakDieWithParent(installation applicationInstallation) applicationInstallation {
	if !installation.flatpak || len(installation.argsPrefix) == 0 || installation.argsPrefix[0] != "run" {
		return installation
	}
	argsPrefix := make([]string, 0, len(installation.argsPrefix)+1)
	argsPrefix = append(argsPrefix, "run", "--die-with-parent")
	argsPrefix = append(argsPrefix, installation.argsPrefix[1:]...)
	installation.argsPrefix = argsPrefix
	return installation
}

func buildApplicationCommand(
	installation applicationInstallation,
	args []string,
	launchEnv func() []string,
) *platforms.LaunchCommand {
	commandArgs := make([]string, 0, len(installation.argsPrefix)+len(args))
	commandArgs = append(commandArgs, installation.argsPrefix...)
	commandArgs = append(commandArgs, args...)
	if launchEnv == nil {
		launchEnv = func() []string { return linuxbase.DesktopSessionEnvOverrides(nil) }
	}
	return &platforms.LaunchCommand{
		Executable: installation.executable,
		Args:       commandArgs,
		Env:        launchEnv(),
	}
}

func readLimitedApplicationFile(path string, maxSize int64) ([]byte, error) {
	//nolint:gosec // Caller supplies a discovered per-user application path.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open application file: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file.
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read application file: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("application file exceeds size limit")
	}
	return data, nil
}

func outputApplicationCommand(
	ctx context.Context,
	command *platforms.LaunchCommand,
	maxSize int64,
) ([]byte, error) {
	if command == nil || command.Executable == "" {
		return nil, errors.New("application command is empty")
	}
	//nolint:gosec // Executable and fixed prefixes come from verified built-in application discovery.
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Env = helpers.MergeEnviron(os.Environ(), command.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open application stdout: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start application command: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxSize+1))
	if int64(len(data)) > maxSize {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("read application output: %w", readErr)
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("application output exceeds size limit")
	}
	if waitErr != nil {
		return nil, fmt.Errorf("wait for application command: %w", waitErr)
	}
	return data, nil
}

func startTrackedApplicationCommand(command *platforms.LaunchCommand) (*os.Process, error) {
	if command == nil || command.Executable == "" {
		return nil, errors.New("application command is empty")
	}
	//nolint:gosec // Executable and fixed prefixes come from verified built-in application discovery.
	cmd := exec.CommandContext(context.Background(), command.Executable, command.Args...)
	cmd.Env = helpers.MergeEnviron(os.Environ(), command.Env)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start application command: %w", err)
	}
	return cmd.Process, nil
}
