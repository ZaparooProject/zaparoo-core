// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

import "regexp"

// reMultiSpace normalizes multiple consecutive spaces to a single space
var reMultiSpace = regexp.MustCompile(`\s+`)

const (
	// Fuzzy matching thresholds
	MinSlugLengthForFuzzy   = 5
	FuzzyMatchMaxLengthDiff = 2
	FuzzyMatchMinSimilarity = 0.85

	// Secondary title minimum length for search
	MinSecondaryTitleSlugLength = 4

	// Confidence thresholds for result selection
	ConfidenceHigh       = 0.95 // Exact match with perfect/near-perfect tags - immediate return
	ConfidenceAcceptable = 0.70 // Good match with most tags matching - acceptable to launch
	ConfidenceMinimum    = 0.60 // Minimum confidence to launch - below this, error out

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
	StrategyTokenSignature        = "strategy_token_signature"
	StrategyJaroWinklerDamerau    = "strategy_jarowinkler_damerau"
	StrategyProgressiveTrim       = "strategy_progressive_trim"
	StrategyExactMatchNoAutoTags  = "strategy_exact_match_no_auto_tags"
	StrategyPrefixMatchNoAutoTags = "strategy_prefix_match_no_auto_tags"
)
