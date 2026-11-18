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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/jonboulle/clockwork"
)

const (
	mediaSimilarDefaultLimit = 20
	// suggestionTitleChunk is how many titles one read of suggested media
	// asks for.
	suggestionTitleChunk = 200
	// playIdentityMatches is how many played paths the index no longer holds
	// are matched by identity instead, most played first. Each match is a
	// title lookup, so the rest are left unmatched.
	playIdentityMatches = 20
	planDayLayout       = "2006-01-02"
)

// recommendClock is the time recommendations are weighed at, replaceable in
// tests.
//
//nolint:gochecknoglobals // package-level clock seam for tests
var recommendClock clockwork.Clock = clockwork.NewRealClock()

// recommendedPlans keeps recently built plans. Reading the media rows of a
// plan is cheap; working out which titles belong in it is not.
//
//nolint:gochecknoglobals // process-wide cache shared by every request
var recommendedPlans = recommend.NewPlanCache()

// suggestionSystems returns the set of systems a request limits its results
// to, or nil when it names none.
func suggestionSystems(requested *[]string, fuzzySystem *bool) (map[string]struct{}, error) {
	if requested == nil || len(*requested) == 0 {
		return nil, nil //nolint:nilnil // nil is the "every system" set
	}
	systems, err := resolveSystems(*requested, fuzzySystem != nil && *fuzzySystem)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(systems))
	for i := range systems {
		allowed[systems[i].ID] = struct{}{}
	}
	return allowed, nil
}

// suggestedMedia is the media row a title would launch, and the system it is
// indexed under.
type suggestedMedia struct {
	systemID string
	result   models.SearchResultMedia
}

// suggestedMediaByTitle reads the media row each title would launch, keyed
// by title. Hidden media is left out, so a title with nothing visible is
// absent.
func suggestedMediaByTitle(
	env *requests.RequestEnv, titleDBIDs []int64,
) (map[int64]suggestedMedia, error) {
	ctx := env.Context
	mediaDB := env.Database.MediaDB
	byTitle := make(map[int64][]database.SearchResultWithCursor, len(titleDBIDs))
	for start := 0; start < len(titleDBIDs); start += suggestionTitleChunk {
		chunk := titleDBIDs[start:min(start+suggestionTitleChunk, len(titleDBIDs))]
		rows, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
			TitleDBIDs:    chunk,
			ExcludeHidden: true,
			Limit:         titleVersionsLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("error reading suggested media: %w", err)
		}
		for i := range rows {
			byTitle[rows[i].MediaTitleID] = append(byTitle[rows[i].MediaTitleID], rows[i])
		}
	}
	chosen := make([]database.SearchResultWithCursor, 0, len(byTitle))
	for _, titleDBID := range titleDBIDs {
		rows, ok := byTitle[titleDBID]
		if !ok {
			continue
		}
		delete(byTitle, titleDBID)
		versions, err := foldVersions(ctx, mediaDB, rows)
		if err != nil {
			return nil, fmt.Errorf("error folding suggested media: %w", err)
		}
		if len(versions) == 0 {
			continue
		}
		pick := versions[0]
		if len(versions) > 1 {
			pick = titles.SelectDefaultVersion(versions, env.Config, systemLaunchers(env, pick.SystemID))
		}
		chosen = append(chosen, pick)
	}
	results := newSearchResultBuilder(env).build(ctx, chosen)
	media := make(map[int64]suggestedMedia, len(results))
	for i := range results {
		media[results[i].TitleID] = suggestedMedia{systemID: chosen[i].SystemID, result: results[i]}
	}
	return media, nil
}

func mediaReasons(reasons []recommend.Reason) []models.MediaReason {
	out := make([]models.MediaReason, 0, len(reasons))
	for i := range reasons {
		out = append(out, models.MediaReason{Code: reasons[i].Code, Tags: reasons[i].Tags})
	}
	return out
}

