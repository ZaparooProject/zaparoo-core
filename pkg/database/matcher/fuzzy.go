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

package matcher

import (
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/hbollon/go-edlib"
	"github.com/rs/zerolog/log"
)

// Shared thresholds keep title discovery and launch resolution aligned.
const (
	MinSlugLengthForFuzzy   = 5
	FuzzyMatchMaxLengthDiff = 2
	FuzzyMatchMinSimilarity = 0.85
)

// FuzzyLengthWindow is how far a candidate slug's length may sit from the
// query's before it is discarded without being scored.
//
// slack widens it upwards only, for a query that may have lost an abbreviation
// expansion to a typo: slug normalisation turns "Super Mario Bros." into
// "supermariobrothers" (18) while the typo "Super Mario Bross" stays
// "supermariobross" (15), so the title the user meant sits three characters
// away and a flat window of two threw it out unscored. Expansion only ever
// lengthens, so the lower bound does not move.
//
// The widening is deliberately not unconditional. Applying it to every query
// measured 26ms to 531ms per lookup on the MiSTer test device against 24,000
// titles, because the length bounds prune whole candidate blocks before any of
// them is read.
func FuzzyLengthWindow(queryLength, slack int) (low, high int) {
	return queryLength - FuzzyMatchMaxLengthDiff, queryLength + FuzzyMatchMaxLengthDiff + slack
}

// FuzzyMatch represents a slug that matches the query with a similarity score.
type FuzzyMatch struct {
	Slug       string
	Similarity float32
}

// FindFuzzyMatches returns slugs that fuzzy match the query using Jaro-Winkler similarity.
// Jaro-Winkler is optimized for short strings and heavily weights matching prefixes,
// making it ideal for game titles where users typically get the start correct.
// It also naturally handles British/American spelling variations (e.g., "colour" vs "color").
// Results are filtered by maxDistance and minSimilarity, sorted by similarity (best first).
func FindFuzzyMatches(query string, candidates []string, maxDistance int, minSimilarity float32) []FuzzyMatch {
	var matches []FuzzyMatch

	for _, candidate := range candidates {
		// Skip exact matches (already handled by earlier strategies)
		if candidate == query {
			continue
		}

		// Length pre-filter: skip candidates with length difference > maxDistance
		lenDiff := len(query) - len(candidate)
		if lenDiff < 0 {
			lenDiff = -lenDiff
		}
		if lenDiff > maxDistance {
			continue
		}

		// A query one sequel number off from a stored title is not a typo of it:
		// "streetfighter2" and "streetfighter" differ by one character, so without
		// this a shared 13-character prefix scores ~0.986 and launches the wrong game.
		if !SameTitleNumbers(query, candidate) {
			continue
		}

		// Calculate Jaro-Winkler similarity (0.0 to 1.0)
		similarity := edlib.JaroWinklerSimilarity(query, candidate)

		// Debug logging for close matches (helps troubleshoot fuzzy matching)
		if similarity > 0.7 {
			log.Debug().
				Str("query", query).
				Str("candidate", candidate).
				Float32("similarity", similarity).
				Float32("minSimilarity", minSimilarity).
				Msg("fuzzy match candidate evaluation")
		}

		// Filter by minimum similarity threshold
		if similarity >= minSimilarity {
			matches = append(matches, FuzzyMatch{
				Slug:       candidate,
				Similarity: similarity,
			})
		}
	}

	// Sort by similarity (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	return matches
}

// SameTitleNumbers reports whether two already-slugified titles carry the same
// embedded numbers, comparing runs of ASCII digits in order. It never parses a
// run as an integer — titles are untrusted input, and a digit run of arbitrary
// length must not risk an overflow or allocation blowup.
//
// Leading zeros are ignored ("touhou06" agrees with "touhou6"), and a lone "1"
// is treated as no number: a series' first game is often unnumbered
// ("finalfantasy" / "finalfantasy1" name the same game). Any other
// difference — including one side having a number the other lacks entirely —
// means the titles disagree. This is what stops "streetfighter2" from being
// treated as a typo of "streetfighter": Jaro-Winkler alone can't tell a typo
// from a sequel number, only SameTitleNumbers can.
func SameTitleNumbers(a, b string) bool {
	return canonicalTitleNumbers(a) == canonicalTitleNumbers(b)
}

