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
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/rs/zerolog/log"
)

func HandleUpdateCheck(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	checkFn func(ctx context.Context, platformID string) (*updater.Result, error),
) (any, error) {
	result, err := checkFn(env.Context, env.Platform.ID())
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return models.UpdateCheckResponse{
			CurrentVersion:  config.AppVersion,
			UpdateAvailable: false,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update check failed: %w", err)
	}

	return models.UpdateCheckResponse{
		CurrentVersion:  result.CurrentVersion,
		LatestVersion:   result.LatestVersion,
		UpdateAvailable: result.UpdateAvailable,
		ReleaseNotes:    result.ReleaseNotes,
	}, nil
}

func HandleUpdateApply(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	applyFn func(ctx context.Context, platformID string) (string, error),
	exitFn func(int),
) (any, error) {
	// Reject updates while media indexing is in progress to avoid
	// interrupting database writes with os.Exit.
	if env.Database != nil {
		if status, err := env.Database.MediaDB.GetIndexingStatus(); err == nil {
			if status == mediadb.IndexingStatusRunning || status == mediadb.IndexingStatusPending {
				return nil, errors.New("cannot apply update while media indexing is in progress")
			}
		}
	}

	previousVersion := config.AppVersion

	newVersion, err := applyFn(env.Context, env.Platform.ID())
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return nil, errors.New("cannot apply updates on development builds")
	}
	if err != nil {
		return nil, fmt.Errorf("update apply failed: %w", err)
	}

	// Trigger a deferred exit so the JSON response has time to flush.
	// Exit with ExitCodeUpdateRestart so the process manager (systemd, etc.)
	// treats it as a failure and restarts with the new binary.
	//
	// TODO: replace os.Exit with a graceful shutdown via st.StopService()
	// and a custom exit code. os.Exit bypasses deferred cleanup (DB close,
	// HTTP server drain, publisher/discovery stop). SQLite WAL mode
	// prevents data corruption, but a graceful path would be cleaner.
	go func() {
		time.Sleep(1 * time.Second)
		log.Info().
			Str("previous", previousVersion).
			Str("new", newVersion).
			Msg("update applied, restarting service")
		exitFn(config.ExitCodeUpdateRestart)
	}()

	return models.UpdateApplyResponse{
		PreviousVersion: previousVersion,
		NewVersion:      newVersion,
	}, nil
}
