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

package apidiag

import "database/sql"

// PoolSnapshot reads in-memory database/sql counters, never a connection/query.
func PoolSnapshot(db *sql.DB) Pool {
	if db == nil {
		return Pool{}
	}
	stats := db.Stats()
	return Pool{
		Available: true, Max: stats.MaxOpenConnections, Open: stats.OpenConnections,
		InUse: stats.InUse, Idle: stats.Idle,
		WaitCount: stats.WaitCount, WaitDuration: stats.WaitDuration,
	}
}
