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

package userdb

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

func (db *UserDB) AddInboxMessage(msg *database.InboxMessage) (*database.InboxMessage, error) {
	return sqlAddInboxMessage(db.ctx, db.sql, msg)
}

func (db *UserDB) GetInboxMessages() ([]database.InboxMessage, error) {
	return sqlGetInboxMessages(db.ctx, db.sql)
}

func (db *UserDB) DeleteInboxMessage(id int64) error {
	return sqlDeleteInboxMessage(db.ctx, db.sql, id)
}

func (db *UserDB) DeleteAllInboxMessages() (int64, error) {
	return sqlDeleteAllInboxMessages(db.ctx, db.sql)
}
