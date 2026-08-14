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

package kodi

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

const (
	tvShowProviderTVDB = "tvdb"
	tvShowProviderTMDB = "tmdb"
	tvShowProviderIMDb = "imdb"
)

var tvShowIdentityProviders = []string{
	tvShowProviderTVDB,
	tvShowProviderTMDB,
	tvShowProviderIMDb,
}

type tvShowReference struct {
	UniqueIDs map[string]string
	Name      string
	LegacyID  int
}

func createTVShowVirtualPath(show TVShow) string {
	path := virtualpath.CreateVirtualPath(shared.SchemeKodiShow, strconv.Itoa(show.ID), show.Label)
	query := make([]string, 0, len(tvShowIdentityProviders))
	for _, provider := range tvShowIdentityProviders {
		value, found := lookupTVShowUniqueID(show.UniqueIDs, provider)
		if !found || strings.TrimSpace(value) == "" {
			continue
		}
		query = append(query, provider+"="+url.QueryEscape(value))
	}
	if len(query) == 0 {
		return path
	}
	return path + "?" + strings.Join(query, "&")
}

func parseTVShowReference(path string) (tvShowReference, error) {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return tvShowReference{}, fmt.Errorf("failed to parse TV show path: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, shared.SchemeKodiShow) {
		return tvShowReference{}, fmt.Errorf(
			"tv show path scheme mismatch: expected %s, got %s",
			shared.SchemeKodiShow,
			parsed.Scheme,
		)
	}

	legacyID, err := strconv.Atoi(parsed.ID)
	if err != nil {
		return tvShowReference{}, fmt.Errorf("failed to parse show ID %q: %w", parsed.ID, err)
	}

	uniqueIDs, err := parseTVShowUniqueIDs(virtualpath.ParseURIComponents(path).Query)
	if err != nil {
		return tvShowReference{}, err
	}

	return tvShowReference{
		UniqueIDs: uniqueIDs,
		Name:      parsed.Name,
		LegacyID:  legacyID,
	}, nil
}

func parseTVShowUniqueIDs(rawQuery string) (map[string]string, error) {
	if rawQuery == "" {
		return map[string]string{}, nil
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TV show identity query: %w", err)
	}

	uniqueIDs := make(map[string]string, len(tvShowIdentityProviders))
	for _, provider := range tvShowIdentityProviders {
		var providerValues []string
		for key, queryValues := range values {
			if strings.EqualFold(key, provider) {
				providerValues = append(providerValues, queryValues...)
			}
		}

		for _, value := range providerValues {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if existing, found := uniqueIDs[provider]; found && !strings.EqualFold(existing, value) {
				return nil, fmt.Errorf("conflicting %s TV show identities", provider)
			}
			uniqueIDs[provider] = value
		}
	}
	return uniqueIDs, nil
}

func resolveTVShowID(reference tvShowReference, shows []TVShow) (int, error) {
	if len(reference.UniqueIDs) > 0 {
		identityMatches := matchingTVShowsByIdentity(reference.UniqueIDs, shows)
		switch len(identityMatches) {
		case 1:
			return identityMatches[0].ID, nil
		case 0:
			// Provider metadata can be absent or corrected over time. Exact title
			// matching below remains a safe fallback for uniquely named shows.
		default:
			titleMatches := matchingTVShowsByTitle(reference.Name, identityMatches)
			if len(titleMatches) == 1 {
				return titleMatches[0].ID, nil
			}
			return 0, errors.New("multiple Kodi TV shows match stored provider identities")
		}
	}

	for _, show := range shows {
		if show.ID != reference.LegacyID {
			continue
		}
		if reference.Name == "" || equalTVShowTitle(show.Label, reference.Name) {
			return show.ID, nil
		}
		break
	}

	titleMatches := matchingTVShowsByTitle(reference.Name, shows)
	if len(titleMatches) == 1 {
		return titleMatches[0].ID, nil
	}
	if len(titleMatches) > 1 {
		return 0, fmt.Errorf("multiple Kodi TV shows match title %q", reference.Name)
	}
	return 0, fmt.Errorf("kodi TV show %q is no longer available", reference.Name)
}

func matchingTVShowsByIdentity(uniqueIDs map[string]string, shows []TVShow) []TVShow {
	matches := make([]TVShow, 0, 1)
	for _, show := range shows {
		matched := false
		conflicted := false
		for provider, expected := range uniqueIDs {
			actual, found := lookupTVShowUniqueID(show.UniqueIDs, provider)
			if !found || strings.TrimSpace(actual) == "" {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expected)) {
				matched = true
			} else {
				conflicted = true
			}
		}
		if matched && !conflicted {
			matches = append(matches, show)
		}
	}
	return matches
}

func matchingTVShowsByTitle(name string, shows []TVShow) []TVShow {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	matches := make([]TVShow, 0, 1)
	for _, show := range shows {
		if equalTVShowTitle(show.Label, name) {
			matches = append(matches, show)
		}
	}
	return matches
}

func lookupTVShowUniqueID(uniqueIDs map[string]string, provider string) (string, bool) {
	for key, value := range uniqueIDs {
		if strings.EqualFold(key, provider) {
			return value, true
		}
	}
	return "", false
}

func equalTVShowTitle(first, second string) bool {
	return strings.EqualFold(strings.TrimSpace(first), strings.TrimSpace(second))
}
