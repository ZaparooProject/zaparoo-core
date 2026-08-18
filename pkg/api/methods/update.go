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
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/rs/zerolog/log"
)

const updateAfterWriteFallbackDelay = 5 * time.Second

type updateRestartGuard struct {
	timer           *time.Timer
	restart         func()
	release         func()
	previousVersion string
	newVersion      string
	once            sync.Once
}

func newUpdateRestartGuard(
	delay time.Duration,
	previousVersion, newVersion string,
	restart, release func(),
) *updateRestartGuard {
	guard := &updateRestartGuard{
		restart:         restart,
		release:         release,
		previousVersion: previousVersion,
		newVersion:      newVersion,
	}
	guard.timer = time.AfterFunc(delay, func() {
		guard.finish(true)
	})
	return guard
}

func (g *updateRestartGuard) afterWrite() {
	g.timer.Stop()
	g.finish(false)
}

func (g *updateRestartGuard) finish(fallback bool) {
	g.once.Do(func() {
		defer g.release()
		event := log.Info()
		message := "update applied, restarting service"
		if fallback {
			event = log.Warn()
			message = "update response callback did not run, forcing service restart"
		}
		event.
			Str("previous", g.previousVersion).
			Str("new", g.newVersion).
			Msg(message)
		g.restart()
	})
}

// updaterOptions describes the device to the updater.
func updaterOptions(env *requests.RequestEnv) updater.Options {
	opts := updater.Options{
		PlatformID: env.Platform.ID(),
		Channel:    env.Config.UpdateChannel(),
		DataDir:    helpers.DataDir(env.Platform),
	}
	if env.Database != nil {
		opts.UserDB = env.Database.UserDB
	}
	return opts
}

func HandleUpdateCheck(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	checkFn func(ctx context.Context, opts updater.Options) (*updater.Result, error),
) (any, error) {
	result, err := checkFn(env.Context, updaterOptions(&env))
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
	applyFn func(ctx context.Context, opts updater.Options) (string, error),
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

	releaseMediaGate := func() {}
	if env.State != nil {
		release, err := env.State.AcquireUpdateMediaGate(env.Context)
		if err != nil {
			return nil, fmt.Errorf("waiting for media activity to settle before update: %w", err)
		}
		releaseMediaGate = release
		if env.State.ActiveMedia() != nil {
			releaseMediaGate()
			return nil, models.ClientErrf("cannot apply update while media is active")
		}
	}
	releaseBeforeRestart := true
	defer func() {
		if releaseBeforeRestart {
			releaseMediaGate()
		}
	}()

	previousVersion := config.AppVersion

	newVersion, err := applyFn(env.Context, updaterOptions(&env))
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return nil, models.ClientErrf("cannot apply updates on development builds")
	}
	if errors.Is(err, updater.ErrUpdateInProgress) {
		return nil, models.ClientErrf("update already in progress")
	}
	if err != nil {
		return nil, fmt.Errorf("update apply failed: %w", err)
	}

	restartGuard := newUpdateRestartGuard(
		updateAfterWriteFallbackDelay, previousVersion, newVersion, restartFn, releaseMediaGate,
	)
	releaseBeforeRestart = false
	return models.ResponseWithCallback{
		Result: models.UpdateApplyResponse{
			PreviousVersion: previousVersion,
			NewVersion:      newVersion,
		},
		AfterWrite: restartGuard.afterWrite,
	}, nil
}