// canonicalTitleNumbers extracts the runs of ASCII digits from s, in order,
// each with leading zeros stripped and a lone "1" dropped, joined by a
// separator that cannot appear in a digit run so "12","3" never collides
// with "1","23".
func canonicalTitleNumbers(s string) string {
	var b strings.Builder
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		run := s[start:end]
		for len(run) > 1 && run[0] == '0' {
			run = run[1:]
		}
		if run != "1" {
			_, _ = b.WriteString(run)
			_ = b.WriteByte(',')
		}
		start = -1
	}
	for i := range len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(s))
	return b.String()
}

// TokenPrefixMatch reports whether query's word tokens are a strict, non-empty
// prefix of candidateName's word tokens: same order, every query token
// matched, and the candidate has at least one more. It compares tokens rather
// than slug bytes so a compound word like "Firefly" cannot be prefix-matched
// by "Fire" — slugification drops the space that would otherwise distinguish
// them, but tokenization (done before spaces are dropped) still has it.
//
// query and candidateName must be original titles with word boundaries
// (e.g. "Street Fighter II"), not slugs — SlugifyWithTokens needs the spaces.
func TokenPrefixMatch(mediaType slugs.MediaType, query, candidateName string) bool {
	queryTokens := slugs.SlugifyWithTokens(mediaType, query).Tokens
	if len(queryTokens) == 0 {
		return false
	}
	candidateTokens := slugs.SlugifyWithTokens(mediaType, candidateName).Tokens
	if len(candidateTokens) <= len(queryTokens) {
		return false
	}
	for i, token := range queryTokens {
		if candidateTokens[i] != token {
			return false
		}
	}
	return true
}

// GenerateTokenSignature creates a normalized, sorted token signature for word-order independent matching.
// Uses the same tokenization pipeline as slugification to ensure consistency.
//
// IMPORTANT: Requires input with word boundaries (e.g., "Super Mario World"), not slugs.
// Slugs have already lost word boundary information and will produce incorrect signatures.
//
// The mediaType parameter ensures media-type-specific parsing is applied (e.g., ParseGame for games),
// matching the indexing pipeline used in GenerateSlugWithMetadata.
//
// Example:
//
//	GenerateTokenSignature(slugs.MediaTypeGame, "Super Mario World") → "mario_super_world"
//	GenerateTokenSignature(slugs.MediaTypeGame, "Mario World Super") → "mario_super_world"
func GenerateTokenSignature(mediaType slugs.MediaType, gameName string) string {
	// Slugify to get the normalized tokens (same pipeline as database indexing)
	// This now applies media-type-specific parsing internally
	result := slugs.SlugifyWithTokens(mediaType, gameName)

	// Sort tokens alphabetically for order-independent matching
	sort.Strings(result.Tokens)

	// Join with underscore delimiter
	return strings.Join(result.Tokens, "_")
}

// FindTokenSignatureMatches finds candidates where the token signature exactly matches the query signature.
// This enables word-order independent matching: "Crystal Space Quest" matches "Quest Space Crystal".
//
// The query and candidates must have word boundaries (e.g., "Super Mario World"), not slugs.
// The mediaType ensures consistent parsing between query and indexed candidates.
// Returns the slugs of matched titles.
func FindTokenSignatureMatches(
	mediaType slugs.MediaType,
	queryName string,
	candidates []database.MediaTitle,
) []string {
	querySignature := GenerateTokenSignature(mediaType, queryName)

	var matches []string
	for _, candidate := range candidates {
		candidateSignature := GenerateTokenSignature(mediaType, candidate.Name)

		// Exact match ensures all tokens match (order-independent)
		if candidateSignature == querySignature {
			matches = append(matches, candidate.Slug)
		}
	}

	return matches
}

