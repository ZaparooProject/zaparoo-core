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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
)

func requireBackupAccess(env *requests.RequestEnv) error {
	if env.Database == nil || env.Database.UserDB == nil {
		return errors.New("database is not available")
	}
	if !isLocalOrAdmin(env) {
		return models.ClientErrf("backup actions require a local or admin client")
	}
	if env.Config == nil || env.Platform == nil {
		return errors.New("backup runtime is not available")
	}
	return nil
}

func requireBackupRuntime(env *requests.RequestEnv) error {
	if env.Config == nil || env.Platform == nil {
		return errors.New("backup runtime is not available")
	}
	return nil
}

func backupMethodError(action string, err error) error {
	if errors.Is(err, backupsvc.ErrPlatformBackupUnsupported) {
		return models.ClientErrf("full-device backup is not supported on this platform")
	}
	if errors.Is(err, backupsvc.ErrRestoreMediaActive) {
		return models.ClientErrf("cannot restore backup while media is active")
	}
	if errors.Is(err, backupsvc.ErrRestoreLaunchInProgress) {
		return models.ClientErrf("cannot restore backup while media is launching or restart is pending")
	}
	var busy *backupsvc.BusyError
	if errors.As(err, &busy) {
		return models.ClientErrf("backup is busy with %s", busy.Kind)
	}
	return fmt.Errorf("failed to %s: %w", action, err)
}

func backupRestoreResponse(env *requests.RequestEnv, result any) any {
	if env.State == nil {
		return result
	}
	return models.ResponseWithCallback{
		Result: result,
		AfterWrite: func() {
			env.State.RestartService()
		},
	}
}

func newBackupManager(env *requests.RequestEnv) *backupsvc.Manager {
	mgr := backupsvc.NewManager(env.Config, env.Platform, env.Database)
	if env.State != nil {
		mgr.WithCoordinator(env.State.BackupCoordinator()).
			WithInbox(env.State.Inbox()).
			WithActiveMedia(env.State.ActiveMedia).
			WithRestoreGate(env.State.BeginRestoreGate)
	}
	mgr.WithPauser(env.BackupPauser)
	return mgr
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackup(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	// Reconcile with current primary-media state before starting, clearing
	// stale pause state left by non-primary media events (same as indexing).
	if env.State != nil {
		syncMediaWorkPauserWithActiveMedia(env.Config, env.State.ActiveMedia(), env.BackupPauser)
	}
	backup, err := newBackupManager(&env).Create(env.Context)
	if err != nil {
		return nil, backupMethodError("create backup", err)
	}
	return backup, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupList(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backups, err := newBackupManager(&env).List()
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	return backups, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupInspect(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupNameParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.Name == "" {
		return nil, models.ClientErrf("invalid params: name is required")
	}
	backup, err := newBackupManager(&env).Inspect(env.Context, params.Name)
	if err != nil {
		return nil, backupMethodError("inspect backup", err)
	}
	return backup, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupDelete(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupNameParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.Name == "" {
		return nil, models.ClientErrf("invalid params: name is required")
	}
	if err := newBackupManager(&env).Delete(env.Context, params.Name); err != nil {
		return nil, backupMethodError("delete backup", err)
	}
	return NoContent{}, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupStatus(env requests.RequestEnv) (any, error) {
	if err := requireBackupRuntime(&env); err != nil {
		return nil, err
	}
	mgr := newBackupManager(&env)
	if isLocalOrAdmin(&env) {
		// Refresh in the background: a stale availability check is a network
		// round trip and must not block the status response.
		mgr.RefreshRemoteAvailabilityIfStaleAsync()
	}
	return mgr.Status(), nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRestore(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupNameParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.Name == "" {
		return nil, models.ClientErrf("invalid params: name is required")
	}
	restore, err := newBackupManager(&env).Restore(env.Context, params.Name)
	if err != nil {
		return nil, backupMethodError("restore backup", err)
	}
	return backupRestoreResponse(&env, restore), nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteRun(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	// Reconcile with current primary-media state before starting, clearing
	// stale pause state left by non-primary media events (same as indexing).
	if env.State != nil {
		syncMediaWorkPauserWithActiveMedia(env.Config, env.State.ActiveMedia(), env.BackupPauser)
	}
	mgr := newBackupManager(&env)
	backup, err := mgr.RunRemote(env.Context, backupsvc.RemoteBackupTypeManual)
	if err != nil {
		return nil, backupMethodError("run remote backup", err)
	}
	return backup, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteList(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backups, err := newBackupManager(&env).ListRemote(env.Context)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote backups: %w", err)
	}
	return backups, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteRestore(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupRemoteRestoreParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.ID == "" {
		return nil, models.ClientErrf("invalid params: id is required")
	}
	restore, err := newBackupManager(&env).RestoreRemote(env.Context, params.ID)
	if err != nil {
		return nil, backupMethodError("restore remote backup", err)
	}
	return backupRestoreResponse(&env, restore), nil
}
