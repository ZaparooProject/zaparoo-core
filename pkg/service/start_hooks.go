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

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

const serviceStartupStateFile = "service_startup_state.json"

type serviceStartupState struct {
	BootID string `json:"boot_id"`
}

var detectSystemBootID = defaultDetectSystemBootID

func runStartupHook(svc *ServiceContext, hookName string, script string) error {
	log.Info().Msgf("running %s: %s", hookName, script)

	plsc := playlists.PlaylistController{
		Active: svc.State.GetActivePlaylist(),
		Queue:  svc.PlaylistQueue,
	}
	t := tokens.Token{
		ScanTime: time.Now(),
		Text:     script,
		Source:   tokens.SourceHook,
	}
	env := zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, nil, nil)
	return runTokenZapScript(svc, t, plsc, &env, false)
}

func runConfiguredStartupHooks(svc *ServiceContext) {
	if script := svc.Config.LaunchersOnServiceStart(); script != "" {
		if err := runStartupHook(svc, "on_service_start", script); err != nil {
			log.Error().Err(err).Msg("error running on_service_start script")
		}
	}

	if script := svc.Config.LaunchersOnBootStart(); script != "" {
		firstStart, err := isFirstServiceStartForBoot(svc.Platform)
		if err != nil {
			log.Warn().Err(err).Msg("skipping on_boot_start: failed to detect boot state")
			return
		}
		if !firstStart {
			log.Debug().Msg("skipping on_boot_start: already ran during this boot")
			return
		}
		if err := runStartupHook(svc, "on_boot_start", script); err != nil {
			log.Error().Err(err).Msg("error running on_boot_start script")
		}
	}
}

func isFirstServiceStartForBoot(pl platforms.Platform) (bool, error) {
	bootID, err := detectSystemBootID()
	if err != nil {
		return false, err
	}

	statePath := filepath.Join(helpers.DataDir(pl), serviceStartupStateFile)
	data, readErr := os.ReadFile(statePath) //nolint:gosec // Path is controlled by platform settings.
	switch {
	case readErr == nil:
		var state serviceStartupState
		if err = json.Unmarshal(data, &state); err == nil && state.BootID == bootID {
			return false, nil
		}
	case !os.IsNotExist(readErr):
		return false, fmt.Errorf("read startup state: %w", readErr)
	}

	if err = writeServiceStartupState(statePath, bootID); err != nil {
		return false, err
	}
	return true, nil
}

func writeServiceStartupState(path string, bootID string) error {
	data, err := json.Marshal(serviceStartupState{BootID: bootID})
	if err != nil {
		return fmt.Errorf("marshal startup state: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create startup state dir: %w", err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // User-owned app state file.
		return fmt.Errorf("write startup state: %w", err)
	}
	return nil
}

func defaultDetectSystemBootID() (string, error) {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(filepath.Join(string(filepath.Separator), "proc", "sys", "kernel", "random", "boot_id"))
		if err == nil {
			bootID := strings.TrimSpace(string(data))
			if bootID != "" {
				return "linux:" + bootID, nil
			}
		}
	}

	systemUptime, err := uptime.Get()
	if err != nil {
		return "", fmt.Errorf("detect system uptime: %w", err)
	}
	bootTime := time.Now().Add(-systemUptime).Truncate(time.Minute)
	return "uptime:" + bootTime.Format(time.RFC3339), nil
}
