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

package mediadb

import "github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"

// APIDiagnostics never waits behind a transaction or queries persisted status.
// A busy transaction lock yields unknown, not a misleading inactive reading.
func (db *MediaDB) APIDiagnostics() apidiag.DatabaseSnapshot {
	if db == nil {
		return apidiag.DatabaseSnapshot{}
	}
	snapshot := apidiag.DatabaseSnapshot{
		Pool:       apidiag.PoolSnapshot(db.sql.Load()),
		Indexing:   apidiag.Observed(db.indexingCacheBoost.Load()),
		Optimizing: apidiag.Observed(db.IsOptimizing()),
		Recovery:   apidiag.Observed(db.recreating.Load()),
	}
	if db.sqlMu.TryRLock() {
		snapshot.Transaction = apidiag.Observed(db.inTransaction)
		db.sqlMu.RUnlock()
	}
	return snapshot
}
