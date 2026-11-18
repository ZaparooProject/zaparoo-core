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

package recommend

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

const (
	// similarCandidates is how many titles sharing tags with one item are
	// scored in full, and similarPostings bounds the tag index entries read
	// to find them.
	similarCandidates = 150
	similarPostings   = 6000

	decadeYears = 10
)

// Reason is one thing a pick shares with the title it was picked for.
type Reason struct {
	Code string
	Tags []database.TagInfo
}

// Pick is a title found to be like another, with why.
type Pick struct {
	SystemID  string
	Reasons   []Reason
	TitleDBID int64
	Score     float64
	Rating    int
}

// seedToken is one tag of a seed title, weighted by its facet and by how rare
// the tag is in the library.
type seedToken struct {
	tag       profileTag
	weight    float64
	titles    int
	generates bool
}

// seed is a title prepared for comparison against others.
type seed struct {
	profile *titleProfile
	tokens  []seedToken
}

// canGenerate reports whether the seed has a tag that can find candidates.
func (s *seed) canGenerate() bool {
	for i := range s.tokens {
		if s.tokens[i].generates {
			return true
		}
	}
	return false
}

// prepareSeeds reads what the given titles are like and weighs each of their
// tags against the library. Titles the index knows nothing about are absent.
func prepareSeeds(ctx context.Context, mediaDB MediaStore, titleDBIDs []int64) (map[int64]*seed, error) {
	seeds := make(map[int64]*seed, len(titleDBIDs))
	if len(titleDBIDs) == 0 {
		return seeds, nil
	}
	rows, err := mediaDB.GetTitleFacetTags(ctx, titleDBIDs, facetTagTypes)
	if err != nil {
		return nil, fmt.Errorf("read seed tags: %w", err)
	}
	profiles := buildProfiles(rows)
	if len(profiles) == 0 {
		return seeds, nil
	}
	titleCount, err := mediaDB.MediaTitleCountEstimate(ctx)
	if err != nil {
		return nil, fmt.Errorf("estimate title count: %w", err)
	}
	cutoff := ubiquitousCutoff(titleCount)

	var tagIDs []int64
	seen := make(map[int64]struct{})
	for _, profile := range profiles {
		for i := range profile.tags {
			id := profile.tags[i].tagDBID
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				tagIDs = append(tagIDs, id)
			}
		}
	}
	slices.Sort(tagIDs)
	counts, err := mediaDB.CountTitlesByTag(ctx, tagIDs, cutoff+1)
	if err != nil {
		return nil, fmt.Errorf("count tag titles: %w", err)
	}

	for titleDBID, profile := range profiles {
		prepared := &seed{profile: profile, tokens: make([]seedToken, 0, len(profile.tags))}
		for i := range profile.tags {
			tag := profile.tags[i]
			titles := max(counts[tag.tagDBID], 1)
			token := seedToken{tag: tag, titles: titles}
			if titles > cutoff {
				token.weight = tag.facet.weight * rarity(titleCount, int64(cutoff)) * ubiquitousDamping
			} else {
				token.weight = tag.facet.weight * rarity(titleCount, int64(titles))
				token.generates = tag.facet.strong
			}
			prepared.tokens = append(prepared.tokens, token)
		}
		// Heaviest first, so a postings budget keeps the tags that say most.
		slices.SortStableFunc(prepared.tokens, func(a, b seedToken) int {
			return cmp.Or(cmp.Compare(b.weight, a.weight), cmp.Compare(a.tag.tagDBID, b.tag.tagDBID))
		})
		seeds[titleDBID] = prepared
	}
	return seeds, nil
}

// candidateTags picks the seed's tags to look candidates up by: the heaviest
// that fit the postings budget. A series tag is always kept, because nothing
// says more that two titles belong together.
func (s *seed) candidateTags(postingsBudget int) []database.WeightedTag {
	var weighted []database.WeightedTag
	spent := 0
	for i := range s.tokens {
		token := &s.tokens[i]
		if !token.generates {
			continue
		}
		if token.tag.facet.code != ReasonSeries && spent+token.titles > postingsBudget {
			continue
		}
		spent += token.titles
		weighted = append(weighted, database.WeightedTag{TagDBID: token.tag.tagDBID, Weight: token.weight})
	}
	return weighted
}

