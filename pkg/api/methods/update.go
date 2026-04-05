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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/rs/zerolog/log"
)

func HandleUpdateCheck(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	checkFn func(ctx context.Context, platformID, channel string) (*updater.Result, error),
) (any, error) {
	result, err := checkFn(env.Context, env.Platform.ID(), env.Config.UpdateChannel())
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
	applyFn func(ctx context.Context, platformID, channel string) (string, error),
	restartFn func(),
) (any, error) {
	// Reject updates while media indexing is in progress to avoid
	// interrupting database writes mid-transaction.
	if env.Database != nil {
		if status, err := env.Database.MediaDB.GetIndexingStatus(); err == nil {
			if status == mediadb.IndexingStatusRunning || status == mediadb.IndexingStatusPending {
				return nil, models.ClientErrf("cannot apply update while media indexing is in progress")
			}
		}
	}

	previousVersion := config.AppVersion

	newVersion, err := applyFn(env.Context, env.Platform.ID(), env.Config.UpdateChannel())
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return nil, models.ClientErrf("cannot apply updates on development builds")
	}
	if err != nil {
		return nil, fmt.Errorf("update apply failed: %w", err)
	}

	return models.ResponseWithCallback{
		Result: models.UpdateApplyResponse{
			PreviousVersion: previousVersion,
			NewVersion:      newVersion,
		},
		AfterWrite: func() {
			log.Info().
				Str("previous", previousVersion).
				Str("new", newVersion).
				Msg("update applied, restarting service")
			restartFn()
		},
	}, nil
}