// ApplyDamerauLevenshteinTieBreaker refines fuzzy matches using Damerau-Levenshtein distance
// to handle transposition errors (e.g., "crono tigger" → "Chrono Trigger").
//
// It takes the top N candidates from Jaro-Winkler and re-ranks them by edit distance.
// This two-stage approach is more accurate than either algorithm alone while remaining fast.
func ApplyDamerauLevenshteinTieBreaker(query string, matches []FuzzyMatch, topN int) []FuzzyMatch {
	if len(matches) == 0 {
		return matches
	}

	// Only apply tie-breaking if we have multiple candidates
	if len(matches) == 1 {
		return matches
	}

	// Limit to top N candidates to keep performance fast
	candidates := matches
	if topN > 0 && len(matches) > topN {
		candidates = matches[:topN]
	}

	type dlScore struct {
		match    FuzzyMatch
		distance int
	}

	// Calculate Damerau-Levenshtein distance for top candidates
	scored := make([]dlScore, len(candidates))
	for i, candidate := range candidates {
		dist := edlib.DamerauLevenshteinDistance(query, candidate.Slug)
		scored[i] = dlScore{
			match:    candidate,
			distance: dist,
		}
	}

	// Sort by distance (lower is better)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].distance < scored[j].distance
	})

	// Return re-ranked matches
	result := make([]FuzzyMatch, len(scored))
	for i, s := range scored {
		result[i] = s.match
	}

	return result
}

// TokenCoverageRatio reports the fraction of query's *required* word tokens
// that have a close match among candidateName's tokens, matching each
// candidate token to at most one query token via a maximum bipartite
// matching - not a greedy, query-order-dependent assignment, which can miss a
// valid pairing that exists: e.g. query tokens ["cattle", "castel"] against
// candidate tokens ["castle", "battle"] has a perfect matching (cattle-battle,
// castel-castle), but greedily assigning "cattle" to its single best match
// first ("castle", the closer of its two eligible candidates) leaves "castel"
// with no eligible candidate left, even though swapping the query tokens'
// order would have found it - the query's incidental word order must not
// change whether coverage is complete. 1.0 means every required query token
// was accounted for; candidateName may still carry extra tokens the query
// never mentioned without being penalized here (a bare-prefix relationship,
// scored separately by TokenPrefixMatch). A query token that is a lone "1" is
// never required: SameTitleNumbers treats a lone "1" the same way, since a
// series' first game is often unnumbered.
//
// A token "matches" if it's identical, close by Jaro-Winkler similarity, or -
// since normal slugification only expands a correctly-spelled abbreviation,
// leaving a typo of one exactly as typed - a typo of a known abbreviation
// whose expansion matches instead ("bross" against "brothers", the same
// tolerance AbbreviationExpansionSlack already gives the whole-string case).
//
// This exists because a whole-string Jaro-Winkler score, however heavily
// patched, can't reliably tell "these are the same title with a typo" from
// "these happen to share a lot of characters": "streetfighter2turbo" scores
// 0.927 against "streetfighterzero2" by whole-string similarity alone, sharing
// a "streetfighter" prefix and a "2" that SameTitleNumbers can't separate,
// while the words "turbo" and "zero" are simply unrelated - which per-token
// comparison sees directly (0.75 coverage, one required token unmatched)
// instead of having to infer it from character-level side effects.
//
// It is not a universal fix. Two title pairs can have the identical *shape* of
// disagreement with opposite ground truth, and no string-shape metric resolves
// that from the text alone - this function does not try to.
//
// query and candidateName must be original titles with word boundaries (e.g.
// "Street Fighter II"), not slugs - SlugifyWithTokens needs the spaces. A
// word-order match from GenerateTokenSignature should not be checked this way:
// it already requires every token to match, by definition.
func TokenCoverageRatio(mediaType slugs.MediaType, query, candidateName string) float64 {
	queryTokens := slugs.SlugifyWithTokens(mediaType, query).Tokens
	if len(queryTokens) == 0 {
		return 0
	}
	required := make([]string, 0, len(queryTokens))
	for _, queryToken := range queryTokens {
		if queryToken == "1" {
			continue
		}
		required = append(required, queryToken)
	}
	if len(required) == 0 {
		return 1
	}
	candidateTokens := slugs.SlugifyWithTokens(mediaType, candidateName).Tokens
	return float64(maxTokenMatching(required, candidateTokens)) / float64(len(required))
}

