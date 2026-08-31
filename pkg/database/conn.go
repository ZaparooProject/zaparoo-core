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

package database

import (
	"database/sql"
	"sync/atomic"
)

// Conn holds a *sql.DB handle that may be swapped at runtime — corruption
// recovery and backup restore close the old connection and open a new one while
// other goroutines are still using the database. The swap (Store) and every read
// (Load) go through an atomic pointer, so reassigning the handle is race-free
// without serializing queries: SQLite's WAL mode and busy_timeout already handle
// query concurrency. A reader that loads the handle mid-swap sees either the old
// connection (returning a clean "database is closed" error) or the new one, never
// a torn pointer. Both UserDB and MediaDB embed Conn so they manage their
// connection handle identically.
//
// Invariant: production code never clears the handle back to nil. A runtime swap
// always goes Open -> Store(new), so once a database has been opened Load() stays
// non-nil. Several call sites rely on this by reading Load() more than once per
// method without re-checking: because the handle is never swapped to nil, a guard
// check (Load() == nil) followed by a later Load() for the query cannot observe nil
// in between. Only tests call Store(nil). A future change that clears the handle in
// production would reintroduce that nil-dereference race across those call sites.
type Conn struct {
	ptr atomic.Pointer[sql.DB]
}

// Load returns the current handle, or nil when none is set.
func (c *Conn) Load() *sql.DB {
	return c.ptr.Load()
}

// Store replaces the current handle. Production code only ever stores a freshly
// opened, non-nil handle; passing nil to clear it is reserved for tests (see the
// Conn invariant above).
func (c *Conn) Store(db *sql.DB) {
	c.ptr.Store(db)
}