// reasonScore gathers what one reason code contributed to a score.
type reasonScore struct {
	code  string
	tags  []database.TagInfo
	score float64
}

// score compares a candidate with the seed. ok is false when they share
// nothing that makes two titles alike.
func (s *seed) score(candidate *titleProfile) (pick Pick, ok bool) {
	byCode := make(map[string]*reasonScore)
	add := func(code string, amount float64, tag database.TagInfo) {
		reason, found := byCode[code]
		if !found {
			reason = &reasonScore{code: code}
			byCode[code] = reason
		}
		reason.score += amount
		reason.tags = append(reason.tags, tag)
	}

	strong := false
	sharedFamilies := make(map[string]struct{})
	for i := range s.tokens {
		token := &s.tokens[i]
		if _, shared := candidate.tagIDs[token.tag.tagDBID]; !shared {
			continue
		}
		code := token.tag.facet.code
		// A title with many values of a facet shares one more easily, so
		// each shared value counts for less.
		amount := token.weight / math.Sqrt(float64(max(candidate.facetCount[code], 1)))
		add(code, amount, database.TagInfo{Type: token.tag.tagType, Tag: token.tag.tag})
		if token.generates {
			strong = true
		}
		if code == ReasonGenre {
			sharedFamilies[genreFamily(token.tag.tag)] = struct{}{}
		}
	}
	if !strong {
		return Pick{}, false
	}

	candidateFamilies := candidate.genreFamilies()
	for family := range s.profile.genreFamilies() {
		if _, exact := sharedFamilies[family]; exact {
			continue
		}
		if _, related := candidateFamilies[family]; related {
			amount := weightGenreFamily / math.Sqrt(float64(max(candidate.facetCount[ReasonGenre], 1)))
			add(ReasonGenre, amount, database.TagInfo{Type: string(tags.TagTypeGenre), Tag: family})
		}
	}
	if s.profile.year > 0 && candidate.year > 0 {
		switch {
		case s.profile.year == candidate.year:
			add(ReasonYear, weightYear, yearTag(candidate.year))
		case s.profile.year/decadeYears == candidate.year/decadeYears:
			add(ReasonDecade, weightDecade, yearTag(candidate.year/decadeYears*decadeYears))
		}
	}

	total := 0.0
	reasons := make([]*reasonScore, 0, len(byCode))
	for _, reason := range byCode {
		total += reason.score
		reasons = append(reasons, reason)
	}
	if s.profile.systemID == candidate.systemID {
		total *= sameSystemBoost
	}
	if total < minSimilarity {
		return Pick{}, false
	}
	slices.SortFunc(reasons, func(a, b *reasonScore) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.code, b.code))
	})
	pick = Pick{
		TitleDBID: candidate.titleDBID,
		SystemID:  candidate.systemID,
		Score:     total,
		Rating:    candidate.rating,
	}
	for _, reason := range reasons[:min(len(reasons), maxReasons)] {
		slices.SortFunc(reason.tags, func(a, b database.TagInfo) int {
			return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Tag, b.Tag))
		})
		pick.Reasons = append(pick.Reasons, Reason{Code: reason.code, Tags: reason.tags})
	}
	return pick, true
}

func yearTag(year int) database.TagInfo {
	return database.TagInfo{Type: string(tags.TagTypeYear), Tag: fmt.Sprintf("%04d", year)}
}

// comparePicks orders picks best first, with the scraped rating and then the
// title's row as tie-breaks so the order never depends on map iteration.
func comparePicks(a, b *Pick) int {
	return cmp.Or(
		cmp.Compare(b.Score, a.Score),
		cmp.Compare(b.Rating, a.Rating),
		cmp.Compare(a.TitleDBID, b.TitleDBID),
	)
}

// versionGroupOf returns the group of each title, falling back to the title
// itself for one the lookup did not return.
func versionGroupOf(groups map[int64]int64, titleDBID int64) int64 {
	if group, ok := groups[titleDBID]; ok {
		return group
	}
	return titleDBID
}

