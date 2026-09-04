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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func formatExtensions(exts []string) []string {
	newExts := make([]string, 0)
	for _, v := range exts {
		newExt := strings.TrimSpace(v)
		if newExt == "" {
			continue
		}
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

// parseCustomControls builds a Controls map from TOML control definitions.
// Values are zapscript strings (e.g., "**input.keyboard:{f2}").
// Scripts are validated at load time; entries with invalid syntax are skipped.
func parseCustomControls(commands map[string]string) map[string]platforms.Control {
	if len(commands) == 0 {
		return nil
	}
	controls := make(map[string]platforms.Control, len(commands))
	for action, script := range commands {
		parser := zapscript.NewParser(script)
		_, err := parser.ParseScript()
		if err != nil {
			log.Warn().Err(err).Str("action", action).Str("script", script).
				Msg("skipping custom control: invalid zapscript syntax")
			continue
		}
		controls[action] = platforms.Control{Script: script}
	}
	if len(controls) == 0 {
		return nil
	}
	return controls
}

// ParseCustomLauncher converts common custom-launcher fields into a platform
// launcher. Native backends can add their own availability and launch functions.
func ParseCustomLauncher(pl platforms.Platform, v *config.LaunchersCustom) (platforms.Launcher, bool) {
	systemID := ""
	if v.System != "" {
		system, err := systemdefs.LookupSystem(v.System)
		if err != nil {
			log.Warn().Err(err).Str("launcherID", v.ID).Str("system", v.System).
				Msg("skipping custom launcher: system not found")
			return platforms.Launcher{}, false
		}
		systemID = system.ID
	}

	lifecycle := parseLifecycle(v.Lifecycle)
	launcherID := v.ID
	launcherGroups := v.Groups
	executeCmd := v.Execute
	exts := formatExtensions(v.FileExts)
	resolvedDirs := make([]string, len(v.MediaDirs))
	for i, dir := range v.MediaDirs {
		resolvedDirs[i] = ResolveRelativePath(dir)
	}

	launcher := platforms.Launcher{
		ID:            launcherID,
		SystemID:      systemID,
		Folders:       resolvedDirs,
		Extensions:    exts,
		Groups:        launcherGroups,
		Schemes:       v.Schemes,
		AllowListOnly: v.Restricted,
		Lifecycle:     lifecycle,
		Controls:      parseCustomControls(v.Controls),
		// An entry with no backend has nothing to run. Validation only lets
		// one through when it names a system and media dirs, so it exists to
		// widen that system's media and defers launching to a real launcher.
		ScanOnly: executeCmd == "" && v.Backend == "" && systemID != "",
	}

	if executeCmd != "" {
		launcher.Launch = func(
			cfg *config.Instance, path string, opts *platforms.LaunchOptions,
		) (*os.Process, error) {
			hostname, err := os.Hostname()
			if err != nil {
				log.Debug().Err(err).Msgf("error getting hostname, continuing")
			}
			defaults := cfg.LookupLauncherDefaults(launcherID, launcherGroups)
			action := ""
			if opts != nil && opts.Action != "" {
				action = opts.Action
			} else {
				action = defaults.Action
			}

			exprEnv := zapscript.CustomLauncherExprEnv{
				Platform: pl.ID(),
				Version:  config.AppVersion,
				Device: zapscript.ExprEnvDevice{
					Hostname: hostname,
					OS:       runtime.GOOS,
					Arch:     runtime.GOARCH,
				},
				MediaPath:  path,
				Action:     action,
				InstallDir: defaults.InstallDir,
				ServerURL:  defaults.ServerURL,
				SystemID:   systemID,
				LauncherID: launcherID,
			}

			parseReader := zapscript.NewParser(executeCmd)
			parsed, parseErr := parseReader.ParseExpressions()
			if parseErr != nil {
				return nil, fmt.Errorf("error parsing expressions: %w", parseErr)
			}
			evalReader := zapscript.NewParser(parsed)
			output, evalErr := evalReader.EvalExpressions(exprEnv)
			if evalErr != nil {
				return nil, fmt.Errorf("error evaluating execute expression: %w", evalErr)
			}
			parts, splitErr := SplitCommand(output)
			if splitErr != nil {
				return nil, fmt.Errorf("failed to parse execute command: %w", splitErr)
			}
			if len(parts) == 0 {
				return nil, errors.New("execute command is empty after parsing")
			}

			log.Debug().Str("launcherID", launcherID).Str("command", output).Strs("argv", parts).
				Msg("executing custom launcher")
			//nolint:gosec,noctx // User-configured launcher commands, managed via lifecycle
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Dir = ExeDir()
			envJSON, jsonErr := json.Marshal(exprEnv)
			if jsonErr != nil {
				log.Debug().Err(jsonErr).Msg("failed to marshal ZAPAROO_ENVIRONMENT")
			} else {
				cmd.Env = append(os.Environ(), "ZAPAROO_ENVIRONMENT="+string(envJSON))
			}
			if startErr := cmd.Start(); startErr != nil {
				log.Error().Err(startErr).Msgf("error running custom launcher: %s", output)
				return nil, fmt.Errorf("failed to start custom launcher command: %w", startErr)
			}
			return cmd.Process, nil
		}
	}

	log.Info().Str("launcherID", launcher.ID).Str("systemID", launcher.SystemID).
		Strs("folders", launcher.Folders).Strs("extensions", launcher.Extensions).
		Int("lifecycle", int(launcher.Lifecycle)).Msg("registered custom launcher")
	return launcher, true
}

// ParseCustomLaunchers converts legacy and command-backed custom launchers.
// Native backends and virtual systems are compiled by their owning platform.
func ParseCustomLaunchers(
	pl platforms.Platform,
	customLaunchers []config.LaunchersCustom,
) []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0, len(customLaunchers))
	skipped := 0
	for i := range customLaunchers {
		entry := &customLaunchers[i]
		if entry.Backend == config.CustomLauncherBackendMisterCore {
			continue
		}
		launcher, ok := ParseCustomLauncher(pl, entry)
		if !ok {
			skipped++
			continue
		}
		launchers = append(launchers, launcher)
	}
	if skipped > 0 {
		log.Warn().Int("skipped", skipped).Int("parsed", len(launchers)).
			Msg("some custom launchers were skipped due to errors")
	}
	return launchers
}

