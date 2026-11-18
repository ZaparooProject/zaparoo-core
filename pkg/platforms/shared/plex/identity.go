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
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

// identityProvider maps a card query key to the scheme Plex uses for the
// same identity in its Guid list.
type identityProvider struct {
	key    string
	scheme string
}

// identityProviders is the order identities are written to a card and tried
// when the stored rating key no longer resolves.
var identityProviders = []identityProvider{
	{key: "tmdb", scheme: "tmdb"},
	{key: "imdb", scheme: "imdb"},
	{key: "tvdb", scheme: "tvdb"},
	{key: "mb", scheme: "mbid"},
}

// itemRef is what a card path says about a library item.
type itemRef struct {
	IDs       map[string]string
	RatingKey string
	Name      string
}

// itemIdentities collects the external identities of an item by card key.
func itemIdentities(md *Metadata) map[string]string {
	ids := make(map[string]string, len(identityProviders))
	for _, g := range md.GUIDs {
		scheme, value, ok := strings.Cut(g.ID, "://")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, p := range identityProviders {
			if strings.EqualFold(scheme, p.scheme) {
				if _, seen := ids[p.key]; !seen {
					ids[p.key] = value
				}
			}
		}
	}
	return ids
}

// createItemPath builds the card path for an item: the rating key and name,
// then every external identity Plex holds for it so the card still finds the
// item after the server gives it a new rating key.
func createItemPath(kind *mediaKind, md *Metadata, name string) string {
	ids := itemIdentities(md)
	query := make([]string, 0, len(ids))
	for _, p := range identityProviders {
		if v, ok := ids[p.key]; ok {
			query = append(query, p.key+"="+url.QueryEscape(v))
		}
	}
	return virtualpath.CreateVirtualPathWithParams(kind.scheme, md.RatingKey, name, query)
}

// parseItemRef reads a card path of the given kind.
func parseItemRef(path string, kind *mediaKind) (itemRef, error) {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return itemRef{}, fmt.Errorf("failed to parse plex path: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, kind.scheme) {
		return itemRef{}, fmt.Errorf("%w: expected %s, got %s",
			virtualpath.ErrSchemeMismatch, kind.scheme, parsed.Scheme)
	}
	if !validKey(parsed.ID) {
		return itemRef{}, fmt.Errorf("invalid plex %s key %q", kind.typeName, parsed.ID)
	}
	params, err := virtualpath.ParseParams(path)
	if err != nil {
		return itemRef{}, fmt.Errorf("failed to parse plex identity: %w", err)
	}
	ids, err := identityParams(params)
	if err != nil {
		return itemRef{}, err
	}
	return itemRef{IDs: ids, RatingKey: parsed.ID, Name: parsed.Name}, nil
}

func identityParams(values url.Values) (map[string]string, error) {
	ids := map[string]string{}
	for _, p := range identityProviders {
		for key, list := range values {
			if !strings.EqualFold(key, p.key) {
				continue
			}
			for _, v := range list {
				v = strings.TrimSpace(v)
				if v == "" || virtualpath.ContainsControlChar(v) {
					continue
				}
				if existing, ok := ids[p.key]; ok && !strings.EqualFold(existing, v) {
					return nil, fmt.Errorf("conflicting %s identities in plex path", p.key)
				}
				ids[p.key] = v
			}
		}
	}
	return ids, nil
}