// SimilarOptions narrows a Similar lookup.
type SimilarOptions struct {
	// Systems limits picks to these systems. Nil allows every system.
	Systems map[string]struct{}
	Limit   int
}

// Similar returns the titles most like the given one, best first. A title is
// never like another version of itself or the same game on another system,
// and each game is picked once however many versions and ports of it the
// library holds. A title the scrapers know nothing
// about has nothing to compare, so it has no picks.
func Similar(
	ctx context.Context, mediaDB MediaStore, titleDBID int64, opts SimilarOptions,
) ([]Pick, error) {
	if opts.Limit <= 0 {
		return nil, nil
	}
	seeds, err := prepareSeeds(ctx, mediaDB, []int64{titleDBID})
	if err != nil {
		return nil, err
	}
	prepared, ok := seeds[titleDBID]
	if !ok || !prepared.canGenerate() {
		return nil, nil
	}
	exclude, err := groupTitles(ctx, mediaDB, titleDBID)
	if err != nil {
		return nil, err
	}
	found, err := mediaDB.FindTitlesSharingTags(
		ctx, prepared.candidateTags(similarPostings), exclude, similarCandidates)
	if err != nil {
		return nil, fmt.Errorf("find similar titles: %w", err)
	}
	candidateIDs := make([]int64, len(found))
	for i := range found {
		candidateIDs[i] = found[i].TitleDBID
	}
	profiles, groups, err := loadCandidates(ctx, mediaDB, candidateIDs)
	if err != nil {
		return nil, err
	}

	best := make(map[int64]Pick)
	for _, id := range candidateIDs {
		profile, known := profiles[id]
		if !known {
			continue
		}
		if opts.Systems != nil {
			if _, allowed := opts.Systems[profile.systemID]; !allowed {
				continue
			}
		}
		if profile.slug != "" && profile.slug == prepared.profile.slug {
			continue
		}
		pick, alike := prepared.score(profile)
		if !alike {
			continue
		}
		group := versionGroupOf(groups, id)
		if current, held := best[group]; !held || comparePicks(&pick, &current) < 0 {
			best[group] = pick
		}
	}
	ranked := make([]Pick, 0, len(best))
	for _, pick := range best {
		ranked = append(ranked, pick)
	}
	slices.SortFunc(ranked, func(a, b Pick) int { return comparePicks(&a, &b) })
	// The best-ranked port stands for a game held on several systems.
	picks := make([]Pick, 0, min(len(ranked), opts.Limit))
	ports := make(map[string]struct{})
	for i := range ranked {
		if len(picks) == opts.Limit {
			break
		}
		if slug := profiles[ranked[i].TitleDBID].slug; slug != "" {
			if _, taken := ports[slug]; taken {
				continue
			}
			ports[slug] = struct{}{}
		}
		picks = append(picks, ranked[i])
	}
	return picks, nil
}

// groupTitles returns every title of the given title's version group.
func groupTitles(ctx context.Context, mediaDB MediaStore, titleDBID int64) ([]int64, error) {
	members, err := mediaDB.GetVersionGroupMembers(ctx, []int64{titleDBID})
	if err != nil {
		return nil, fmt.Errorf("look up version group: %w", err)
	}
	if group := members[titleDBID]; len(group) > 0 {
		return group, nil
	}
	return []int64{titleDBID}, nil
}

// loadCandidates reads what candidate titles are like and which version
// group each belongs to.
func loadCandidates(
	ctx context.Context, mediaDB MediaStore, titleDBIDs []int64,
) (profiles map[int64]*titleProfile, groups map[int64]int64, err error) {
	if len(titleDBIDs) == 0 {
		return map[int64]*titleProfile{}, map[int64]int64{}, nil
	}
	rows, err := mediaDB.GetTitleFacetTags(ctx, titleDBIDs, facetTagTypes)
	if err != nil {
		return nil, nil, fmt.Errorf("read candidate tags: %w", err)
	}
	groups, err = mediaDB.GetTitleVersionGroups(ctx, titleDBIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("look up candidate version groups: %w", err)
	}
	return buildProfiles(rows), groups, nil
}
