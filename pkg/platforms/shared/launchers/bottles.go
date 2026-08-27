//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

const (
	maxBottlesOutputSize  = 16 << 20
	maxBottles            = 256
	maxBottlesPrograms    = 100_000
	maxBottlesFieldLength = 4096
	bottlesCommandTimeout = 10 * time.Second
)

type bottlesProgram struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

type bottlesTarget struct {
	Bottle    string `json:"bottle"`
	ProgramID string `json:"programId,omitempty"`
	Program   string `json:"program,omitempty"`
}

type bottlesOutputFunc func(context.Context, *platforms.LaunchCommand, int64) ([]byte, error)

type bottlesStartFunc func(*platforms.LaunchCommand) (*os.Process, error)

//nolint:govet // Test hooks keep resolver and command output configuration grouped.
type bottlesOptions struct {
	resolver  applicationResolverOptions
	launchEnv func() []string
	output    bottlesOutputFunc
	start     bottlesStartFunc
}

// NewBottlesLauncher creates a launcher for programs managed by Bottles.
func NewBottlesLauncher() platforms.Launcher {
	return buildBottlesLauncher(bottlesOptions{
		resolver: applicationResolverOptions{checkFlatpak: true},
		output:   outputApplicationCommand,
	})
}

func buildBottlesLauncher(options bottlesOptions) platforms.Launcher {
	if options.output == nil {
		options.output = outputApplicationCommand
	}
	if options.start == nil {
		options.start = startTrackedApplicationCommand
	}
	resolve := newApplicationResolver("bottles-cli", FlatpakBottlesID, options.resolver)
	resolveCLI := func() (applicationInstallation, error) {
		installation, err := resolve()
		if err != nil {
			return applicationInstallation{}, err
		}
		if installation.flatpak {
			installation.argsPrefix = []string{
				"run", "--die-with-parent", "--command=bottles-cli", FlatpakBottlesID,
			}
		}
		return installation, nil
	}
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		target, err := parseBottlesTarget(path)
		if err != nil {
			return nil, err
		}
		installation, err := resolveCLI()
		if err != nil {
			return nil, err
		}
		args := []string{"run", "-b", target.Bottle}
		if target.ProgramID != "" {
			args = append(args, "--program-id", target.ProgramID)
		} else {
			args = append(args, "-p", target.Program)
		}
		return buildApplicationCommand(installation, args, options.launchEnv), nil
	}

	return platforms.Launcher{
		ID: "Bottles", SystemID: systemdefs.SystemPC, Schemes: []string{shared.SchemeBottles},
		Lifecycle: platforms.LifecycleBlocking,
		Availability: func(*config.Instance) error {
			_, err := resolveCLI()
			return err
		},
		BuildLaunchCommand: func(
			_ *config.Instance, path string, _ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return buildCommand(path)
		},
		Scanner: func(
			ctx context.Context,
			_ *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			installation, err := resolveCLI()
			if err != nil {
				return results, nil //nolint:nilerr // Optional integration contributes no media when absent.
			}
			items, err := scanBottlesPrograms(ctx, installation, options)
			if err != nil {
				log.Warn().Err(err).Msg("failed to scan Bottles programs")
				return results, nil
			}
			return append(results, items...), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			process, err := options.start(command)
			if err != nil {
				return nil, fmt.Errorf("start Bottles program: %w", err)
			}
			return process, nil
		},
	}
}

