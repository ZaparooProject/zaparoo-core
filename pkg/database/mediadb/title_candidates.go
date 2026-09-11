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

package mediadb

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/hbollon/go-edlib"
)

var errCandidateDatabaseChanged = errors.New("media database changed; retry title candidates")

const candidateBatchSize = 128

// TitleCandidates discovers titles without selecting files or touching resolution
// caches. SQL always verifies present, visible media, including cache nominations.
// One connection pins the database incarnation, not an indexing snapshot: ordinary
// indexing may become visible between statements, just as in media.search.
func (db *MediaDB) TitleCandidates(
	ctx context.Context, systemID, name string, limit int,
) ([]database.TitleCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !database.ValidTitleCandidateName(name) {
		return nil, errors.New("invalid candidate name")
	}
	if limit < 1 || limit > database.TitleCandidateLimit {
		return nil, errors.New("invalid candidate limit")
	}
	system, err := systemdefs.GetSystem(systemID)
	if err != nil {
		return nil, fmt.Errorf("candidate system: %w", err)
	}
	query := GenerateSlugWithMetadata(system.GetMediaType(), name)
	if query.Slug == "" {
		return nil, errors.New("candidate name normalizes to empty")
	}
	source, err := db.readConn()
	if err != nil {
		return nil, err
	}
	if db.recreating.Load() {
		return nil, errCandidateDatabaseChanged
	}
	conn, err := source.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("candidate connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var systemDBID int64
	err = conn.QueryRowContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?", system.ID).Scan(&systemDBID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("candidate system lookup: %w", err)
	}
	var ranked []rankedTitle
	if err == nil {
		ranker := titleRanker{
			query: query, name: name, system: *system, limit: limit,
			expansionSlack: slugs.AbbreviationExpansionSlack(name),
		}
		// Indexed exact reads avoid a system-wide cache walk and see new titles
		// before the shared fuzzy cache refreshes. Exact results are never padded.
		err = ranker.read(ctx, conn, systemDBID, ranker.exactCondition(), ranker.exactArgs())
		if err != nil {
			return nil, err
		}
		if len(ranker.top) == 0 && len(query.Slug) >= matcher.MinSlugLengthForFuzzy {
			ranker.allowFuzzy = true
			ranker.signature = matcher.GenerateTokenSignature(system.GetMediaType(), name)
			cache := db.slugSearchCache.Load()
			if cache != nil && cache.source == source && cache.CanServeSystems([]string{system.ID}) {
				err = ranker.cachedFuzzy(ctx, conn, cache, systemDBID)
			} else {
				err = ranker.read(ctx, conn, systemDBID, candidatePrefilter, ranker.prefilterArgs())
			}
			if err != nil {
				return nil, err
			}
		}
		ranked = ranker.top
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db.recreating.Load() || db.sql.Load() != source {
		return nil, errCandidateDatabaseChanged
	}
	results := make([]database.TitleCandidate, len(ranked))
	for i := range ranked {
		results[i] = ranked[i].candidate
		results[i].Rank = i + 1
	}
	return results, nil
}

type rankedTitle struct {
	candidate database.TitleCandidate
	id        int64
	quality   int
	distance  int
}

type titleRanker struct {
	characters *candidateCharacterBound
	top        []rankedTitle
	system     systemdefs.System
	name       string
	signature  string
	query      SlugMetadata
	limit      int
	// expansionSlack allows for a slug that lost an abbreviation expansion to
	// a typo, so the title the user meant is not pruned on length alone.
	expansionSlack int
	allowFuzzy     bool
}

func (r *titleRanker) exactCondition() string {
	if r.query.SecondarySlug == "" {
		return "(t.Slug = ? OR t.SecondarySlug = ?)"
	}
	return "(t.Slug = ? OR t.Slug = ? OR t.SecondarySlug = ?)"
}

func (r *titleRanker) exactArgs() []any {
	if r.query.SecondarySlug == "" {
		return []any{r.query.Slug, r.query.Slug}
	}
	return []any{r.query.Slug, r.query.SecondarySlug, r.query.SecondarySlug}
}

const candidatePrefilter = "t.SlugLength BETWEEN ? AND ? AND t.SlugWordCount BETWEEN ? AND ?"

func (r *titleRanker) prefilterArgs() []any {
	return []any{
		max(0, r.query.SlugLength-3), r.query.SlugLength + 3,
		max(1, r.query.SlugWordCount-1), r.query.SlugWordCount + 1,
	}
}

func (r *titleRanker) fuzzyCutoff() float32 {
	if len(r.top) == r.limit && r.top[len(r.top)-1].quality == 2 {
		return float32(r.top[len(r.top)-1].candidate.Confidence)
	}
	return matcher.FuzzyMatchMinSimilarity
}

func (r *titleRanker) cachedFuzzy(
	ctx context.Context, conn *sql.Conn, cache *SlugSearchCache, systemDBID int64,
) error {
	return r.scanCachedFuzzy(ctx, conn, cache, systemDBID, true)
}

// scanCachedFuzzy retains sequential traversal as an internal benchmark oracle.
// Prioritization changes visit order only, never the eligible candidate set.
func (r *titleRanker) scanCachedFuzzy(
	ctx context.Context, conn *sql.Conn, cache *SlugSearchCache, systemDBID int64, prioritize bool,
) error {
	entryRange := cache.systemRanges[systemDBID]
	queryCharacters := r.characterBound()
	characters := *queryCharacters // Cache-prefix scratch must not affect SQL row validation.
	blocks := cache.candidateBlocks[systemDBID]
	if len(blocks) != (entryRange[1]-entryRange[0]+candidateBlockEntries-1)/candidateBlockEntries {
		blocks = nil // Older or partial cache metadata must fall back, not lose candidates.
	}
	ids := make([]any, 0, candidateBatchSize)
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		condition := candidatePrefilter + " AND t.DBID IN (" + prepareVariadic("?", ",", len(ids)) + ")"
		err := r.read(ctx, conn, systemDBID, condition, append(r.prefilterArgs(), ids...))
		ids = ids[:0]
		return err
	}
	scan := func(start, end int) error {
		for i := start; i < end; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			slugBytes := cache.slugForEntry(i)
			low, high := matcher.FuzzyLengthWindow(len(r.query.Slug), r.expansionSlack)
			if len(slugBytes) < low || len(slugBytes) > high {
				continue
			}
			slug := string(slugBytes)
			possibleSignature, possible := characters.check(slug, r.fuzzyCutoff())
			if !possibleSignature && (!possible || candidateSimilarity(r.query.Slug, slug) < r.fuzzyCutoff()) {
				continue
			}
			ids = append(ids, cache.titleDBIDs[i])
			if len(ids) == candidateBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if len(blocks) == 0 {
		if err := scan(entryRange[0], entryRange[1]); err != nil {
			return err
		}
		return flush()
	}
	visit := func(index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		block := &blocks[index]
		if !queryCharacters.blockPossible(block, r.fuzzyCutoff()) {
			return nil
		}
		start := entryRange[0] + index*candidateBlockEntries
		characters.setPrefix(cache.slugForEntry(start)[:block.prefixLength])
		return scan(start, min(start+candidateBlockEntries, entryRange[1]))
	}
	var seeds [candidateSeedBlocks]int
	count := 0
	if prioritize && len(blocks) > candidateSeedBlocks {
		var err error
		seeds, count, err = cache.seedBlocks(ctx, r.query.Slug, r.expansionSlack, entryRange)
		if err != nil {
			return err
		}
		for _, index := range seeds[:count] {
			if err := visit(index); err != nil {
				return err
			}
		}
		// Seed evidence can tighten bounds only after SQL checks visibility.
		if err := flush(); err != nil {
			return err
		}
	}
	for index := range blocks {
		if slices.Contains(seeds[:count], index) {
			continue
		}
		if err := visit(index); err != nil {
			return err
		}
	}
	return flush()
}

// candidateCharacterBound is request-local scratch, not a retained title index.
// One multiset intersection checks both token-reordering potential and Jaro's
// maximum matching-character count. Only touched counters need resetting.
// Unicode bypasses the byte bound and uses the existing rune-aware scorer.
type candidateCharacterBound struct {
	query           string
	expansionSlack  int
	letterCount     int
	outsideCount    int
	prefixLength    int
	prefixMatches   int
	counts          [128]uint16
	baseCounts      [128]uint16
	remaining       [128]uint16
	touched         [128]byte
	letters         [128]byte
	outside         [trigramAlphabetSize]byte
	ascii           bool
	blockCompatible bool
}

func newCandidateCharacterBound(query string, expansionSlack int) *candidateCharacterBound {
	bound := &candidateCharacterBound{
		query: query, expansionSlack: expansionSlack,
		ascii: len(query) <= 65535, blockCompatible: true,
	}
	if !bound.ascii {
		return bound
	}
	for i := range len(query) {
		if query[i] >= 128 {
			bound.ascii = false
			return bound
		}
		ch := query[i]
		if bound.counts[ch] == 0 {
			bound.letters[bound.letterCount] = ch
			bound.letterCount++
		}
		if trigramCharIndex(ch) < 0 {
			bound.blockCompatible = false
		}
		bound.counts[ch]++
	}
	bound.baseCounts = bound.counts
	bound.remaining = bound.counts
	for i := byte(0); i < trigramAlphabetSize; i++ {
		if bound.counts[candidateBlockAlphabet[i]] == 0 {
			bound.outside[bound.outsideCount] = i
			bound.outsideCount++
		}
	}
	return bound
}

func (r *titleRanker) characterBound() *candidateCharacterBound {
	if r.characters == nil {
		r.characters = newCandidateCharacterBound(r.query.Slug, r.expansionSlack)
	}
	return r.characters
}

// setPrefix consumes a group's shared prefix once. Only the cache-local copy
// uses this: SQL validation may read nominations from several different groups.
// Subsequent check calls must receive titles sharing this prefix.
func (c *candidateCharacterBound) setPrefix(prefix []byte) {
	c.baseCounts = c.counts
	c.prefixLength, c.prefixMatches = 0, 0
	for _, ch := range prefix {
		if ch >= 128 {
			c.baseCounts = c.counts
			c.remaining = c.counts
			c.prefixMatches = 0
			return
		}
		if c.baseCounts[ch] > 0 {
			c.baseCounts[ch]--
			c.prefixMatches++
		}
	}
	c.prefixLength = len(prefix)
	c.remaining = c.baseCounts
}

func (c *candidateCharacterBound) check(b string, cutoff float32) (sameLetters, possible bool) {
	a := c.query
	if a == "" || b == "" {
		return false, false
	}
	if !c.ascii {
		return sameCandidateLetters(a, b), true
	}
	n, matches := 0, c.prefixMatches
	ascii := true
	for i := c.prefixLength; i < len(b); i++ {
		ch := b[i]
		if ch >= 128 {
			ascii = false
			continue
		}
		if c.remaining[ch] > 0 {
			if c.remaining[ch] == c.baseCounts[ch] {
				c.touched[n] = ch
				n++
			}
			c.remaining[ch]--
			matches++
		}
	}
	for _, ch := range c.touched[:n] {
		c.remaining[ch] = c.baseCounts[ch]
	}
	if !ascii {
		return sameCandidateLetters(a, b), true
	}
	if matches == 0 {
		return false, false
	}
	prefix := 0
	for prefix < min(4, len(a), len(b)) && a[prefix] == b[prefix] {
		prefix++
	}
	upper := (float32(matches)/float32(len(a)) + float32(matches)/float32(len(b)) + 1) / 3
	upper += 0.1 * float32(prefix) * (1 - upper)
	// With no transpositions, this upper bound is conservative even at float32
	// threshold rounding; the real score is still computed before ranking.
	return len(a) == len(b) && matches == len(a), upper+0.000001 >= cutoff
}

func (r *titleRanker) read(
	ctx context.Context, conn *sql.Conn, systemDBID int64, condition string, args []any,
) error {
	// Conditions contain only internal SQL and placeholder lists. Names, slugs,
	// system IDs, and nomination IDs are always bound parameters.
	//nolint:gosec // Internally constructed predicates; all input is bound.
	rows, err := conn.QueryContext(ctx, `SELECT t.DBID, t.Name, t.Slug, COALESCE(t.SecondarySlug, '')
		FROM MediaTitles t WHERE t.SystemDBID = ? AND `+condition+`
		AND EXISTS (SELECT 1 FROM Media m WHERE m.MediaTitleDBID = t.DBID
			AND m.IsMissing = 0`+browseVisibilityCondition("m.DBID", true)+`)`,
		append([]any{systemDBID}, args...)...)
	if err != nil {
		return fmt.Errorf("query title candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var id int64
	var name, slug, secondary string
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rows.Scan(&id, &name, &slug, &secondary); err != nil {
			return fmt.Errorf("scan title candidate: %w", err)
		}
		r.consider(id, name, slug, secondary)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read title candidates: %w", err)
	}
	return nil
}

func firstCandidateToken(mediaType slugs.MediaType, name string) string {
	main, _, _ := slugs.SplitTitle(slugs.StripLeadingArticle(strings.TrimSpace(name)))
	tokens := slugs.SlugifyWithTokens(mediaType, main).Tokens
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func (r *titleRanker) secondaryMatch(name, slug, secondary string) bool {
	if r.query.SecondarySlug == "" {
		return secondary == r.query.Slug
	}
	if secondary == "" {
		return slug == r.query.SecondarySlug
	}
	if secondary != r.query.SecondarySlug {
		return false
	}
	// A shared subtitle cannot join unrelated franchises.
	first := firstCandidateToken(r.system.GetMediaType(), r.name)
	return first != "" && first == firstCandidateToken(r.system.GetMediaType(), name)
}

func (r *titleRanker) consider(id int64, name, slug, secondary string) {
	item := rankedTitle{id: id, candidate: database.TitleCandidate{SystemID: r.system.ID, Name: name}}
	switch {
	case slug == r.query.Slug:
		item.candidate.MatchType, item.candidate.Confidence = "exact", 1
	case r.secondaryMatch(name, slug, secondary):
		item.quality = 1
		item.candidate.MatchType, item.candidate.Confidence = "secondary", 0.92
	default:
		if !r.allowFuzzy || len(r.query.Slug) < matcher.MinSlugLengthForFuzzy ||
			outsideCandidateLengthWindow(len(slug), len(r.query.Slug), r.expansionSlack) {
			return
		}
		possibleSignature, possible := r.characterBound().check(slug, r.fuzzyCutoff())
		if !possibleSignature && !possible {
			return
		}
		similarity := candidateSimilarity(r.query.Slug, slug)
		if possibleSignature &&
			matcher.GenerateTokenSignature(r.system.GetMediaType(), name) == r.signature {
			similarity = 1
		}
		if similarity < matcher.FuzzyMatchMinSimilarity {
			return
		}
		item.quality = 2
		item.candidate.MatchType, item.candidate.Confidence = "fuzzy", float64(similarity)
		if len(r.top) == r.limit {
			last := r.top[len(r.top)-1]
			if last.quality < item.quality || last.candidate.Confidence > item.candidate.Confidence {
				return
			}
			if last.candidate.Confidence == item.candidate.Confidence {
				lower := candidateDistanceLowerBound(r.query.Slug, slug)
				nameOrder := strings.Compare(name, last.candidate.Name)
				if lower > last.distance || (lower == last.distance &&
					(nameOrder > 0 || (nameOrder == 0 && id >= last.id))) {
					return
				}
			}
		}
		item.distance = edlib.DamerauLevenshteinDistance(r.query.Slug, slug)
	}
	// The public identity is system + canonical name, not a regional media row
	// or a potentially duplicated title row. Only retained top-k needs deduping.
	for i := range r.top {
		if r.top[i].candidate.Name == name {
			if compareRankedTitle(item, r.top[i]) >= 0 {
				return
			}
			r.top = slices.Delete(r.top, i, i+1)
			break
		}
	}
	if len(r.top) == r.limit && compareRankedTitle(item, r.top[len(r.top)-1]) >= 0 {
		return
	}
	r.top = append(r.top, item)
	slices.SortFunc(r.top, compareRankedTitle)
	if len(r.top) > r.limit {
		r.top = r.top[:r.limit]
	}
}

// Reordered tokens preserve slug bytes. Reject impossible signatures cheaply
// before running the full normalization pipeline for each prefiltered title.
func sameCandidateLetters(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var counts [256]int
	for i := range len(b) {
		counts[b[i]]++
	}
	for i := range len(a) {
		counts[a[i]]--
		if counts[a[i]] < 0 {
			return false
		}
	}
	return true
}

func compareRankedTitle(a, b rankedTitle) int { //nolint:gocritic // slices.SortFunc comparator signature
	if order := cmp.Compare(a.quality, b.quality); order != 0 {
		return order
	}
	if order := cmp.Compare(b.candidate.Confidence, a.candidate.Confidence); order != 0 {
		return order
	}
	if order := cmp.Compare(a.distance, b.distance); order != 0 {
		return order
	}
	if order := strings.Compare(a.candidate.Name, b.candidate.Name); order != 0 {
		return order
	}
	return cmp.Compare(a.id, b.id)
}

// outsideCandidateLengthWindow reports whether a candidate slug is too far from
// the query's length to be worth scoring.
func outsideCandidateLengthWindow(candidateLength, queryLength, expansionSlack int) bool {
	low, high := matcher.FuzzyLengthWindow(queryLength, expansionSlack)
	return candidateLength < low || candidateLength > high
}
