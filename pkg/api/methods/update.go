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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
func updaterOptions(env *requests.RequestEnv, mode updater.Mode) updater.Options {
	opts := updater.Options{
		PlatformID: env.Platform.ID(),
		Channel:    env.Config.UpdateChannel(),
		DataDir:    helpers.DataDir(env.Platform),
		DeviceID:   env.Config.DeviceID(),
		Managed:    env.Platform.ManagedByPackageManager(),
		Mode:       mode,
	}
	if env.Database != nil {
		opts.UserDB = env.Database.UserDB
	}
	return opts
}

// updateGateDeps wires the gate up to what this device is currently doing.
// Anything the request environment does not carry is left nil, which the gate
// reads as nothing to report rather than as a reason to refuse.
func updateGateDeps(env *requests.RequestEnv) *updater.GateDeps {
	pl := env.Platform
	deps := &updater.GateDeps{
		Power: func() power.Status { return platforms.PowerStatus(pl) },
	}

	if env.Database != nil {
		deps.IndexingStatus = env.Database.MediaDB.GetIndexingStatus
		deps.OptimizationStatus = env.Database.MediaDB.GetOptimizationStatus
		deps.ScrapingStatus = env.Database.MediaDB.GetScrapingStatus
	}

	st := env.State
	if st == nil {
		return deps
	}
	if coordinator := st.BackupCoordinator(); coordinator != nil {
		deps.BackupActive = func() bool {
			_, _, active := coordinator.Active()
			return active
		}
	}
	deps.ReaderWriteActive = st.AnyReaderWriteActive
	deps.ActiveMedia = func() bool { return st.ActiveMedia() != nil }
	deps.BackgroundMedia = func() bool { return st.BackgroundMedia() != nil }
	deps.ActivePlaylist = func() bool { return st.GetActivePlaylist() != nil }
	return deps
}

// updateProgressFn forwards the updater's progress to every connected client.
func updateProgressFn(env *requests.RequestEnv) updater.ProgressFn {
	if env.State == nil || env.State.Notifications == nil {
		return nil
	}
	ns := env.State.Notifications
	return func(progress updater.Progress) {
		notifications.UpdateState(ns, models.UpdateStateNotification{
			Stage:           string(progress.Stage),
			Version:         progress.Version,
			Trigger:         progress.Trigger,
			Error:           progress.Error,
			BytesDownloaded: progress.BytesDownloaded,
			BytesTotal:      progress.BytesTotal,
		})
	}
}

// HandleUpdateCheck asks the release server what the newest build for this
// device is. Any local or paired client may call it, member included. A check
// makes the device fetch and verify signed metadata and write the result to
// its data directory, so unpaired remote clients are refused: otherwise
// anyone on the network could drive repeated flash writes and outbound
// requests.
func HandleUpdateCheck(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	checkFn func(ctx context.Context, opts updater.Options) (*updater.Result, error),
) (any, error) {
	if err := requireAuthenticated(&env); err != nil {
		return nil, err
	}

	autoInstall := env.Config.UpdateInstall()

	opts := updaterOptions(&env, updater.ModeManual)
	// The gate is read here so a client knows what is in the way before it
	// offers an update button, rather than finding out from a failed apply.
	opts.Gate = updateGateDeps(&env)

	result, err := checkFn(env.Context, opts)
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return models.UpdateCheckResponse{
			CurrentVersion:  config.AppVersion,
			Eligibility:     updater.EligibilityDevelopment,
			Channel:         env.Config.UpdateChannel(),
			AutoInstall:     autoInstall,
			UpdateAvailable: false,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update check failed: %w", err)
	}

	resp := models.UpdateCheckResponse{
		CurrentVersion:  result.CurrentVersion,
		LatestVersion:   result.LatestVersion,
		UpdateAvailable: result.UpdateAvailable,
		ReleaseNotes:    result.ReleaseNotes,
		Channel:         result.Channel,
		Eligibility:     result.Eligibility,
		RolloutHeld:     result.RolloutHeld,
		AutoInstall:     autoInstall,
		DeferredReason:  result.DeferredReason,
	}
	if !result.CheckedAt.IsZero() {
		checkedAt := result.CheckedAt
		resp.CheckedAt = &checkedAt
	}
	if !result.DeferredSince.IsZero() {
		since := result.DeferredSince
		resp.DeferredSince = &since
	}
	if result.BlockedReason != "" {
		resp.BlockedBy = &models.UpdateBlockedBy{
			Reason:    result.BlockedReason,
			Message:   result.BlockedMessage,
			Forceable: result.BlockedForceable,
		}
	}
	if result.LastResult != nil {
		resp.LastResult = &models.UpdateLastResult{
			At:          result.LastResult.At,
			Outcome:     result.LastResult.Outcome,
			FromVersion: result.LastResult.FromVersion,
			ToVersion:   result.LastResult.ToVersion,
			Detail:      result.LastResult.Detail,
		}
	}
	return resp, nil
}

