// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package helpers

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/rs/zerolog/log"
)

func formatExtensions(exts []string) []string {
	newExts := make([]string, 0)
	for _, v := range exts {
		if v == "" {
			continue
		}
		newExt := strings.TrimSpace(v)
		if newExt[0] != '.' {
			newExt = "." + newExt
		}
		newExt = strings.ToLower(newExt)
		newExts = append(newExts, newExt)
	}
	return newExts
}

func parseLifecycle(lifecycle string) platforms.LauncherLifecycle {
	if strings.EqualFold(lifecycle, "background") {
		return platforms.LifecycleFireAndForget
	}
	return platforms.LifecycleBlocking
}

func ParseCustomLaunchers(
	pl platforms.Platform,
	customLaunchers []config.LaunchersCustom,
) []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0)
	for i := range customLaunchers {
		v := &customLaunchers[i]

		systemID := ""
		if v.System != "" {
			system, err := systemdefs.LookupSystem(v.System)
			if err != nil {
				log.Err(err).Msgf("custom launcher %s: system not found: %s", v.ID, v.System)
				continue
			}
			systemID = system.ID
		}

		lifecycle := parseLifecycle(v.Lifecycle)

		// Capture values for the closure
		launcherID := v.ID
		launcherSystemID := systemID
		launcherGroups := v.Groups
		executeCmd := v.Execute

		launchers = append(launchers, platforms.Launcher{
			ID:            launcherID,
			SystemID:      launcherSystemID,
			Folders:       v.MediaDirs,
			Extensions:    formatExtensions(v.FileExts),
			Groups:        launcherGroups,
			Schemes:       v.Schemes,
			AllowListOnly: v.Restricted,
			Lifecycle:     lifecycle,
			Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
				hostname, err := os.Hostname()
				if err != nil {
					log.Debug().Err(err).Msgf("error getting hostname, continuing")
				}

				// Get config defaults for this launcher
				defaults := cfg.LookupLauncherDefaults(launcherID, launcherGroups)

				// Resolve action from opts or defaults
				action := ""
				if opts != nil && opts.Action != "" {
					action = opts.Action
				} else {
					action = defaults.Action
				}

				exprEnv := parser.CustomLauncherExprEnv{
					Platform: pl.ID(),
					Version:  config.AppVersion,
					Device: parser.ExprEnvDevice{
						Hostname: hostname,
						OS:       runtime.GOOS,
						Arch:     runtime.GOARCH,
					},
					MediaPath:  path,
					Action:     action,
					InstallDir: defaults.InstallDir,
					ServerURL:  defaults.ServerURL,
					SystemID:   launcherSystemID,
					LauncherID: launcherID,
				}

				parseReader := parser.NewParser(executeCmd)
				parsed, err := parseReader.ParseExpressions()
				if err != nil {
					return nil, fmt.Errorf("error parsing expressions: %w", err)
				}

				evalReader := parser.NewParser(parsed)
				output, err := evalReader.EvalExpressions(exprEnv)
				if err != nil {
					return nil, fmt.Errorf("error evaluating execute expression: %w", err)
				}

				var cmd *exec.Cmd

				if runtime.GOOS == "windows" {
					//nolint:gosec,noctx // User-configured launcher commands, managed via lifecycle
					cmd = exec.Command("cmd", "/c", output)
				} else {
					//nolint:gosec,noctx // User-configured launcher commands, managed via lifecycle
					cmd = exec.Command("sh", "-c", output)
				}

				// Pass ZAPAROO_ENVIRONMENT JSON env var
				envJSON, jsonErr := json.Marshal(exprEnv)
				if jsonErr != nil {
					log.Debug().Err(jsonErr).Msg("failed to marshal ZAPAROO_ENVIRONMENT")
				} else {
					cmd.Env = append(os.Environ(), "ZAPAROO_ENVIRONMENT="+string(envJSON))
				}

				if err = cmd.Start(); err != nil {
					log.Error().Err(err).Msgf("error running custom launcher: %s", output)
					return nil, fmt.Errorf("failed to start custom launcher command: %w", err)
				}

				// Custom launchers can be tracked - return process for lifecycle management
				return cmd.Process, nil
			},
		})
	}
	return launchers
}
