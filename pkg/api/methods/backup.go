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
	if !env.IsLocal {
		return models.ClientErrf("backup actions require a local client")
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

func requireRemoteBackupAccess(env *requests.RequestEnv) error {
	if err := requireBackupAccess(env); err != nil {
		return err
	}
	if !env.Config.BackupRemoteEnabled() {
		return models.ClientErrf("remote backup is disabled")
	}
	return nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackup(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backup, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}
	return backup, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupList(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backups, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).List()
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
	backup, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).Inspect(params.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect backup: %w", err)
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
	if err := backupsvc.NewManager(env.Config, env.Platform, env.Database).Delete(params.Name); err != nil {
		return nil, fmt.Errorf("failed to delete backup: %w", err)
	}
	return NoContent{}, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupStatus(env requests.RequestEnv) (any, error) {
	if err := requireBackupRuntime(&env); err != nil {
		return nil, err
	}
	return backupsvc.NewManager(env.Config, env.Platform, env.Database).Status(), nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRestore(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupRestoreParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.Name == "" {
		return nil, models.ClientErrf("invalid params: name is required")
	}
	restore, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).Restore(params.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to restore backup: %w", err)
	}
	return restore, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteRun(env requests.RequestEnv) (any, error) {
	if err := requireRemoteBackupAccess(&env); err != nil {
		return nil, err
	}
	mgr := backupsvc.NewManager(env.Config, env.Platform, env.Database)
	if env.State != nil {
		mgr.WithInbox(env.State.Inbox())
	}
	backup, err := mgr.RunRemote(env.Context, backupsvc.RemoteBackupTypeManual)
	if err != nil {
		return nil, fmt.Errorf("failed to run remote backup: %w", err)
	}
	return backup, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteList(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backups, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).ListRemote(env.Context)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote backups: %w", err)
	}
	return backups, nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackupRemoteRestore(env requests.RequestEnv) (any, error) {
	if err := requireRemoteBackupAccess(&env); err != nil {
		return nil, err
	}
	var params models.BackupRemoteRestoreParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.ID <= 0 {
		return nil, models.ClientErrf("invalid params: id is required")
	}
	restore, err := backupsvc.NewManager(env.Config, env.Platform, env.Database).RestoreRemote(env.Context, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to restore remote backup: %w", err)
	}
	return restore, nil
}
