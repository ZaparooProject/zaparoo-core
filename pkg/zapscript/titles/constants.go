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

package titles

import (
	"regexp"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
)

// reMultiSpace normalizes multiple consecutive spaces to a single space
var reMultiSpace = regexp.MustCompile(`\s+`)

const (
	// Fuzzy matching thresholds
	MinSlugLengthForFuzzy   = matcher.MinSlugLengthForFuzzy
	FuzzyMatchMaxLengthDiff = matcher.FuzzyMatchMaxLengthDiff
	FuzzyMatchMinSimilarity = matcher.FuzzyMatchMinSimilarity

	// Confidence thresholds for result selection
	ConfidenceHigh       = 0.95 // Exact match with perfect/near-perfect tags - immediate return
	ConfidenceAcceptable = 0.70 // Good match with most tags matching - acceptable to launch
	ConfidenceMinimum    = 0.60 // Minimum confidence to launch - below this, error out

	// TitleAmbiguityDiscount applies whenever a result is fundamentally a guess
	// rather than a found answer. Two cases: SelectBestResult's tie-break
	// resolves across results that are genuinely different titles (e.g. a
	// bare-prefix match spanning several distinct sequel entries), not just
	// files of one; or a result comes from the raw Jaro-Winkler fuzzy strategy,
	// which corrects a typo by character shape alone and has no structural
	// guarantee it reached the right title (see resolve.go's Strategy 5) - two
	// titles of the identical "N tokens vs N-1, sharing N-1" shape can score
	// identically while one is the right typo correction and the other is a
	// different, unrelated sequel. Both cases land the confidence in the
	// 0.65-0.70 band instead of the 0.85-0.93 a clean match would claim.
	TitleAmbiguityDiscount = 0.75

	// Match quality scores (base confidence for each strategy, before tag matching adjustment)
	MatchQualityExact           = 1.00 // Perfect slug match
	MatchQualitySecondaryTitle  = 0.92 // Exact secondary title match
	MatchQualityMainTitle       = 0.90 // Main title only match (partial match)
	MatchQualityProgressiveTrim = 0.85 // Progressive word trimming (last resort)
	// Note: Fuzzy match quality comes from similarity algorithm (0.85-1.0)

	// Strategy identifiers (order-independent naming)
	StrategyExactMatch            = "strategy_exact_match"
	StrategyPrefixMatch           = "strategy_prefix_match"
	StrategyMainTitleOnly         = "strategy_main_title_only"
	StrategySecondaryTitleExact   = "strategy_secondary_title_exact"
	StrategySharedSecondaryTitle  = "strategy_shared_secondary_title"
	StrategyTokenSignature        = "strategy_token_signature"
	StrategyJaroWinklerDamerau    = "strategy_jarowinkler_damerau"
	StrategyProgressiveTrim       = "strategy_progressive_trim"
	StrategyExactMatchNoAutoTags  = "strategy_exact_match_no_auto_tags"
	StrategyPrefixMatchNoAutoTags = "strategy_prefix_match_no_auto_tags"
)
