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

package mediascanner

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// IndexedSource identifies an applicable contribution, not a durable media row.
type IndexedSource struct {
	LauncherID string
	SystemID   string
	Files      int
}

// IndexSourceOptions requests a bounded summary for selected launchers. Completed
// runs only after successful finalization; existing index callers need no hook.
type IndexSourceOptions struct {
	Completed   func([]IndexedSource)
	LauncherIDs []string
}

type indexSourceCollector struct {
	cfg     *config.Instance
	wanted  map[string]bool
	matcher *helpers.LauncherMatcher
	sources []IndexedSource
}

func newIndexSourceCollector(
	cfg *config.Instance, pl platforms.Platform, opts *IndexSourceOptions,
) *indexSourceCollector {
	if opts == nil || opts.Completed == nil || len(opts.LauncherIDs) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(opts.LauncherIDs))
	for _, id := range opts.LauncherIDs {
		wanted[id] = true
	}
	return &indexSourceCollector{cfg: cfg, wanted: wanted, matcher: helpers.NewLauncherMatcher(cfg, pl)}
}

func (c *indexSourceCollector) record(
	systemID string, files []platforms.ScanResult, launcher *platforms.Launcher,
	succeeded map[string]bool, incomplete bool,
) {
	if c == nil || !c.wanted[launcher.ID] || launcher.AvailabilityReason != "" {
		return
	}
	// The index-local cache intentionally does not evaluate launch availability.
	// Check only opted-in sources, without changing ordinary indexing behavior.
	if launcher.Availability != nil && launcher.Availability(c.cfg) != nil {
		return
	}
	if launcher.Scanner != nil && !succeeded[launcher.ID] {
		return
	}
	if incomplete && !launcher.SkipFilesystemScan {
		return
	}
	count := 0
	for _, file := range files {
		if c.matcher.MatchLauncherFileForScan(launcher, file.Path) {
			count++
		}
	}
	if count > 0 {
		c.sources = append(c.sources, IndexedSource{LauncherID: launcher.ID, SystemID: systemID, Files: count})
	}
}
