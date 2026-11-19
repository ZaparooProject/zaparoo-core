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

package romm

import (
	"context"
	"slices"
)

// ServerSystems lists the Zaparoo systems the server has a platform for.
func ServerSystems(ctx context.Context, c *Client) ([]string, error) {
	list, err := platformsFor(ctx, c)
	if err != nil {
		return nil, err
	}
	systems := make([]string, 0, len(list))
	for i := range list {
		if id, ok := SystemForPlatform(list[i].Slug, list[i].FSSlug); ok {
			systems = append(systems, id)
		}
	}
	slices.Sort(systems)
	return slices.Compact(systems), nil
}

// SystemRoms lists every ROM the server files under platforms that map to
// systemID. A system no platform maps to has none, and is not an error.
func SystemRoms(ctx context.Context, c *Client, systemID string) ([]Rom, error) {
	list, err := platformsFor(ctx, c)
	if err != nil {
		return nil, err
	}
	var roms []Rom
	for i := range list {
		if id, ok := SystemForPlatform(list[i].Slug, list[i].FSSlug); !ok || id != systemID {
			continue
		}
		page, romsErr := c.Roms(ctx, list[i].ID)
		if romsErr != nil {
			return nil, romsErr
		}
		roms = append(roms, page...)
	}
	return roms, nil
}

// MatchNames are the lowercase file names a ROM is stored under on a device
// once downloaded, or under which a user's own copy of it would be indexed.
// It answers nil for a ROM whose files cannot be stored safely.
func MatchNames(rom *Rom) []string {
	layout, err := LayoutFor(rom)
	if err != nil {
		return nil
	}
	return LocalNames(rom, layout)
}
