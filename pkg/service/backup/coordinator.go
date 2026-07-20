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

package backup

import backupcoordinator "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup/coordinator"

type OperationKind = backupcoordinator.OperationKind

type OperationMode = backupcoordinator.OperationMode

type BusyError = backupcoordinator.BusyError

type Coordinator = backupcoordinator.Coordinator

type Lease = backupcoordinator.Lease

const (
	OperationLocalCreate   = backupcoordinator.OperationLocalCreate
	OperationLocalInspect  = backupcoordinator.OperationLocalInspect
	OperationLocalDelete   = backupcoordinator.OperationLocalDelete
	OperationLocalRestore  = backupcoordinator.OperationLocalRestore
	OperationRemoteUpload  = backupcoordinator.OperationRemoteUpload
	OperationRemoteRestore = backupcoordinator.OperationRemoteRestore
	OperationRecovery      = backupcoordinator.OperationRecovery
	OperationRead          = backupcoordinator.OperationRead
	OperationWrite         = backupcoordinator.OperationWrite
)

var ErrCoordinatorStopped = backupcoordinator.ErrStopped

func NewCoordinator() *Coordinator {
	return backupcoordinator.New()
}
