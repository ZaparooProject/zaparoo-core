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

package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	// zapScriptVersionsPage is how many versions one media.versions read
	// asks for while a ZapScript list collects them all.
	zapScriptVersionsPage = 100
	// zapScriptFlaggedPage is how many results one media.search read asks
	// for while a ZapScript list collects every flagged game.
	zapScriptFlaggedPage = 1000
)

// ZapScriptLibrary lends the API's media library to ZapScript. Every method
// runs the API handler it stands for, so a command gets the same list, and
// makes the same edit, that the app does.
type ZapScriptLibrary struct {
	// Env returns the environment a handler runs in for a command. It
	// carries no client identity: the command's own bound and the device's
	// content rule already govern what ZapScript may do.
	Env func(ctx context.Context) requests.RequestEnv
}

var _ platforms.MediaLibrary = (*ZapScriptLibrary)(nil)

func (l *ZapScriptLibrary) call(
	ctx context.Context, handler func(requests.RequestEnv) (any, error), params any,
) (any, error) {
	env := l.Env(ctx)
	env.Context = ctx
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("encode library request: %w", err)
		}
		env.Params = raw
	}
	result, err := handler(env)
	if err != nil {
		var clientErr *models.ClientError
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("%w: %w", platforms.ErrLibraryRefused, err)
		}
		return nil, err
	}
	return result, nil
}

// refuseLocked refuses an edit on a locked device, the way the API refuses
// media.tags.update and decks.update to a caller the lock holds.
func (l *ZapScriptLibrary) refuseLocked(ctx context.Context) error {
	env := l.Env(ctx)
	if env.Kiosk.Locked() {
		return fmt.Errorf("%w: %w", platforms.ErrLibraryRefused, ErrKioskLocked)
	}
	return nil
}

// libraryItem names a result the way a list of several versions of one game
// can tell them apart: by the tags that make it a version, such as
// "Quest (us, en)".
func libraryItem(result *models.SearchResultMedia) platforms.LibraryItem {
	name := result.Name
	if len(result.DisambiguatingTags) > 0 {
		values := make([]string, 0, len(result.DisambiguatingTags))
		for i := range result.DisambiguatingTags {
			values = append(values, result.DisambiguatingTags[i].Tag)
		}
		name += " (" + strings.Join(values, ", ") + ")"
	}
	return platforms.LibraryItem{
		SystemID: result.System.ID,
		Name:     name,
		Path:     result.Path,
		MediaID:  result.MediaID,
	}
}

func refMediaID(ref platforms.MediaRef) *int64 {
	if ref.MediaID <= 0 {
		return nil
	}
	id := ref.MediaID
	return &id
}

// Flagged lists every game holding a user flag, reading each page of
// media.search in turn. Hidden media is left out of every list but the
// hidden list itself.
func (l *ZapScriptLibrary) Flagged(ctx context.Context, flag string) ([]platforms.LibraryItem, error) {
	limit, query, sort := zapScriptFlaggedPage, "", "name-asc"
	includeHidden := flag == string(tags.TagUserHidden)
	filter := []string{string(tags.TagTypeUser) + ":" + flag}
	if !includeHidden {
		// A search for a user flag shows hidden games too, the way the app's
		// own lists do, so the other lists ask for them to be left out.
		filter = append(filter, "-"+string(tags.TagTypeUser)+":"+string(tags.TagUserHidden))
	}
	params := models.SearchParams{
		Query:         &query,
		Tags:          &filter,
		Sort:          &sort,
		MaxResults:    &limit,
		IncludeHidden: includeHidden,
	}
	var items []platforms.LibraryItem
	for {
		result, err := l.call(ctx, HandleMediaSearch, &params)
		if err != nil {
			return nil, err
		}
		response, ok := result.(models.SearchResults)
		if !ok {
			return nil, fmt.Errorf("unexpected media.search result %T", result)
		}
		for i := range response.Results {
			items = append(items, libraryItem(&response.Results[i]))
		}
		if response.Pagination == nil || !response.Pagination.HasNextPage ||
			response.Pagination.NextCursor == nil {
			return items, nil
		}
		params.Cursor = response.Pagination.NextCursor
	}
}

// Recommended lists the active profile's recommendations.
func (l *ZapScriptLibrary) Recommended(ctx context.Context, section string) ([]platforms.LibraryItem, error) {
	var params models.MediaRecommendedParams
	if section == platforms.RecommendedQueue {
		layout := models.RecommendedLayoutQueue
		params.Layout = &layout
	}
	result, err := l.call(ctx, HandleMediaRecommended, &params)
	if err != nil {
		return nil, err
	}
	response, ok := result.(models.MediaRecommendedResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected media.recommended result %T", result)
	}
	var items []platforms.LibraryItem
	seen := make(map[int64]struct{})
	for i := range response.Sections {
		kind := response.Sections[i].Kind
		if section != "" && section != platforms.RecommendedQueue && !strings.EqualFold(kind, section) {
			continue
		}
		for j := range response.Sections[i].Items {
			item := &response.Sections[i].Items[j].SearchResultMedia
			if _, dup := seen[item.MediaID]; dup {
				continue
			}
			seen[item.MediaID] = struct{}{}
			items = append(items, libraryItem(item))
		}
	}
	return items, nil
}