// tokenIsMatch reports whether queryToken can be considered a match for
// candidateToken: identical, close by Jaro-Winkler similarity, or - since
// normal slugification only expands a correctly-spelled abbreviation, leaving
// a typo of one exactly as typed - a typo of a known abbreviation whose
// expansion is close to candidateToken instead.
func tokenIsMatch(queryToken, candidateToken string) bool {
	if queryToken == candidateToken {
		return true
	}
	if edlib.JaroWinklerSimilarity(queryToken, candidateToken) >= FuzzyMatchMinSimilarity {
		return true
	}
	if expansion, ok := slugs.ExpandWordIfAbbreviationTypo(queryToken); ok {
		return tokenIsMatch(expansion, candidateToken)
	}
	return false
}

// maxTokenMatching returns the size of a maximum bipartite matching between
// requiredTokens and candidateTokens, with an edge wherever tokenIsMatch
// holds. Kuhn's augmenting-path algorithm: straightforward and exact at the
// token counts a title ever has (a handful of words), where even the
// O(tokens^3) worst case costs nothing.
func maxTokenMatching(requiredTokens, candidateTokens []string) int {
	adjacency := make([][]int, len(requiredTokens))
	for i, queryToken := range requiredTokens {
		for j, candidateToken := range candidateTokens {
			if tokenIsMatch(queryToken, candidateToken) {
				adjacency[i] = append(adjacency[i], j)
			}
		}
	}

	matchedTo := make([]int, len(candidateTokens))
	for i := range matchedTo {
		matchedTo[i] = -1
	}

	var augment func(i int, visited []bool) bool
	augment = func(i int, visited []bool) bool {
		for _, j := range adjacency[i] {
			if visited[j] {
				continue
			}
			visited[j] = true
			if matchedTo[j] == -1 || augment(matchedTo[j], visited) {
				matchedTo[j] = i
				return true
			}
		}
		return false
	}

	matched := 0
	for i := range requiredTokens {
		visited := make([]bool, len(candidateTokens))
		if augment(i, visited) {
			matched++
		}
	}
	return matched
}

// FilterByTokenCoverage drops matches whose candidate (looked up in
// namesBySlug by FuzzyMatch.Slug) does not fully cover query's word tokens per
// TokenCoverageRatio. Applied after Jaro-Winkler and its tie-breaker have
// already narrowed the candidate set to a handful, since computing token
// coverage for every length-eligible candidate would cost as much as the
// similarity scoring it exists to double-check. A candidate missing from
// namesBySlug scores zero coverage and is dropped - every caller builds the
// map from the same candidate list FindFuzzyMatches scored, so this should not
// happen in practice, but a match this function cannot evaluate is not one it
// can vouch for either.
func FilterByTokenCoverage(
	mediaType slugs.MediaType, query string, matches []FuzzyMatch, namesBySlug map[string]string,
) []FuzzyMatch {
	filtered := make([]FuzzyMatch, 0, len(matches))
	for _, match := range matches {
		if TokenCoverageRatio(mediaType, query, namesBySlug[match.Slug]) >= 1 {
			filtered = append(filtered, match)
		}
	}
	return filtered
}
