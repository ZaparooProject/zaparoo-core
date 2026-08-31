//go:build linux

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

package tracker

import (
	"context"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/rs/zerolog/log"
)

// ResolveArcadeSetName maps a MiSTer arcade core's set name (as reported by
// /tmp/CORENAME) to the single indexed .mra file that owns it. arcadeName is
// the pretty name from ArcadeDatabase.csv, used to narrow the MediaDB search
// before each candidate is confirmed by parsing its own <setname>. Ambiguous
// or unconfirmable set names report ok=false rather than guessing - callers
// keep today's set-name-only behaviour in that case.
func ResolveArcadeSetName(
	ctx context.Context, mediaDB database.MediaDBI, setName, arcadeName string,
) (path string, ok bool) {
	if mediaDB == nil || setName == "" || arcadeName == "" {
		return "", false
	}

	results, err := mediaDB.SearchMediaBySlug(ctx, ArcadeSystem, arcadeName, nil)
	if err != nil {
		log.Debug().Err(err).Str("setname", setName).Msg("failed to search media for arcade set name")
		return "", false
	}

	candidates := dedupeMediaPaths(results)
	if len(candidates) == 0 {
		return "", false
	}

	var confirmed []string
	unreadable := 0
	for _, candidate := range candidates {
		mra, mraErr := mgls.ReadMRA(candidate)
		switch {
		case mraErr != nil:
			unreadable++
		case strings.EqualFold(mra.SetName, setName):
			confirmed = append(confirmed, candidate)
		}
	}

	switch {
	case len(confirmed) == 1:
		return confirmed[0], true
	case len(confirmed) == 0 && len(candidates) == 1 && unreadable == 1:
		// The only slug candidate couldn't be confirmed (unreadable MRA),
		// but it is still the best - and only - available match.
		return candidates[0], true
	default:
		return "", false
	}
}

func dedupeMediaPaths(results []database.SearchResultWithCursor) []string {
	seen := make(map[string]struct{}, len(results))
	paths := make([]string, 0, len(results))
	for i := range results {
		p := results[i].Path
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	return paths
}
