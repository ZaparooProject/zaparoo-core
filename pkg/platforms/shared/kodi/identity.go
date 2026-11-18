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
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// Library IDs in Kodi change whenever its database is rebuilt, so a card that
// only stored the ID would play the wrong item or nothing. Movie and episode
// paths also carry the provider IDs Kodi scraped, and the episode's show and
// position, so a card can find its item again.
const (
	resolveTimeout      = 30 * time.Second
	episodeQuerySeason  = "season"
	episodeQueryEpisode = "episode"
	episodeShowPrefix   = "show_"
)

type movieReference struct {
	UniqueIDs map[string]string
	Name      string
	ID        int
}

type episodeReference struct {
	UniqueIDs     map[string]string
	ShowUniqueIDs map[string]string
	Name          string
	ID            int
	Season        int
	Episode       int
}

// identityQuery encodes the provider IDs as query parameters, each key given
// the prefix.
func identityQuery(prefix string, uniqueIDs map[string]string) []string {
	query := make([]string, 0, len(tvShowIdentityProviders))
	for _, provider := range tvShowIdentityProviders {
		value, found := lookupTVShowUniqueID(uniqueIDs, provider)
		if !found || strings.TrimSpace(value) == "" {
			continue
		}
		query = append(query, prefix+provider+"="+url.QueryEscape(strings.TrimSpace(value)))
	}
	return query
}

// identityFromQuery reads the provider IDs stored under the prefixed keys.
// Repeated keys must agree.
func identityFromQuery(values url.Values, prefix string) (map[string]string, error) {
	uniqueIDs := make(map[string]string, len(tvShowIdentityProviders))
	for _, provider := range tvShowIdentityProviders {
		for key, queryValues := range values {
			if !strings.EqualFold(key, prefix+provider) {
				continue
			}
			for _, value := range queryValues {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				if existing, found := uniqueIDs[provider]; found && !strings.EqualFold(existing, value) {
					return nil, fmt.Errorf("conflicting %s identities", prefix+provider)
				}
				uniqueIDs[provider] = value
			}
		}
	}
	return uniqueIDs, nil
}

// identityAgreement compares a card's provider IDs with an item's. matched is
// true when a provider both carry has the same value; conflicted is true when
// one has a different value.
func identityAgreement(card, item map[string]string) (matched, conflicted bool) {
	for provider, expected := range card {
		actual, found := lookupTVShowUniqueID(item, provider)
		if !found || strings.TrimSpace(actual) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expected)) {
			matched = true
		} else {
			conflicted = true
		}
	}
	return matched, conflicted
}

// libraryPath is a parsed Kodi library virtual path.
type libraryPath struct {
	Query url.Values
	Name  string
	ID    int
}

func parseLibraryPath(path, scheme string) (libraryPath, error) {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return libraryPath{}, fmt.Errorf("failed to parse %s path: %w", scheme, err)
	}
	if !strings.EqualFold(parsed.Scheme, scheme) {
		return libraryPath{}, fmt.Errorf("path scheme mismatch: expected %s, got %s", scheme, parsed.Scheme)
	}
	id, err := strconv.Atoi(parsed.ID)
	if err != nil {
		return libraryPath{}, fmt.Errorf("failed to parse ID %q: %w", parsed.ID, err)
	}
	values, err := virtualpath.ParseParams(path)
	if err != nil {
		// Paths written before identities were stored carry only the ID, and
		// stray query text never stopped them launching.
		log.Debug().Err(err).Str("path", path).Msg("kodi: ignoring unreadable path parameters")
		values = url.Values{}
	}
	return libraryPath{Query: values, Name: parsed.Name, ID: id}, nil
}

func createMovieVirtualPath(movie Movie) string {
	return virtualpath.CreateVirtualPathWithParams(
		shared.SchemeKodiMovie, strconv.Itoa(movie.ID), movie.Label, identityQuery("", movie.UniqueIDs))
}

func parseMovieReference(path string) (movieReference, error) {
	parsed, err := parseLibraryPath(path, shared.SchemeKodiMovie)
	if err != nil {
		return movieReference{}, err
	}
	uniqueIDs, err := identityFromQuery(parsed.Query, "")
	if err != nil {
		return movieReference{}, err
	}
	return movieReference{UniqueIDs: uniqueIDs, Name: parsed.Name, ID: parsed.ID}, nil
}

func createEpisodeVirtualPath(show TVShow, episode *Episode) string {
	query := identityQuery("", episode.UniqueIDs)
	if episode.Season > 0 || episode.Episode > 0 {
		query = append(query,
			episodeQuerySeason+"="+strconv.Itoa(episode.Season),
			episodeQueryEpisode+"="+strconv.Itoa(episode.Episode))
	}
	query = append(query, identityQuery(episodeShowPrefix, show.UniqueIDs)...)
	return virtualpath.CreateVirtualPathWithParams(
		shared.SchemeKodiEpisode, strconv.Itoa(episode.ID), episodeName(show, episode), query)
}

