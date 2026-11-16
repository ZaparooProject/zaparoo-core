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

package jellyfin

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// pageSize is how many items one library request asks for.
const pageSize = 500

type pageLister func(context.Context, url.Values) (*ItemsPage, error)

// kindQuery is the base query listing every item of a kind. Album artists
// come from their own endpoint, where IncludeItemTypes filters the artists'
// items rather than the artists, so it is left off.
func kindQuery(kind *mediaKind) url.Values {
	q := url.Values{"Fields": {itemFields}}
	if kind != kindArtist {
		q.Set("Recursive", "true")
		q.Set("IncludeItemTypes", kind.itemType)
	}
	return q
}

func kindLister(c Client, kind *mediaKind) pageLister {
	if kind == kindArtist {
		return c.ListAlbumArtists
	}
	return c.ListItems
}

// eachPage calls fn with every page of a query until the server runs out of
// items or ctx ends.
func eachPage(ctx context.Context, list pageLister, query url.Values, fn func([]Item)) error {
	start := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("jellyfin query cancelled: %w", err)
		}
		q := cloneValues(query)
		q.Set("StartIndex", strconv.Itoa(start))
		q.Set("Limit", strconv.Itoa(pageSize))
		page, err := list(ctx, q)
		if err != nil {
			return err
		}
		fn(page.Items)
		start += len(page.Items)
		if len(page.Items) == 0 || start >= page.TotalRecordCount {
			return nil
		}
	}
}

func collectAll(ctx context.Context, list pageLister, query url.Values) ([]Item, error) {
	var items []Item
	err := eachPage(ctx, list, query, func(page []Item) {
		items = append(items, page...)
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Scan appends every item of the launcher's kind on the server to results.
func scan(
	ctx context.Context, c Client, kind *mediaKind, results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	err := eachPage(ctx, kindLister(c, kind), kindQuery(kind), func(page []Item) {
		for i := range page {
			item := &page[i]
			if !validItemID(item.ID) || !kindAccepts(kind, item) {
				continue
			}
			name := kind.name(item)
			results = append(results, platforms.ScanResult{
				Name:  name,
				Path:  itemPath(kind, item, name),
				NoExt: true,
			})
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan jellyfin %ss: %w", kind.label, err)
	}
	return results, nil
}

// kindAccepts skips the compilation pseudo-artists Kodi also leaves out.
func kindAccepts(kind *mediaKind, item *Item) bool {
	if kind == kindArtist && (item.Name == "Various Artists" || item.Name == "Various") {
		return false
	}
	return true
}

// collectionTracks lists what a show, album or artist item plays, in order.
func collectionTracks(ctx context.Context, c Client, kind *mediaKind, item *Item) (*mediaKind, []Item, error) {
	switch kind {
	case kindShow:
		episodes, err := c.ShowEpisodes(ctx, item.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get episodes for jellyfin show %s: %w", item.ID, err)
		}
		episodes = slices.Clone(episodes)
		slices.SortStableFunc(episodes, func(a, b Item) int {
			return cmp.Or(cmpIndex(a.ParentIndexNumber, b.ParentIndexNumber), cmpIndex(a.IndexNumber, b.IndexNumber))
		})
		return kindEpisode, episodes, nil
	case kindAlbum, kindArtist:
		q := url.Values{
			"Recursive":        {"true"},
			"IncludeItemTypes": {typeAudio},
			"Fields":           {itemFields},
			"SortBy":           {"Album,ParentIndexNumber,IndexNumber,SortName"},
		}
		if kind == kindAlbum {
			q.Set("ParentId", item.ID)
		} else {
			q.Set("ArtistIds", item.ID)
		}
		tracks, err := collectAll(ctx, c.ListItems, q)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get tracks for jellyfin %s %s: %w", kind.label, item.ID, err)
		}
		slices.SortStableFunc(tracks, func(a, b Item) int {
			return cmp.Or(
				cmp.Compare(a.Album, b.Album),
				cmp.Compare(a.AlbumID, b.AlbumID),
				cmpIndex(a.ParentIndexNumber, b.ParentIndexNumber),
				cmpIndex(a.IndexNumber, b.IndexNumber),
			)
		})
		return kindSong, tracks, nil
	default:
		return kind, []Item{*item}, nil
	}
}

// cmpIndex orders missing season, disc or track numbers first.
func cmpIndex(a, b *int) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return cmp.Compare(*a, *b)
	}
}