func HandleUpdateApply(
	env requests.RequestEnv, //nolint:gocritic // hugeParam
	applyFn func(ctx context.Context, opts updater.Options) (string, error),
	restartFn func(),
) (any, error) {
	// Refuses paired members and unpaired remote clients alike.
	if err := requireCapability(&env, permissions.CapUpdateApply); err != nil {
		log.Warn().
			Str("clientId", env.ClientID).
			Bool("local", env.IsLocal).
			Str("role", env.ClientRole).
			Msg("rejected update apply request")
		return nil, err
	}

	var params models.UpdateApplyParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	// The gate takes the restore and media gates itself, in the order the rest
	// of the service takes them, and holds them until the restart.
	deps := updateGateDeps(&env)
	if env.State != nil {
		deps.AcquireRestore = env.State.TryAcquireRestoreAccess
		deps.AcquireMediaGate = env.State.AcquireUpdateMediaGate
	}
	decision, err := updater.CanApplyUpdate(env.Context, deps, updater.ModeManual, params.Force)
	if err != nil {
		return nil, fmt.Errorf("preparing the device for an update: %w", err)
	}
	if !decision.OK {
		log.Info().
			Str("reason", decision.Reason).
			Bool("forceable", decision.Forceable).
			Msg("refused an update the device is not ready for")
		return nil, models.ClientErrf("%s", decision.Message)
	}

	releaseBeforeRestart := true
	release := decision.Release
	defer func() {
		if releaseBeforeRestart {
			release()
		}
	}()

	previousVersion := config.AppVersion

	opts := updaterOptions(&env, updater.ModeManual)
	opts.Progress = updateProgressFn(&env)
	// The download can outlast a charger being unplugged, so the power reading
	// is taken again at the last moment the install can still be called off.
	opts.PreQuiesce = func(context.Context) error {
		powered := updater.PowerReady(deps, updater.ModeManual, params.Force)
		return powered.Err()
	}

	newVersion, err := applyFn(env.Context, opts)
	if errors.Is(err, updater.ErrDevelopmentVersion) {
		return nil, models.ClientErrf("cannot apply updates on development builds")
	}
	if errors.Is(err, updater.ErrUpdateInProgress) {
		return nil, models.ClientErrf("update already in progress")
	}
	if errors.Is(err, updater.ErrInsufficientSpace) {
		// The error names the directory and the shortfall, which is the only
		// part the user can act on.
		return nil, models.ClientErrf("%s", err.Error())
	}
	if errors.Is(err, updater.ErrPlatformUnsupported) {
		// The error already says what to do instead.
		return nil, models.ClientErrf("%s", err.Error())
	}
	var gateErr *updater.GateError
	if errors.As(err, &gateErr) {
		return nil, models.ClientErrf("%s", gateErr.Message)
	}
	if err != nil {
		return nil, fmt.Errorf("update apply failed: %w", err)
	}

	restartGuard := newUpdateRestartGuard(
		updateAfterWriteFallbackDelay, previousVersion, newVersion, restartFn, release,
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
