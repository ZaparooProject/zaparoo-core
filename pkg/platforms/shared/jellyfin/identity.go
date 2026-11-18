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
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// Jellyfin item types.
const (
	typeMovie   = "Movie"
	typeEpisode = "Episode"
	typeSeries  = "Series"
	typeAudio   = "Audio"
	typeAlbum   = "MusicAlbum"
	typeArtist  = "MusicArtist"
)

// itemFields are the optional fields every item query asks for, enough to
// name an item and record its identity.
const itemFields = "ProviderIds,ProductionYear,ParentIndexNumber,IndexNumber,SeriesName"

// providerKey maps a card query key to the provider name Jellyfin reports.
type providerKey struct {
	key      string
	jellyfin string
}

var (
	videoProviders  = []providerKey{{"tmdb", "Tmdb"}, {"imdb", "Imdb"}, {"tvdb", "Tvdb"}}
	songProviders   = []providerKey{{"mb_track", "MusicBrainzTrack"}}
	albumProviders  = []providerKey{{"mb_album", "MusicBrainzAlbum"}}
	artistProviders = []providerKey{{"mb_artist", "MusicBrainzArtist"}}
)

// mediaKind describes one of the six kinds of library item Core plays.
type mediaKind struct {
	name       func(*Item) string
	searchTerm func(string) string
	scheme     string
	itemType   string
	launcherID string
	systemID   string
	label      string
	providers  []providerKey
}

var (
	kindMovie = &mediaKind{
		scheme: shared.SchemeJellyfinMovie, itemType: typeMovie, label: "movie",
		launcherID: shared.LauncherJellyfinMovie, systemID: systemdefs.SystemMovie,
		providers: videoProviders, name: movieName, searchTerm: stripYear,
	}
	kindEpisode = &mediaKind{
		scheme: shared.SchemeJellyfinEpisode, itemType: typeEpisode, label: "episode",
		launcherID: shared.LauncherJellyfinTVEpisode, systemID: systemdefs.SystemTVEpisode,
		providers: videoProviders, name: episodeName, searchTerm: lastPart,
	}
	kindShow = &mediaKind{
		scheme: shared.SchemeJellyfinShow, itemType: typeSeries, label: "TV show",
		launcherID: shared.LauncherJellyfinTVShow, systemID: systemdefs.SystemTVShow,
		providers: videoProviders, name: plainName, searchTerm: strings.TrimSpace,
	}
	kindSong = &mediaKind{
		scheme: shared.SchemeJellyfinSong, itemType: typeAudio, label: "song",
		launcherID: shared.LauncherJellyfinSong, systemID: systemdefs.SystemMusicTrack,
		providers: songProviders, name: songName, searchTerm: lastPart,
	}
	kindAlbum = &mediaKind{
		scheme: shared.SchemeJellyfinAlbum, itemType: typeAlbum, label: "album",
		launcherID: shared.LauncherJellyfinAlbum, systemID: systemdefs.SystemMusicAlbum,
		providers: albumProviders, name: albumName,
		searchTerm: func(s string) string { return lastPart(stripYear(s)) },
	}
	kindArtist = &mediaKind{
		scheme: shared.SchemeJellyfinArtist, itemType: typeArtist, label: "artist",
		launcherID: shared.LauncherJellyfinArtist, systemID: systemdefs.SystemMusicArtist,
		providers: artistProviders, name: plainName, searchTerm: strings.TrimSpace,
	}
)

func plainName(item *Item) string { return item.Name }

// movieName matches the "Title (Year)" labels Kodi gives movies.
func movieName(item *Item) string {
	if item.ProductionYear > 0 {
		return fmt.Sprintf("%s (%d)", item.Name, item.ProductionYear)
	}
	return item.Name
}

func episodeName(item *Item) string {
	title := item.Name
	if item.ParentIndexNumber != nil && item.IndexNumber != nil {
		title = fmt.Sprintf("S%02dE%02d - %s", *item.ParentIndexNumber, *item.IndexNumber, item.Name)
	}
	return joinNonEmpty(item.SeriesName, title)
}

func songName(item *Item) string {
	artist := item.AlbumArtist
	if len(item.Artists) > 0 && item.Artists[0] != "" {
		artist = item.Artists[0]
	}
	return joinNonEmpty(artist, item.Album, item.Name)
}

func albumName(item *Item) string {
	name := joinNonEmpty(item.AlbumArtist, item.Name)
	if item.ProductionYear > 0 {
		name = fmt.Sprintf("%s (%d)", name, item.ProductionYear)
	}
	return name
}