// CombineLaunchers assembles the launcher list a platform reports from its
// built-in launchers and the user's custom ones. Every platform returns through
// this, so the launcher cache and the code reading pl.Launchers directly never
// see different lists.
func CombineLaunchers(
	cfg *config.Instance,
	pl platforms.Platform,
	builtIn []platforms.Launcher,
) []platforms.Launcher {
	return CombineParsedLaunchers(ParseCustomLaunchers(pl, cfg.CustomLaunchers()), builtIn)
}

// CombineParsedLaunchers is CombineLaunchers for platforms that already parsed
// their custom launchers, because they feed them to the emulator integrations
// as the set of IDs those must not redefine.
func CombineParsedLaunchers(custom, builtIn []platforms.Launcher) []platforms.Launcher {
	custom = FilterConflictingLaunchers(custom, builtIn)
	combined := make([]platforms.Launcher, 0, len(custom)+len(builtIn))
	combined = append(combined, custom...)
	combined = append(combined, builtIn...)
	return combined
}

// FilterConflictingLaunchers drops custom launchers whose ID matches a built-in
// one. Launcher IDs are the handle for launch overrides, controls and the
// launcher advanced argument, so a duplicate makes those resolve to whichever
// entry happens to come first, and shadows a launcher that can actually launch
// with one that usually cannot.
func FilterConflictingLaunchers(custom, builtIn []platforms.Launcher) []platforms.Launcher {
	if len(custom) == 0 || len(builtIn) == 0 {
		return custom
	}

	taken := make(map[string]string, len(builtIn))
	for i := range builtIn {
		taken[strings.ToLower(builtIn[i].ID)] = builtIn[i].ID
	}

	kept := make([]platforms.Launcher, 0, len(custom))
	for i := range custom {
		if existing, conflict := taken[strings.ToLower(custom[i].ID)]; conflict {
			log.Error().Str("id", custom[i].ID).Str("existingLauncher", existing).
				Msg("ignoring custom launcher: id is already used by a built-in launcher")
			continue
		}
		kept = append(kept, custom[i])
	}
	return kept
}