// identityMatch compares stored identities with an item's. It reports
// whether any identity agrees and whether any disagrees.
func identityMatch(stored map[string]string, md *Metadata) (matched, conflicted bool) {
	actual := itemIdentities(md)
	for key, want := range stored {
		got, ok := actual[key]
		if !ok {
			continue
		}
		if strings.EqualFold(got, want) {
			matched = true
		} else {
			conflicted = true
		}
	}
	return matched, conflicted
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// resolveItem finds the library item a card names. The stored rating key is
// tried first. A path without identities is the rating key and nothing more.
// A path with identities is checked against the item at that key, and when
// the key is gone or now holds something else, the item is found again by
// identity and then by exact name; anything but one match is an error.
func resolveItem(ctx context.Context, client PlexClient, kind *mediaKind, path string) (*Metadata, error) {
	ref, err := parseItemRef(path, kind)
	if err != nil {
		return nil, err
	}

	md, err := client.Metadata(ctx, ref.RatingKey)
	switch {
	case err == nil:
		if !strings.EqualFold(md.Type, kind.typeName) {
			if len(ref.IDs) == 0 {
				return nil, fmt.Errorf("plex item %s is a %s, not a %s", ref.RatingKey, md.Type, kind.typeName)
			}
			break
		}
		if len(ref.IDs) == 0 {
			return md, nil
		}
		matched, conflicted := identityMatch(ref.IDs, md)
		if matched && !conflicted {
			return md, nil
		}
		if !matched && !conflicted && (ref.Name == "" || sameName(displayName(kind, md), ref.Name)) {
			// The item at the key only shares the card's name. When the item
			// the card's identities name still exists elsewhere, that is it.
			moved, guidErr := findMovedByGUID(ctx, client, kind, ref)
			switch {
			case guidErr == nil && moved != nil:
				return moved, nil
			case guidErr == nil, errors.Is(guidErr, errAmbiguousGUID):
				return md, nil
			default:
				return nil, guidErr
			}
		}
	case errors.Is(err, ErrNotFound):
		if len(ref.IDs) == 0 {
			return nil, fmt.Errorf("plex %s %q is no longer available", kind.typeName, refLabel(ref))
		}
	default:
		return nil, fmt.Errorf("failed to look up plex %s %s: %w", kind.typeName, ref.RatingKey, err)
	}

	return findMoved(ctx, client, kind, ref)
}

func refLabel(ref itemRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	return ref.RatingKey
}

var errAmbiguousGUID = errors.New("several plex items hold the identity")

func kindSections(ctx context.Context, client PlexClient, kind *mediaKind) ([]Section, error) {
	sections, err := client.Sections(ctx)
	if err != nil {
		return nil, err //nolint:wrapcheck // the client error already names the request
	}
	return sectionsOfType(sections, kind.sectionType), nil
}

// findMoved finds an item whose stored rating key no longer names it.
func findMoved(ctx context.Context, client PlexClient, kind *mediaKind, ref itemRef) (*Metadata, error) {
	sections, err := kindSections(ctx, client, kind)
	if err != nil {
		return nil, err
	}
	md, err := findByGUID(ctx, client, kind, ref, sections)
	if err != nil || md != nil {
		return md, err
	}
	return findByName(ctx, client, kind, ref, sections)
}

// findMovedByGUID finds the item the card's identities name, or nil when
// none does.
func findMovedByGUID(ctx context.Context, client PlexClient, kind *mediaKind, ref itemRef) (*Metadata, error) {
	sections, err := kindSections(ctx, client, kind)
	if err != nil {
		return nil, err
	}
	return findByGUID(ctx, client, kind, ref, sections)
}

// findByGUID searches for the card's identities in turn and returns the one
// item holding the first identity any item has, or nil when none does.
func findByGUID(
	ctx context.Context, client PlexClient, kind *mediaKind, ref itemRef, sections []Section,
) (*Metadata, error) {
	for _, p := range identityProviders {
		value, ok := ref.IDs[p.key]
		if !ok {
			continue
		}
		guid := p.scheme + "://" + value
		filter := url.Values{}
		filter.Set("guid", guid)
		var matches []Metadata
		for _, s := range sections {
			err := client.Items(ctx, s.Key, kind.plexType, filter, func(items []Metadata) error {
				for i := range items {
					// A server that does not filter by guid returns everything;
					// only items that hold the identity count.
					matched, conflicted := identityMatch(ref.IDs, &items[i])
					if matched && !conflicted && hasGUID(&items[i], guid) {
						matches = appendUnique(matches, &items[i])
					}
				}
				return nil
			})
			if err != nil {
				return nil, err //nolint:wrapcheck // the client error already names the request
			}
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			return &matches[0], nil
		default:
			if md := pickIdentified(kind, ref, matches); md != nil {
				return md, nil
			}
			return nil, fmt.Errorf("%w: multiple plex %ss match %s", errAmbiguousGUID, kind.typeName, guid)
		}
	}
	return nil, nil //nolint:nilnil // no item holds any of the identities
}

// findByName walks the whole library for the card's item: one holding the
// card's identities, which a server that filters by guid only on an item's
// primary Plex guid does not return to findByGUID, or else the one item with
// the card's exact name and no conflicting identity.
func findByName(
	ctx context.Context, client PlexClient, kind *mediaKind, ref itemRef, sections []Section,
) (*Metadata, error) {
	if strings.TrimSpace(ref.Name) == "" && len(ref.IDs) == 0 {
		return nil, fmt.Errorf("plex %s %s is no longer available", kind.typeName, ref.RatingKey)
	}
	var identified, matches []Metadata
	for _, s := range sections {
		err := client.Items(ctx, s.Key, kind.plexType, nil, func(items []Metadata) error {
			for i := range items {
				matched, conflicted := identityMatch(ref.IDs, &items[i])
				switch {
				case conflicted:
				case matched:
					identified = appendUnique(identified, &items[i])
				case ref.Name != "" && sameName(displayName(kind, &items[i]), ref.Name):
					matches = appendUnique(matches, &items[i])
				}
			}
			return nil
		})
		if err != nil {
			return nil, err //nolint:wrapcheck // the client error already names the request
		}
	}
	if len(identified) > 0 {
		if md := pickIdentified(kind, ref, identified); md != nil {
			return md, nil
		}
		return nil, fmt.Errorf("%w: multiple plex %ss match %q", errAmbiguousGUID, kind.typeName, refLabel(ref))
	}
	if strings.TrimSpace(ref.Name) == "" {
		return nil, fmt.Errorf("plex %s %s is no longer available", kind.typeName, ref.RatingKey)
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, fmt.Errorf("plex %s %q is no longer available", kind.typeName, ref.Name)
	default:
		return nil, fmt.Errorf("multiple plex %ss match title %q", kind.typeName, ref.Name)
	}
}

func sectionsOfType(sections []Section, sectionType string) []Section {
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		if strings.EqualFold(s.Type, sectionType) {
			out = append(out, s)
		}
	}
	return out
}