func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " - ")
}

var yearSuffix = regexp.MustCompile(`\s*\(\d{4}\)$`)

// stripYear and lastPart recover a search term from a stored display name.
// Jellyfin matches the term as a substring of the title, so a term that is
// only part of a title containing " - " still finds it.
func stripYear(name string) string {
	return strings.TrimSpace(yearSuffix.ReplaceAllString(strings.TrimSpace(name), ""))
}

func lastPart(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, " - "); i >= 0 && strings.TrimSpace(name[i+3:]) != "" {
		return strings.TrimSpace(name[i+3:])
	}
	return name
}

// reference is what a card's virtual path says about an item: the ID it had
// when indexed, its display name and the provider IDs the server reported.
type reference struct {
	ProviderIDs map[string]string
	ID          string
	Name        string
}

// itemPath builds an item's virtual path. Jellyfin item IDs change when a
// library is rebuilt, so the path also carries the item's provider IDs, which
// let a card find the item again.
func itemPath(kind *mediaKind, item *Item, name string) string {
	query := make([]string, 0, len(kind.providers))
	for _, p := range kind.providers {
		value := strings.TrimSpace(lookupProviderID(item.ProviderIDs, p.jellyfin))
		if !usableProviderID(value) {
			continue
		}
		query = append(query, p.key+"="+url.QueryEscape(value))
	}
	return virtualpath.CreateVirtualPathWithParams(kind.scheme, item.ID, name, query)
}

// usableProviderID rejects values that cannot be sent in Jellyfin's
// comma-separated provider filter.
func usableProviderID(value string) bool {
	return value != "" && !strings.ContainsAny(value, ",") && !virtualpath.ContainsControlChar(value)
}

func lookupProviderID(ids map[string]string, provider string) string {
	for k, v := range ids {
		if strings.EqualFold(k, provider) {
			return v
		}
	}
	return ""
}

// parseReference reads a card's virtual path. Paths without a query, from
// before provider IDs were recorded, carry only the ID and name.
func parseReference(path string, kind *mediaKind) (reference, error) {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return reference{}, fmt.Errorf("failed to parse jellyfin %s path: %w", kind.label, err)
	}
	if !strings.EqualFold(parsed.Scheme, kind.scheme) {
		return reference{}, fmt.Errorf("%w: expected %s, got %s",
			virtualpath.ErrSchemeMismatch, kind.scheme, parsed.Scheme)
	}
	if !validItemID(parsed.ID) {
		return reference{}, fmt.Errorf("invalid jellyfin item id %q", parsed.ID)
	}
	params, err := virtualpath.ParseParams(path)
	if err != nil {
		return reference{}, fmt.Errorf("failed to parse jellyfin identity: %w", err)
	}
	ids, err := parseProviderIDs(params, kind)
	if err != nil {
		return reference{}, err
	}
	return reference{ID: parsed.ID, Name: parsed.Name, ProviderIDs: ids}, nil
}

func parseProviderIDs(values url.Values, kind *mediaKind) (map[string]string, error) {
	ids := make(map[string]string, len(kind.providers))
	for _, p := range kind.providers {
		for key, vals := range values {
			if !strings.EqualFold(key, p.key) {
				continue
			}
			for _, v := range vals {
				v = strings.TrimSpace(v)
				if !usableProviderID(v) {
					continue
				}
				if existing, ok := ids[p.key]; ok && !strings.EqualFold(existing, v) {
					return nil, fmt.Errorf("conflicting %s identities in jellyfin path", p.key)
				}
				ids[p.key] = v
			}
		}
	}
	return ids, nil
}