// HandleMediaSimilar lists media like one media item, from what the index's
// scraped tags say each title is. Media the scrapers know nothing about has
// nothing to be compared by, and gets an empty list.
//
//nolint:gocritic // single-use parameter in API handler
func HandleMediaSimilar(env requests.RequestEnv) (any, error) {
	var params models.MediaSimilarParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	ref := mediaRefParam{MediaID: params.MediaID, System: params.System, Path: params.Path}
	if err := validateMediaRef(ref); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil, errors.New("media database is not available")
	}
	systems, err := suggestionSystems(params.Systems, params.FuzzySystem)
	if err != nil {
		return nil, err
	}
	limit := mediaSimilarDefaultLimit
	if params.MaxResults != nil {
		limit = *params.MaxResults
	}

	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}
	// Hidden picks are dropped when their rows are read, so a few more than
	// asked for are scored to keep the list full.
	picks, err := recommend.Similar(env.Context, env.Database.MediaDB, resolved[0].Row.Title.DBID,
		recommend.SimilarOptions{Systems: systems, Limit: limit + limit/2})
	if err != nil {
		return nil, fmt.Errorf("error finding similar media: %w", err)
	}
	titleDBIDs := make([]int64, len(picks))
	for i := range picks {
		titleDBIDs[i] = picks[i].TitleDBID
	}
	media, err := suggestedMediaByTitle(&env, titleDBIDs)
	if err != nil {
		return nil, err
	}
	response := models.MediaSimilarResponse{Results: make([]models.SuggestedMedia, 0, min(len(picks), limit))}
	for i := range picks {
		if len(response.Results) == limit {
			break
		}
		result, ok := media[picks[i].TitleDBID]
		if !ok {
			continue
		}
		response.Results = append(response.Results, models.SuggestedMedia{
			SearchResultMedia: result.result,
			Reasons:           mediaReasons(picks[i].Reasons),
		})
	}
	return response, nil
}

// resolvePlayedTitles maps each play total to the title its path is indexed
// under. A path the index no longer holds is matched by the identity its
// session recorded, for the most played of them.
func resolvePlayedTitles(
	env *requests.RequestEnv,
) func(ctx context.Context, plays []database.MediaPlayTotal) (map[int]int64, error) {
	return func(ctx context.Context, plays []database.MediaPlayTotal) (map[int]int64, error) {
		refs := make([]mediaPathRef, len(plays))
		for i := range plays {
			refs[i] = mediaPathRef{SystemID: plays[i].SystemID, Path: plays[i].MediaPath}
		}
		rows, err := resolveMediaPathIDs(ctx, env.Database.MediaDB, refs)
		if err != nil {
			return nil, fmt.Errorf("resolve played paths: %w", err)
		}
		var missed []int
		for i := range plays {
			if row, ok := rows[refs[i]]; (!ok || row.DBID <= 0) && plays[i].MediaIdentity != nil {
				missed = append(missed, i)
			}
		}
		slices.SortStableFunc(missed, func(a, b int) int {
			return cmp.Compare(plays[b].PlayTime, plays[a].PlayTime)
		})
		missed = missed[:min(len(missed), playIdentityMatches)]
		if len(missed) > 0 {
			identityRefs := make([]identityRef, len(missed))
			for i, index := range missed {
				identityRefs[i] = identityRef{Ref: refs[index], Identity: plays[index].MediaIdentity}
			}
			addMediaIdentityMatches(env, rows, identityRefs)
		}
		resolved := make(map[int]int64, len(plays))
		for i := range plays {
			if row, ok := rows[refs[i]]; ok && row.MediaTitleDBID > 0 {
				resolved[i] = row.MediaTitleDBID
			}
		}
		return resolved, nil
	}
}

// activeProfileID returns the active profile's ID, or "" for the shared
// profile.
func activeProfileID(env *requests.RequestEnv) string {
	if env.State == nil {
		return ""
	}
	if profile := env.State.ActiveProfile(); profile != nil {
		return profile.ProfileID
	}
	return ""
}

// recommendedPlanKey names what a plan for this request is built from.
func recommendedPlanKey(
	env *requests.RequestEnv, systems map[string]struct{}, opts *recommend.Options,
) (recommend.PlanKey, error) {
	key := recommend.PlanKey{
		ProfileID:   activeProfileID(env),
		Day:         recommendClock.Now().UTC().Format(planDayLayout),
		MaxResults:  opts.MaxResults,
		MaxSections: opts.MaxSections,
	}
	names := make([]string, 0, len(systems))
	for id := range systems {
		names = append(names, id)
	}
	slices.Sort(names)
	key.Systems = strings.Join(names, ",")

	var err error
	if key.IndexGeneration, err = env.Database.MediaDB.IndexGeneration(); err != nil {
		return key, fmt.Errorf("error reading index generation: %w", err)
	}
	if key.PreferencesRev, err = env.Database.MediaDB.MediaPreferencesRevision(env.Context); err != nil {
		return key, fmt.Errorf("error reading media preferences revision: %w", err)
	}
	if env.Database.UserDB != nil {
		if key.HistoryVersion, err = env.Database.UserDB.MediaHistoryVersion(env.Context); err != nil {
			return key, fmt.Errorf("error reading media history version: %w", err)
		}
	}
	return key, nil
}