func hasGUID(md *Metadata, guid string) bool {
	for _, g := range md.GUIDs {
		if strings.EqualFold(strings.TrimSpace(g.ID), guid) {
			return true
		}
	}
	return false
}

func appendUnique(list []Metadata, md *Metadata) []Metadata {
	for i := range list {
		if list[i].RatingKey == md.RatingKey {
			return list
		}
	}
	return append(list, *md)
}

// pickIdentified returns the one item holding the most of the card's
// identities, with the card's name breaking a tie, or nil when the items
// still tie.
func pickIdentified(kind *mediaKind, ref itemRef, items []Metadata) *Metadata {
	var best []Metadata
	bestCount := 0
	for i := range items {
		n := agreeingIdentities(ref.IDs, &items[i])
		switch {
		case n > bestCount:
			best, bestCount = append(best[:0], items[i]), n
		case n == bestCount:
			best = append(best, items[i])
		}
	}
	if len(best) == 1 {
		return &best[0]
	}
	if named := matchingName(kind, ref.Name, best); len(named) == 1 {
		return &named[0]
	}
	return nil
}

// agreeingIdentities counts the stored identities an item also holds.
func agreeingIdentities(stored map[string]string, md *Metadata) int {
	actual := itemIdentities(md)
	n := 0
	for key, want := range stored {
		if got, ok := actual[key]; ok && strings.EqualFold(got, want) {
			n++
		}
	}
	return n
}

func matchingName(kind *mediaKind, name string, items []Metadata) []Metadata {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	var out []Metadata
	for i := range items {
		if sameName(displayName(kind, &items[i]), name) {
			out = append(out, items[i])
		}
	}
	return out
}
