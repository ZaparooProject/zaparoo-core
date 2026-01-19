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

package platforms

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// LaunchParams contains all dependencies required for launching media.
type LaunchParams struct {
	Platform       Platform
	Config         *config.Instance
	SetActiveMedia func(*models.ActiveMedia)
	Launcher       *Launcher
	DB             *database.Database
	Options        *LaunchOptions
	Path           string
}

// ResolveAction returns the effective action for a launch, checking:
// 1. Explicit action from LaunchOptions (from advargs)
// 2. Config default for the launcher
// 3. Empty string (default "run" behavior)
func ResolveAction(opts *LaunchOptions, cfg *config.Instance, launcher *Launcher) string {
	log.Debug().
		Str("launcherID", launcher.ID).
		Bool("optsNil", opts == nil).
		Bool("cfgNil", cfg == nil).
		Msg("ResolveAction called")

	if opts != nil && opts.Action != "" {
		log.Debug().
			Str("action", opts.Action).
			Msg("ResolveAction: using explicit action from LaunchOptions")
		return opts.Action
	}
	if cfg != nil && launcher.ID != "" {
		def := cfg.LookupLauncherDefaults(launcher.ID, launcher.Groups)
		if def.Action != "" {
			log.Debug().
				Str("action", def.Action).
				Str("launcherID", launcher.ID).
				Msg("ResolveAction: using config default action")
			return def.Action
		}
		log.Debug().
			Str("launcherID", launcher.ID).
			Msg("ResolveAction: no config default found for launcher")
	}
	log.Debug().Msg("ResolveAction: returning empty (default run behavior)")
	return ""
}

// IsActionDetails returns true if action is "details" (case-insensitive).
func IsActionDetails(action string) bool {
	return strings.EqualFold(action, zapscript.ActionDetails)
}

// DoLaunch launches the given path and updates the active media with it if
// it was successful. The getDisplayName callback extracts a display name from the path.
func DoLaunch(params *LaunchParams, getDisplayName func(string) string) error {
	log.Debug().Msgf("launching with: %v", params.Launcher)

	action := ResolveAction(params.Options, params.Config, params.Launcher)

	// Populate resolved action into Options for launcher to use
	if params.Options == nil {
		params.Options = &LaunchOptions{}
	}
	if params.Options.Action == "" {
		params.Options.Action = action
	}

	// Stop any currently running launcher before starting new one
	// This ensures tracked processes (like videos) are stopped even when
	// FireAndForget launches (like MGL files) start. UNLESS the new launcher
	// uses a running instance (e.g., Kodi), in which case the platform's
	// shouldKeepRunningInstance logic will handle stopping if needed.
	if params.Launcher.UsesRunningInstance == "" {
		if stopErr := params.Platform.StopActiveLauncher(StopForPreemption); stopErr != nil {
			log.Debug().Err(stopErr).Msg("no active launcher to stop or error stopping")
		}
	}

	switch params.Launcher.Lifecycle {
	case LifecycleTracked:
		proc, err := params.Launcher.Launch(params.Config, params.Path, params.Options)
		if err != nil {
			return fmt.Errorf("failed to launch: %w", err)
		}
		if proc != nil {
			params.Platform.SetTrackedProcess(proc)
		}
		log.Debug().Msgf("launched tracked process for: %s", params.Path)
	case LifecycleBlocking:
		go func() {
			log.Debug().Msgf("launching blocking process for: %s", params.Path)
			proc, err := params.Launcher.Launch(params.Config, params.Path, params.Options)
			if err != nil {
				log.Error().Err(err).Msgf("blocking launcher failed for: %s", params.Path)
				params.SetActiveMedia(nil)
				return
			}

			if proc != nil {
				params.Platform.SetTrackedProcess(proc)

				_, waitErr := proc.Wait()
				if waitErr != nil {
					log.Debug().Err(waitErr).Msgf("blocking process wait error for: %s", params.Path)
				} else {
					log.Debug().Msgf("blocking process completed for: %s", params.Path)
				}

				params.SetActiveMedia(nil)
				log.Debug().Msgf("cleared active media after blocking process ended: %s", params.Path)
			}
		}()
	case LifecycleFireAndForget:
		_, err := params.Launcher.Launch(params.Config, params.Path, params.Options)
		if err != nil {
			return fmt.Errorf("failed to launch: %w", err)
		}
	}

	// "details" action just shows info page, doesn't launch a game
	if IsActionDetails(action) {
		log.Debug().Msg("skipping ActiveMedia for details action")
		return nil
	}

	// Try to look up SystemID from MediaDB if launcher doesn't have one
	systemID := params.Launcher.SystemID
	displayName := tags.ParseTitleFromFilename(getDisplayName(params.Path), false)

	if params.DB != nil && params.DB.MediaDB != nil {
		results, searchErr := params.DB.MediaDB.SearchMediaPathExact(nil, params.Path)
		if searchErr == nil && len(results) > 0 {
			if systemID == "" && results[0].SystemID != "" {
				systemID = results[0].SystemID
			}
			if results[0].Name != "" {
				displayName = results[0].Name
			}
		}
	}

	if systemID == "" {
		log.Debug().Msg("skipping ActiveMedia - no SystemID available")
		return nil
	}

	systemMeta, err := assets.GetSystemMetadata(systemID)
	if err != nil {
		log.Debug().Err(err).Msgf("no system metadata for: %s", systemID)
	}

	activeMedia := models.NewActiveMedia(
		systemID,
		systemMeta.Name,
		params.Path,
		displayName,
		params.Launcher.ID,
	)

	log.Info().Msgf(
		"DoLaunch setting ActiveMedia: SystemID='%s', SystemName='%s', Path='%s', Name='%s', LauncherID='%s'",
		activeMedia.SystemID, activeMedia.SystemName, activeMedia.Path, activeMedia.Name, activeMedia.LauncherID,
	)

	params.SetActiveMedia(activeMedia)

	return nil
}