func parseEpisodeReference(path string) (episodeReference, error) {
	parsed, err := parseLibraryPath(path, shared.SchemeKodiEpisode)
	if err != nil {
		return episodeReference{}, err
	}
	uniqueIDs, err := identityFromQuery(parsed.Query, "")
	if err != nil {
		return episodeReference{}, err
	}
	showIDs, err := identityFromQuery(parsed.Query, episodeShowPrefix)
	if err != nil {
		return episodeReference{}, err
	}
	ref := episodeReference{UniqueIDs: uniqueIDs, ShowUniqueIDs: showIDs, Name: parsed.Name, ID: parsed.ID}
	season, seasonErr := strconv.Atoi(parsed.Query.Get(episodeQuerySeason))
	number, numberErr := strconv.Atoi(parsed.Query.Get(episodeQueryEpisode))
	if seasonErr == nil && numberErr == nil && season >= 0 && number > 0 {
		ref.Season = season
		ref.Episode = number
	}
	return ref, nil
}

// hasIdentity reports whether the card stored anything beyond the ID. Cards
// written before identities were stored keep resolving by ID alone.
func (r *episodeReference) hasIdentity() bool {
	return len(r.UniqueIDs) > 0 || len(r.ShowUniqueIDs) > 0 || r.Episode > 0
}

// resolveMovie finds the movie a kodi-movie path names, following it to its
// new ID when Kodi's library has been rebuilt.
func resolveMovie(ctx context.Context, client KodiClient, path string) (int, error) {
	ref, err := parseMovieReference(path)
	if err != nil {
		return 0, err
	}
	movie, err := client.GetMovieDetails(ctx, ref.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to look up Kodi movie %d: %w", ref.ID, err)
	}
	if len(ref.UniqueIDs) == 0 && movie != nil {
		return ref.ID, nil
	}
	// A movie at the stored ID that only shares the card's title is used
	// only when no movie holds the card's provider ID.
	keptByName := false
	if movie != nil {
		matched, conflicted := identityAgreement(ref.UniqueIDs, movie.UniqueIDs)
		if matched && !conflicted {
			return ref.ID, nil
		}
		keptByName = !matched && !conflicted && equalTVShowTitle(ref.Name, movie.Label)
	}

	movies, err := client.GetMovies(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list Kodi movies: %w", err)
	}
	if len(ref.UniqueIDs) > 0 {
		var matches []Movie
		for _, m := range movies {
			if matched, conflicted := identityAgreement(ref.UniqueIDs, m.UniqueIDs); matched && !conflicted {
				matches = append(matches, m)
			}
		}
		if len(matches) == 1 {
			return matches[0].ID, nil
		}
		if len(matches) > 1 {
			if id, ok := uniqueByName(matches, ref.Name, movieLabel); ok {
				return id, nil
			}
			return 0, fmt.Errorf("multiple Kodi movies match %q", ref.Name)
		}
	}
	if keptByName {
		return ref.ID, nil
	}
	eligible := make([]Movie, 0, len(movies))
	for _, m := range movies {
		if _, conflicted := identityAgreement(ref.UniqueIDs, m.UniqueIDs); !conflicted {
			eligible = append(eligible, m)
		}
	}
	if id, ok := uniqueByName(eligible, ref.Name, movieLabel); ok {
		return id, nil
	}
	return 0, fmt.Errorf("kodi movie %q is no longer available", ref.Name)
}

