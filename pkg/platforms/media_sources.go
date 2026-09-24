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

package platforms

import "context"

// HostMediaProvider supplies non-filesystem media without inventing launchers.
// Implementations snapshot their granted sources once per index run. Ordinary
// platforms need not implement this optional capability.
type HostMediaProvider interface {
	OpenMediaScan(context.Context) (HostMediaScan, error)
}

// HostMediaScan streams canonical media identities through Core's normal indexer.
// Walk must stop when context or yield fails; it must never report a partial walk
// as successful. Close releases all host resources on every exit. Successful is
// true only after the requested index has completed, not merely enumeration.
type HostMediaScan interface {
	Systems() []string
	Walk(context.Context, string, func() error, func(ScanResult) error) error
	Close(successful bool) error
}
