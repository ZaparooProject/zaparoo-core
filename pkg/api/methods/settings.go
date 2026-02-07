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

package methods

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

func HandleSettings(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received settings request")

	connectCfg := env.Config.Readers().Connect
	readersConnect := make([]models.ReaderConnection, 0, len(connectCfg))
	for _, rc := range connectCfg {
		readersConnect = append(readersConnect, models.ReaderConnection{
			Driver:   rc.Driver,
			Path:     rc.Path,
			IDSource: rc.IDSource,
		})
	}

	resp := models.SettingsResponse{
		RunZapScript:            env.State.RunZapScriptEnabled(),
		DebugLogging:            env.Config.DebugLogging(),
		AudioScanFeedback:       env.Config.AudioFeedback(),
		ReadersAutoDetect:       env.Config.Readers().AutoDetect,
		ReadersScanMode:         env.Config.ReadersScan().Mode,
		ReadersScanExitDelay:    env.Config.ReadersScan().ExitDelay,
		ReadersScanIgnoreSystem: make([]string, 0),
		ReadersConnect:          readersConnect,
		ErrorReporting:          env.Config.ErrorReporting(),
	}

	resp.ReadersScanIgnoreSystem = append(resp.ReadersScanIgnoreSystem, env.Config.ReadersScan().IgnoreSystem...)

	return resp, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleSettingsReload(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings reload request")

	err := env.Config.Load()
	if err != nil {
		log.Error().Err(err).Msg("error loading settings")
		return nil, errors.New("error loading settings")
	}

	mapDir := filepath.Join(helpers.DataDir(env.Platform), config.MappingsDir)
	err = env.Config.LoadMappings(mapDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading mappings")
		return nil, errors.New("error loading mappings")
	}

	launchersDir := filepath.Join(helpers.DataDir(env.Platform), config.LaunchersDir)
	err = env.Config.LoadCustomLaunchers(launchersDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading custom launchers")
		return nil, errors.New("error loading custom launchers")
	}

	env.LauncherCache.Refresh(env.Platform, env.Config)

	if env.Player != nil {
		env.Player.ClearFileCache()
	}

	return NoContent{}, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleSettingsUpdate(env requests.RequestEnv) (any, error) {
	log.Debug().Msg("received settings update request")

	var params models.UpdateSettingsParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if params.RunZapScript != nil {
		log.Debug().Bool("runZapScript", *params.RunZapScript).Msg("updating setting")
		env.State.SetRunZapScript(*params.RunZapScript)
	}

	if params.DebugLogging != nil {
		log.Debug().Bool("debugLogging", *params.DebugLogging).Msg("updating setting")
		env.Config.SetDebugLogging(*params.DebugLogging)
	}

	if params.AudioScanFeedback != nil {
		log.Debug().Bool("audioScanFeedback", *params.AudioScanFeedback).Msg("updating setting")
		env.Config.SetAudioFeedback(*params.AudioScanFeedback)
	}

	if params.ReadersAutoDetect != nil {
		log.Debug().Bool("readersAutoDetect", *params.ReadersAutoDetect).Msg("updating setting")
		env.Config.SetAutoDetect(*params.ReadersAutoDetect)
	}

	if params.ErrorReporting != nil {
		log.Debug().Bool("errorReporting", *params.ErrorReporting).Msg("updating setting")
		env.Config.SetErrorReporting(*params.ErrorReporting)
	}

	if params.ReadersScanMode != nil {
		log.Debug().Str("readersScanMode", *params.ReadersScanMode).Msg("updating setting")
		// empty string defaults to tap mode
		if *params.ReadersScanMode == "" {
			env.Config.SetScanMode(config.ScanModeTap)
		} else {
			env.Config.SetScanMode(*params.ReadersScanMode)
		}
	}

	if params.ReadersScanExitDelay != nil {
		log.Debug().Float32("readersScanExitDelay", *params.ReadersScanExitDelay).Msg("updating setting")
		env.Config.SetScanExitDelay(*params.ReadersScanExitDelay)
	}

	if params.ReadersScanIgnoreSystem != nil {
		log.Debug().Strs("readersScanIgnoreSystem", *params.ReadersScanIgnoreSystem).Msg("updating setting")
		env.Config.SetScanIgnoreSystem(*params.ReadersScanIgnoreSystem)
	}

	if params.ReadersConnect != nil {
		log.Debug().Int("count", len(*params.ReadersConnect)).Msg("updating readers.connect")
		connections := make([]config.ReadersConnect, 0, len(*params.ReadersConnect))
		for _, rc := range *params.ReadersConnect {
			connections = append(connections, config.ReadersConnect{
				Driver:   rc.Driver,
				Path:     rc.Path,
				IDSource: rc.IDSource,
			})
		}
		env.Config.SetReaderConnections(connections)
	}

	err := env.Config.Save()
	if err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}
	return NoContent{}, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandlePlaytimeLimits(env requests.RequestEnv) (any, error) {
	log.Debug().Msg("received playtime limits request")

	enabled := env.Config.PlaytimeLimitsEnabled()
	daily := env.Config.DailyLimit()
	session := env.Config.SessionLimit()
	sessionReset := env.Config.SessionResetTimeout()
	warnings := env.Config.WarningIntervals()
	retention := env.Config.PlaytimeRetention()

	resp := models.PlaytimeLimitsResponse{
		Enabled:  enabled,
		Warnings: make([]string, 0, len(warnings)),
	}

	if daily > 0 {
		dailyStr := daily.String()
		resp.Daily = &dailyStr
	}

	if session > 0 {
		sessionStr := session.String()
		resp.Session = &sessionStr
	}

	resetStr := sessionReset.String()
	resp.SessionReset = &resetStr

	for _, w := range warnings {
		resp.Warnings = append(resp.Warnings, w.String())
	}

	if retention > 0 {
		resp.Retention = &retention
	}

	return resp, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandlePlaytimeLimitsUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received playtime limits update request")

	var params models.UpdatePlaytimeLimitsParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if params.Enabled != nil {
		log.Info().Bool("enabled", *params.Enabled).Msg("playtime limits update")
		env.Config.SetPlaytimeLimitsEnabled(*params.Enabled)

		// Apply immediately to running LimitsManager
		if env.LimitsManager != nil {
			env.LimitsManager.SetEnabled(*params.Enabled)

			// If re-enabling limits while a game is already running,
			// manually trigger session start since no media.started
			// notification will be sent for the already-running game.
			if *params.Enabled && env.State != nil && env.State.ActiveMedia() != nil {
				log.Debug().Msg("playtime: game already running, triggering session start")
				env.LimitsManager.OnMediaStarted()
			}
		}
	}

	if params.Daily != nil {
		log.Info().Str("daily", *params.Daily).Msg("playtime limits update")
		err := env.Config.SetDailyLimit(*params.Daily)
		if err != nil {
			return nil, fmt.Errorf("invalid daily limit duration: %w", err)
		}
	}

	if params.Session != nil {
		log.Info().Str("session", *params.Session).Msg("playtime limits update")
		err := env.Config.SetSessionLimit(*params.Session)
		if err != nil {
			return nil, fmt.Errorf("invalid session limit duration: %w", err)
		}
	}

	if params.SessionReset != nil {
		log.Info().Str("session_reset", *params.SessionReset).Msg("playtime limits update")
		err := env.Config.SetSessionResetTimeout(params.SessionReset)
		if err != nil {
			return nil, fmt.Errorf("invalid session reset duration: %w", err)
		}
	}

	if params.Warnings != nil {
		log.Info().Strs("warnings", *params.Warnings).Msg("playtime limits update")
		err := env.Config.SetWarningIntervals(*params.Warnings)
		if err != nil {
			return nil, fmt.Errorf("invalid warning intervals: %w", err)
		}
	}

	if params.Retention != nil {
		log.Info().Int("retention", *params.Retention).Msg("playtime limits update")
		env.Config.SetPlaytimeRetention(*params.Retention)
	}

	err := env.Config.Save()
	if err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return NoContent{}, nil
}