// resolveEpisode finds the episode a kodi-episode path names, following it
// to its new ID when Kodi's library has been rebuilt.
func resolveEpisode(ctx context.Context, client KodiClient, path string) (int, error) {
	ref, err := parseEpisodeReference(path)
	if err != nil {
		return 0, err
	}
	episode, err := client.GetEpisodeDetails(ctx, ref.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to look up Kodi episode %d: %w", ref.ID, err)
	}
	var shows []TVShow
	listShows := func() error {
		if shows != nil {
			return nil
		}
		var listErr error
		if shows, listErr = client.GetTVShows(ctx); listErr != nil {
			return fmt.Errorf("failed to list Kodi TV shows: %w", listErr)
		}
		if shows == nil {
			shows = []TVShow{}
		}
		return nil
	}
	if episode != nil {
		if !ref.hasIdentity() {
			return ref.ID, nil
		}
		matched, conflicted := identityAgreement(ref.UniqueIDs, episode.UniqueIDs)
		switch {
		case matched && !conflicted:
			return ref.ID, nil
		case !conflicted && sameEpisode(&ref, episode, episode.ShowTitle+" - "+episode.Label):
			// Only the title or place matched, so the episode must also
			// belong to the card's show.
			if len(ref.ShowUniqueIDs) == 0 {
				return ref.ID, nil
			}
			if err := listShows(); err != nil {
				return 0, err
			}
			if !showConflicts(ref.ShowUniqueIDs, shows, episode.TVShowID) {
				return ref.ID, nil
			}
		}
	}

	if err := listShows(); err != nil {
		return 0, err
	}
	var candidates []TVShow
	if len(ref.ShowUniqueIDs) > 0 {
		candidates = matchingTVShowsByIdentity(ref.ShowUniqueIDs, shows)
	}
	if len(candidates) == 0 {
		for _, show := range shows {
			if strings.HasPrefix(strings.ToLower(ref.Name), strings.ToLower(show.Label)+" - ") {
				candidates = append(candidates, show)
			}
		}
	}

	var byIdentity, byNumber, byName []int
	for _, show := range candidates {
		episodes, err := client.GetEpisodes(ctx, show.ID)
		if err != nil {
			return 0, fmt.Errorf("failed to list episodes for Kodi show %d: %w", show.ID, err)
		}
		for _, ep := range episodes {
			matched, conflicted := identityAgreement(ref.UniqueIDs, ep.UniqueIDs)
			if conflicted {
				continue
			}
			if matched {
				byIdentity = append(byIdentity, ep.ID)
			}
			if ref.Episode > 0 && ep.Season == ref.Season && ep.Episode == ref.Episode {
				byNumber = append(byNumber, ep.ID)
			}
			if equalTVShowTitle(episodeName(show, &ep), ref.Name) {
				byName = append(byName, ep.ID)
			}
		}
	}
	if len(byIdentity) == 0 && len(ref.UniqueIDs) > 0 {
		// The show may have been renamed or lost its provider IDs, so look
		// for the episode's own provider ID across the whole library.
		episodes, err := client.GetEpisodes(ctx, allTVShows)
		if err != nil {
			return 0, fmt.Errorf("failed to list Kodi episodes: %w", err)
		}
		for _, ep := range episodes {
			if matched, conflicted := identityAgreement(ref.UniqueIDs, ep.UniqueIDs); matched && !conflicted {
				byIdentity = append(byIdentity, ep.ID)
			}
		}
	}
	for _, ids := range [][]int{byIdentity, byNumber, byName} {
		if len(ids) == 1 {
			return ids[0], nil
		}
		if len(ids) > 1 {
			return 0, fmt.Errorf("multiple Kodi episodes match %q", ref.Name)
		}
	}
	return 0, fmt.Errorf("kodi episode %q is no longer available", ref.Name)
}

// allTVShows asks GetEpisodes for every episode in the library.
const allTVShows = -1

// showConflicts reports whether the show with the given ID holds provider
// IDs that contradict the card's show identity. An unknown show does not.
func showConflicts(cardShowIDs map[string]string, shows []TVShow, showID int) bool {
	for _, show := range shows {
		if show.ID == showID {
			_, conflicted := identityAgreement(cardShowIDs, show.UniqueIDs)
			return conflicted
		}
	}
	return false
}

// sameEpisode decides whether the episode at a card's stored ID still has
// the card's title, or the same place in the same-titled show.
func sameEpisode(ref *episodeReference, episode *Episode, name string) bool {
	if equalTVShowTitle(name, ref.Name) {
		return true
	}
	// A retitled episode is still the same one if it holds the same place in
	// the same show.
	return ref.Episode > 0 && episode.Season == ref.Season && episode.Episode == ref.Episode &&
		strings.HasPrefix(strings.ToLower(ref.Name), strings.ToLower(episode.ShowTitle)+" - ")
}

func movieLabel(m Movie) (id int, name string) { return m.ID, m.Label }

// uniqueByName returns the ID of the only item whose name equals name.
func uniqueByName[T any](items []T, name string, label func(T) (int, string)) (int, bool) {
	if strings.TrimSpace(name) == "" {
		return 0, false
	}
	found := 0
	count := 0
	for _, item := range items {
		id, itemName := label(item)
		if equalTVShowTitle(itemName, name) {
			found = id
			count++
		}
	}
	return found, count == 1
}

// resolveLibraryPath rewrites a library path to name the item's current ID,
// so the launch and the playback watcher both act on the item Kodi has now.
func resolveLibraryPath(
	client KodiClient,
	path, scheme string,
	resolve func(context.Context, KodiClient, string) (int, error),
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	id, err := resolve(ctx, client, path)
	if err != nil {
		return "", err
	}
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s path: %w", scheme, err)
	}
	return virtualpath.CreateVirtualPath(scheme, strconv.Itoa(id), parsed.Name), nil
}
