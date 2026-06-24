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
)

func requireBackupAccess(env *requests.RequestEnv) error {
	if env.Database == nil || env.Database.UserDB == nil {
		return errors.New("database is not available")
	}
	if !env.IsLocal {
		return models.ClientErrf("backup actions require a local client")
	}
	return nil
}

//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleBackup(env requests.RequestEnv) (any, error) {
	if err := requireBackupAccess(&env); err != nil {
		return nil, err
	}
	backup, err := env.Database.UserDB.Backup("manual", true)
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
	backups, err := env.Database.UserDB.ListBackups()
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	return backups, nil
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
	restore, err := env.Database.UserDB.RestoreBackup(params.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to restore backup: %w", err)
	}
	return restore, nil
}
