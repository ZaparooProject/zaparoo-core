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

package plex

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// displayName is the name an item is indexed and matched under, in the same
// shapes the Kodi launchers use for the same media.
func displayName(kind *mediaKind, md *Metadata) string {
	title := strings.TrimSpace(md.Title)
	switch kind.plexType {
	case plexTypeMovie:
		if md.Year > 0 {
			return fmt.Sprintf("%s (%d)", title, md.Year)
		}
		return title
	case plexTypeEpisode:
		return fmt.Sprintf("%s - S%02dE%02d - %s",
			strings.TrimSpace(md.GrandparentTitle), md.ParentIndex, md.Index, title)
	case plexTypeAlbum:
		name := strings.TrimSpace(md.ParentTitle) + " - " + title
		if md.Year > 0 {
			name = fmt.Sprintf("%s (%d)", name, md.Year)
		}
		return name
	case plexTypeTrack:
		return strings.TrimSpace(md.GrandparentTitle) + " - " + strings.TrimSpace(md.ParentTitle) + " - " + title
	default:
		return title
	}
}

// isCompilationArtist matches the placeholder artists that hold
// compilations, which are skipped as Kodi skips them.
func isCompilationArtist(name string) bool {
	return name == "Various Artists" || name == "Various"
}

// scanKind indexes every item of one kind across the server's matching
// library sections.
func scanKind(
	ctx context.Context,
	client PlexClient,
	kind *mediaKind,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	sections, err := client.Sections(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to scan plex %ss: %w", kind.typeName, err)
	}
	for _, s := range sectionsOfType(sections, kind.sectionType) {
		err := client.Items(ctx, s.Key, kind.plexType, nil, func(items []Metadata) error {
			for i := range items {
				md := &items[i]
				if md.RatingKey == "" || !validKey(md.RatingKey) || strings.TrimSpace(md.Title) == "" {
					continue
				}
				if kind.plexType == plexTypeArtist && isCompilationArtist(md.Title) {
					continue
				}
				name := displayName(kind, md)
				results = append(results, platforms.ScanResult{
					Name:  name,
					Path:  createItemPath(kind, md, name),
					NoExt: true,
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to scan plex section %q: %w", s.Title, err)
		}
	}
	return results, nil
}

// collect lists the episodes or tracks under the show, album or artist a
// path names, in play order.
func collect(ctx context.Context, client PlexClient, kind *mediaKind, path string) ([]platforms.CollectionItem, error) {
	md, err := resolveItem(ctx, client, kind, path)
	if err != nil {
		return nil, err
	}
	var leaves []Metadata
	err = client.Leaves(ctx, md.RatingKey, func(items []Metadata) error {
		leaves = append(leaves, items...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list plex %s %s: %w", kind.typeName, md.RatingKey, err)
	}
	leafKind := kindTrack
	if kind.plexType == plexTypeShow {
		leafKind = kindEpisode
		slices.SortStableFunc(leaves, func(a, b Metadata) int {
			return cmp.Or(cmp.Compare(a.ParentIndex, b.ParentIndex), cmp.Compare(a.Index, b.Index))
		})
	} else {
		slices.SortStableFunc(leaves, func(a, b Metadata) int {
			return cmp.Or(
				cmp.Compare(a.ParentTitle, b.ParentTitle),
				cmp.Compare(a.ParentRatingKey, b.ParentRatingKey),
				cmp.Compare(a.ParentIndex, b.ParentIndex),
				cmp.Compare(a.Index, b.Index),
			)
		})
	}
	items := make([]platforms.CollectionItem, 0, len(leaves))
	for i := range leaves {
		leaf := &leaves[i]
		if !validKey(leaf.RatingKey) {
			continue
		}
		name := displayName(leafKind, leaf)
		items = append(items, platforms.CollectionItem{
			Name: name,
			Path: createItemPath(leafKind, leaf, name),
		})
	}
	return items, nil
}