// resolveItem finds the item a card names. The stored ID is tried first; if
// the server no longer has it, the provider IDs and then the exact display
// name must pick out exactly one item of the same type.
func resolveItem(ctx context.Context, c Client, kind *mediaKind, ref reference) (*Item, error) {
	item, err := c.GetItem(ctx, ref.ID)
	switch {
	case err == nil && strings.EqualFold(item.Type, kind.itemType) && !identityConflicts(kind, ref.ProviderIDs, item):
		return item, nil
	case err == nil, errors.Is(err, ErrItemNotFound):
	default:
		return nil, fmt.Errorf("failed to get jellyfin %s: %w", kind.label, err)
	}

	if len(ref.ProviderIDs) > 0 {
		matches, listErr := providerMatches(ctx, c, kind, ref)
		if listErr != nil {
			return nil, listErr
		}
		switch len(matches) {
		case 0:
		case 1:
			return &matches[0], nil
		default:
			named := matchingByName(kind, ref.Name, matches)
			if len(named) == 1 {
				return &named[0], nil
			}
			return nil, fmt.Errorf("multiple jellyfin %ss match the card's provider IDs", kind.label)
		}
	}

	if strings.TrimSpace(ref.Name) == "" {
		return nil, fmt.Errorf("jellyfin %s %s is no longer available", kind.label, ref.ID)
	}
	candidates, err := searchCandidates(ctx, c, kind, ref.Name)
	if err != nil {
		return nil, err
	}
	named := matchingByName(kind, ref.Name, candidates)
	switch len(named) {
	case 1:
		return &named[0], nil
	case 0:
		return nil, fmt.Errorf("jellyfin %s %q is no longer available", kind.label, ref.Name)
	default:
		return nil, fmt.Errorf("multiple jellyfin %ss are named %q", kind.label, ref.Name)
	}
}

// providerMatches asks the server for items with any of the card's
// provider IDs and keeps those matchingByIdentity accepts. Album artists
// cannot be filtered by provider, so they are searched by name and then
// matched by provider. Each page is matched as it arrives, so a server that
// ignores the filter and returns its whole library is not held in memory.
func providerMatches(ctx context.Context, c Client, kind *mediaKind, ref reference) ([]Item, error) {
	var list pageLister = c.ListItems
	var q url.Values
	if kind == kindArtist {
		term := kind.searchTerm(ref.Name)
		if term == "" {
			return nil, nil
		}
		list = kindLister(c, kind)
		q = kindQuery(kind)
		q.Set("searchTerm", term)
	} else {
		ids := make([]string, 0, len(ref.ProviderIDs))
		for _, p := range kind.providers {
			if v, ok := ref.ProviderIDs[p.key]; ok {
				ids = append(ids, p.jellyfin+"."+v)
			}
		}
		q = url.Values{
			"Recursive":           {"true"},
			"IncludeItemTypes":    {kind.itemType},
			"AnyProviderIdEquals": {strings.Join(ids, ",")},
			"Fields":              {itemFields},
		}
	}
	var matches []Item
	err := eachPage(ctx, list, q, func(page []Item) {
		matches = append(matches, matchingByIdentity(kind, ref.ProviderIDs, page)...)
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func searchCandidates(ctx context.Context, c Client, kind *mediaKind, name string) ([]Item, error) {
	term := kind.searchTerm(name)
	if term == "" {
		return nil, nil
	}
	q := kindQuery(kind)
	q.Set("searchTerm", term)
	return collectAll(ctx, kindLister(c, kind), q)
}

// matchingByIdentity keeps items that share a provider ID with the card and
// contradict none. The server's filter is not trusted to have been applied.
func matchingByIdentity(kind *mediaKind, ids map[string]string, items []Item) []Item {
	var matches []Item
	for i := range items {
		item := &items[i]
		if !strings.EqualFold(item.Type, kind.itemType) {
			continue
		}
		if matched, conflicted := compareIdentity(kind, ids, item); matched && !conflicted {
			matches = append(matches, *item)
		}
	}
	return matches
}

// compareIdentity reports whether any provider ID the card and the item both
// hold agrees, and whether any disagrees.
func compareIdentity(kind *mediaKind, ids map[string]string, item *Item) (matched, conflicted bool) {
	for _, p := range kind.providers {
		want, ok := ids[p.key]
		if !ok {
			continue
		}
		have := strings.TrimSpace(lookupProviderID(item.ProviderIDs, p.jellyfin))
		if have == "" {
			continue
		}
		if strings.EqualFold(have, want) {
			matched = true
		} else {
			conflicted = true
		}
	}
	return matched, conflicted
}

// identityConflicts reports whether the item holds a provider ID that
// contradicts the card's, which makes it a different item.
func identityConflicts(kind *mediaKind, ids map[string]string, item *Item) bool {
	_, conflicted := compareIdentity(kind, ids, item)
	return conflicted
}

func matchingByName(kind *mediaKind, name string, items []Item) []Item {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var matches []Item
	for i := range items {
		item := &items[i]
		if strings.EqualFold(item.Type, kind.itemType) &&
			strings.EqualFold(strings.TrimSpace(kind.name(item)), name) {
			matches = append(matches, *item)
		}
	}
	return matches
}