// HandleMediaRecommended lists media the active profile may want to play
// next, in sections that each say why. It is built from the profile's own
// play, the library's flags and the decks made on this device, and from
// nothing else: with none of those, there are no sections.
//
//nolint:gocritic // single-use parameter in API handler
func HandleMediaRecommended(env requests.RequestEnv) (any, error) {
	var params models.MediaRecommendedParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil, errors.New("media database is not available")
	}
	systems, err := suggestionSystems(params.Systems, params.FuzzySystem)
	if err != nil {
		return nil, err
	}
	opts := recommend.Options{
		Systems:     systems,
		MaxResults:  recommend.DefaultMaxResults,
		MaxSections: recommend.DefaultMaxSections,
	}
	if params.MaxResults != nil {
		opts.MaxResults = *params.MaxResults
	}
	if params.MaxSections != nil {
		opts.MaxSections = *params.MaxSections
	}

	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}

	key, err := recommendedPlanKey(&env, systems, &opts)
	if err != nil {
		return nil, err
	}
	now := recommendClock.Now()
	plan, cached := recommendedPlans.Get(&key, now)
	if !cached {
		plan, err = recommend.BuildPlan(env.Context, &recommend.Deps{
			MediaDB:      env.Database.MediaDB,
			UserDB:       env.Database.UserDB,
			ResolvePlays: resolvePlayedTitles(&env),
		}, key.ProfileID, now, opts)
		if err != nil {
			return nil, fmt.Errorf("error building recommendations: %w", err)
		}
		// A plan built mid-index describes a library that is still filling.
		if !statusInstance.isRunning() {
			recommendedPlans.Put(&key, plan, now)
		}
	}
	return hydrateRecommendedPlan(&env, plan, systems, opts.MaxResults)
}

// hydrateRecommendedPlan reads the media rows a plan names. An item whose
// media is hidden is dropped, and a section left with too little to show, or
// whose seed is hidden, goes with it.
func hydrateRecommendedPlan(
	env *requests.RequestEnv, plan *recommend.Plan, systems map[string]struct{}, maxResults int,
) (models.MediaRecommendedResponse, error) {
	response := models.MediaRecommendedResponse{Sections: []models.RecommendedSection{}}
	var titleDBIDs []int64
	seen := make(map[int64]struct{})
	add := func(titleDBID int64) {
		if _, ok := seen[titleDBID]; !ok && titleDBID > 0 {
			seen[titleDBID] = struct{}{}
			titleDBIDs = append(titleDBIDs, titleDBID)
		}
	}
	for i := range plan.Sections {
		add(plan.Sections[i].SeedTitleDBID)
		for j := range plan.Sections[i].Items {
			add(plan.Sections[i].Items[j].TitleDBID)
		}
	}
	if len(titleDBIDs) == 0 {
		return response, nil
	}
	media, err := suggestedMediaByTitle(env, titleDBIDs)
	if err != nil {
		return response, err
	}
	for i := range plan.Sections {
		planned := &plan.Sections[i]
		section := models.RecommendedSection{
			Kind:   planned.Kind,
			Signal: planned.Signal,
			Tag:    planned.Tag,
			Items:  make([]models.SuggestedMedia, 0, min(len(planned.Items), maxResults)),
		}
		if planned.Kind == recommend.SectionSeed {
			seed, ok := media[planned.SeedTitleDBID]
			if !ok {
				continue
			}
			section.Seed = &seed.result
		}
		for j := range planned.Items {
			if len(section.Items) == maxResults {
				break
			}
			result, ok := media[planned.Items[j].TitleDBID]
			if !ok {
				continue
			}
			// A play later item the scrapers know nothing about reaches here
			// without its system having been checked.
			if _, allowed := systems[result.systemID]; systems != nil && !allowed {
				continue
			}
			section.Items = append(section.Items, models.SuggestedMedia{
				SearchResultMedia: result.result,
				Reasons:           mediaReasons(planned.Items[j].Reasons),
			})
		}
		// A request for fewer items than a section normally needs still gets
		// its sections, as long as each is full.
		if len(section.Items) >= min(planned.MinItems(), maxResults) {
			response.Sections = append(response.Sections, section)
		}
	}
	return response, nil
}