func scanBottlesPrograms(
	ctx context.Context,
	installation applicationInstallation,
	options bottlesOptions,
) ([]platforms.ScanResult, error) {
	listCtx, cancel := context.WithTimeout(ctx, bottlesCommandTimeout)
	listCommand := buildApplicationCommand(installation, []string{"--json", "list", "bottles"}, options.launchEnv)
	output, err := options.output(listCtx, listCommand, maxBottlesOutputSize)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("list Bottles bottles: %w", err)
	}
	bottles, err := parseBottlesList(output)
	if err != nil {
		return nil, err
	}
	results := make([]platforms.ScanResult, 0)
	for _, bottle := range bottles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		programCtx, programCancel := context.WithTimeout(ctx, bottlesCommandTimeout)
		command := buildApplicationCommand(
			installation, []string{"--json", "programs", "-b", bottle}, options.launchEnv,
		)
		programOutput, outputErr := options.output(programCtx, command, maxBottlesOutputSize)
		programCancel()
		if outputErr != nil {
			log.Warn().Err(outputErr).Str("bottle", bottle).Msg("failed to list Bottles programs")
			continue
		}
		programs, parseErr := parseBottlesPrograms(bottle, programOutput)
		if parseErr != nil {
			log.Warn().Err(parseErr).Str("bottle", bottle).Msg("failed to parse Bottles programs")
			continue
		}
		if len(results)+len(programs) > maxBottlesPrograms {
			return nil, errors.New("bottles program library exceeds entry limit")
		}
		results = append(results, programs...)
	}
	return results, nil
}

func parseBottlesList(data []byte) ([]string, error) {
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		var bottles map[string]json.RawMessage
		if mapErr := json.Unmarshal(data, &bottles); mapErr != nil {
			return nil, fmt.Errorf("parse Bottles bottle list: %w", err)
		}
		for name := range bottles {
			names = append(names, name)
		}
	}
	if len(names) > maxBottles {
		return nil, errors.New("bottles library exceeds bottle limit")
	}
	filtered := names[:0]
	for _, name := range names {
		name = strings.TrimSpace(name)
		if validApplicationField(name, maxBottlesFieldLength) {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return filtered, nil
}

func parseBottlesPrograms(bottle string, data []byte) ([]platforms.ScanResult, error) {
	var programs []bottlesProgram
	if err := json.Unmarshal(data, &programs); err != nil {
		return nil, fmt.Errorf("parse Bottles programs: %w", err)
	}
	if len(programs) > maxBottlesPrograms {
		return nil, errors.New("bottles program library exceeds entry limit")
	}
	results := make([]platforms.ScanResult, 0, len(programs))
	for _, program := range programs {
		name := strings.TrimSpace(program.Name)
		programID := strings.TrimSpace(program.ID)
		if program.Removed || !validApplicationField(name, maxBottlesFieldLength) ||
			(programID != "" && !validApplicationField(programID, maxBottlesFieldLength)) {
			continue
		}
		target := bottlesTarget{Bottle: bottle, ProgramID: programID, Program: name}
		encoded, err := json.Marshal(target)
		if err != nil {
			continue
		}
		results = append(results, platforms.ScanResult{
			Name: name, Path: virtualpath.CreateVirtualPath(shared.SchemeBottles, string(encoded), name), NoExt: true,
		})
	}
	return results, nil
}

func parseBottlesTarget(path string) (bottlesTarget, error) {
	id, err := virtualpath.ExtractSchemeID(path, shared.SchemeBottles)
	if err != nil {
		return bottlesTarget{}, fmt.Errorf("extract Bottles target: %w", err)
	}
	if len(id) > maxBottlesFieldLength*3 {
		return bottlesTarget{}, errors.New("bottles target exceeds size limit")
	}
	var target bottlesTarget
	if err := json.Unmarshal([]byte(id), &target); err != nil {
		return bottlesTarget{}, fmt.Errorf("parse Bottles target: %w", err)
	}
	target.Bottle = strings.TrimSpace(target.Bottle)
	target.ProgramID = strings.TrimSpace(target.ProgramID)
	target.Program = strings.TrimSpace(target.Program)
	if !validApplicationField(target.Bottle, maxBottlesFieldLength) ||
		(target.ProgramID == "" && !validApplicationField(target.Program, maxBottlesFieldLength)) ||
		(target.ProgramID != "" && !validApplicationField(target.ProgramID, maxBottlesFieldLength)) {
		return bottlesTarget{}, errors.New("invalid Bottles target")
	}
	return target, nil
}