// Similar lists media like ref.
func (l *ZapScriptLibrary) Similar(ctx context.Context, ref platforms.MediaRef) ([]platforms.LibraryItem, error) {
	params := models.MediaSimilarParams{MediaID: refMediaID(ref)}
	if params.MediaID == nil {
		params.System, params.Path = ref.SystemID, ref.Path
	}
	result, err := l.call(ctx, HandleMediaSimilar, &params)
	if err != nil {
		return nil, err
	}
	response, ok := result.(models.MediaSimilarResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected media.similar result %T", result)
	}
	items := make([]platforms.LibraryItem, 0, len(response.Results))
	for i := range response.Results {
		items = append(items, libraryItem(&response.Results[i].SearchResultMedia))
	}
	return items, nil
}

// Versions lists every version of ref's game, reading each page in turn.
func (l *ZapScriptLibrary) Versions(ctx context.Context, ref platforms.MediaRef) ([]platforms.LibraryItem, error) {
	limit := zapScriptVersionsPage
	params := models.MediaVersionsParams{MediaID: refMediaID(ref), MaxResults: &limit}
	if params.MediaID == nil {
		params.System, params.Path = ref.SystemID, ref.Path
	}
	var items []platforms.LibraryItem
	for {
		result, err := l.call(ctx, HandleMediaVersions, &params)
		if err != nil {
			return nil, err
		}
		response, ok := result.(models.MediaVersionsResponse)
		if !ok {
			return nil, fmt.Errorf("unexpected media.versions result %T", result)
		}
		for i := range response.Versions {
			items = append(items, libraryItem(&response.Versions[i].SearchResultMedia))
		}
		if response.Pagination == nil || !response.Pagination.HasNextPage ||
			response.Pagination.NextCursor == nil {
			return items, nil
		}
		params.Cursor = response.Pagination.NextCursor
	}
}

// MostPlayed lists the most played media of all time, leaving out hidden
// media the way every other list does.
func (l *ZapScriptLibrary) MostPlayed(ctx context.Context, systemID string) ([]platforms.LibraryItem, error) {
	var params models.MediaHistoryTopParams
	if systemID != "" {
		params.Systems = &[]string{systemID}
	}
	result, err := l.call(ctx, HandleMediaHistoryTop, &params)
	if err != nil {
		return nil, err
	}
	response, ok := result.(models.MediaHistoryTopResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected media.history.top result %T", result)
	}
	items := make([]platforms.LibraryItem, 0, len(response.Entries))
	for i := range response.Entries {
		entry := &response.Entries[i]
		if hasUserTag(entry.Tags, tags.TagUserHidden) {
			continue
		}
		items = append(items, platforms.LibraryItem{
			SystemID: entry.SystemID,
			Name:     entry.MediaName,
			Path:     entry.MediaPath,
			MediaID:  entry.MediaID,
		})
	}
	return items, nil
}

func hasUserTag(list []database.TagInfo, value tags.TagValue) bool {
	for i := range list {
		if list[i].Type == string(tags.TagTypeUser) && list[i].Tag == string(value) {
			return true
		}
	}
	return false
}

// SetUserFlag turns one user flag on or off for ref.
func (l *ZapScriptLibrary) SetUserFlag(ctx context.Context, ref platforms.MediaRef, flag string, on bool) error {
	if err := l.refuseLocked(ctx); err != nil {
		return err
	}
	params := models.MediaTagsUpdateParams{MediaID: refMediaID(ref)}
	if params.MediaID == nil {
		params.System, params.Path = ref.SystemID, ref.Path
	}
	tag := string(tags.TagTypeUser) + ":" + flag
	if on {
		params.Add = []string{tag}
	} else {
		params.Remove = []string{tag}
	}
	_, err := l.call(ctx, HandleMediaTagsUpdate, &params)
	return err
}

// EditDeck adds ref to a deck, or removes every item linked to it. A deck
// that already holds ref, or never did, is left as it is.
func (l *ZapScriptLibrary) EditDeck(
	ctx context.Context, deckID string, ref platforms.MediaRef, add bool,
) (bool, error) {
	if err := l.refuseLocked(ctx); err != nil {
		return false, err
	}
	env := l.Env(ctx)
	env.Context = ctx
	deck, err := loadDeck(&env, deckID)
	if err != nil {
		var clientErr *models.ClientError
		if errors.As(err, &clientErr) {
			return false, fmt.Errorf("%w: %w", platforms.ErrLibraryRefused, err)
		}
		return false, err
	}
	linked := linkedDeckItemIDs(deck.Items, ref)
	params := models.DecksUpdateParams{DeckID: deck.DeckID}
	switch {
	case add && len(linked) > 0, !add && len(linked) == 0:
		return false, nil
	case add:
		params.AddItems = []models.DeckItemInput{{
			Kind:    deckItemKindMedia,
			MediaID: refMediaID(ref),
			System:  ref.SystemID,
			Path:    ref.Path,
		}}
		if params.AddItems[0].MediaID != nil {
			params.AddItems[0].System, params.AddItems[0].Path = "", ""
		}
	default:
		params.RemoveItemIDs = linked
	}
	if _, err := l.call(ctx, HandleDecksUpdate, &params); err != nil {
		return false, err
	}
	return true, nil
}

// linkedDeckItemIDs returns the items of a deck linked to ref's file.
func linkedDeckItemIDs(items []database.DeckItem, ref platforms.MediaRef) []int64 {
	path := pathutil.CanonicalMediaPath(ref.Path)
	var ids []int64
	for i := range items {
		anchor := &items[i].Anchor
		if anchor.Path != "" && strings.EqualFold(anchor.SystemID, ref.SystemID) &&
			pathutil.CanonicalMediaPath(anchor.Path) == path {
			ids = append(ids, items[i].DBID)
		}
	}
	return ids
}
