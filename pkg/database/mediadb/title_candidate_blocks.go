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
	"context"
	"slices"
	"sort"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
)

const (
	candidateBlockEntries  = 32
	candidateSeedBlocks    = 8
	candidateSeedProbes    = 128
	candidateBlockAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz-"
)

// candidateBlock bounds a contiguous group within one system. Counts are the
// minimum/maximum multiplicity of each character per title, not group totals.
// This is derived cache metadata, not another catalog or a persisted format.
type candidateBlock struct {
	minLength    int
	maxLength    int
	counts       [trigramAlphabetSize]byte
	minimum      [trigramAlphabetSize]byte
	unbounded    bool
	prefixLength byte
}

func buildCandidateBlock(cache *SlugSearchCache, start, end int) (candidateBlock, bool) {
	block := candidateBlock{minLength: len(cache.slugData), prefixLength: 255}
	for i := range block.minimum {
		block.minimum[i] = 255
	}
	if start < 0 || end <= start || end >= len(cache.slugOffsets) {
		return block, false
	}
	var first []byte
	for i := start; i < end; i++ {
		from, to := cache.slugOffsets[i], cache.slugOffsets[i+1]
		if from > to || uint64(to) > uint64(len(cache.slugData)) {
			return block, false
		}
		slug := cache.slugForEntry(i)
		if i == start {
			first = slug
		}
		prefix := byte(0)
		for prefix < block.prefixLength && int(prefix) < len(slug) && first[prefix] == slug[prefix] {
			prefix++
		}
		block.prefixLength = prefix
		block.minLength = min(block.minLength, len(slug))
		block.maxLength = max(block.maxLength, len(slug))
		if block.unbounded {
			continue
		}
		var counts [trigramAlphabetSize]byte
		for _, ch := range slug {
			index := trigramCharIndex(ch)
			if index < 0 {
				block.unbounded = true
				break
			}
			if counts[index] == 255 {
				block.unbounded = true // Long repeats bypass pruning instead of overflowing.
				break
			}
			counts[index]++
		}
		for ch, count := range counts {
			block.counts[ch] = max(block.counts[ch], count)
			block.minimum[ch] = min(block.minimum[ch], count)
		}
	}
	return block, true
}

func (cache *SlugSearchCache) buildCandidateBlocks() {
	cache.candidateBlocks = nil
	if cache.entryCount < 0 || len(cache.slugOffsets) <= cache.entryCount {
		return
	}
	blocks := make(map[int64][]candidateBlock, len(cache.systemRanges))
	for system, entries := range cache.systemRanges {
		if entries[0] < 0 || entries[1] < entries[0] || entries[1] > cache.entryCount {
			return // Malformed cache metadata must not introduce a new load-time panic.
		}
		group := make([]candidateBlock, 0, (entries[1]-entries[0]+candidateBlockEntries-1)/candidateBlockEntries)
		for start := entries[0]; start < entries[1]; start += candidateBlockEntries {
			block, ok := buildCandidateBlock(cache, start, min(start+candidateBlockEntries, entries[1]))
			if !ok {
				return
			}
			group = append(group, block)
		}
		blocks[system] = group
	}
	cache.candidateBlocks = blocks
}

// Missing bounds fall back to scanning. Fragment bounds are system-relative,
// so appending a refreshed system does not require rewriting their positions.
func mergeCandidateBlocks(base, replacement *SlugSearchCache) map[int64][]candidateBlock {
	merged := make(map[int64][]candidateBlock, len(base.systemRanges)+len(replacement.systemRanges))
	for system := range base.systemRanges {
		merged[system] = base.candidateBlocks[system]
	}
	for system := range replacement.systemRanges {
		merged[system] = replacement.candidateBlocks[system]
	}
	return merged
}

func (cache *SlugSearchCache) candidateBlocksSize() int {
	size := 0
	for _, blocks := range cache.candidateBlocks {
		// Includes the two ints, counters, bool and conservative alignment
		// padding. Like Size's other arrays, excludes map overhead.
		size += cap(blocks) * (80 + 2*(strconv.IntSize/8))
	}
	return size
}

// candidateSeedPostings borrows a rare nonempty posting list within this system.
// Missing/capped trigrams are ignored: these are hints, never fuzzy filters.
// Slicing each layer avoids allocating a union of catalog-sized posting lists.
func (cache *SlugSearchCache) candidateSeedPostings(query string, entries [2]int) []uint32 {
	var best []uint32
	consider := func(postings []uint32) {
		start := sort.Search(len(postings), func(i int) bool { return int64(postings[i]) >= int64(entries[0]) })
		end := sort.Search(len(postings), func(i int) bool { return int64(postings[i]) >= int64(entries[1]) })
		postings = postings[start:end]
		if len(postings) > 0 && (best == nil || len(postings) < len(best)) {
			best = postings
		}
	}
	for _, trigram := range extractQueryTrigrams([]byte(query)) {
		if int(trigram) < len(cache.trigramCapped) && cache.trigramCapped[trigram] {
			continue
		}
		if int64(trigram)+1 < int64(len(cache.trigramOffsets)) {
			consider(cache.trigramPostings[cache.trigramOffsets[trigram]:cache.trigramOffsets[trigram+1]])
		}
		for _, delta := range cache.trigramDeltas {
			consider(delta[trigram])
		}
	}
	return best
}

// seedBlocks samples at most128 hints and retains at most8 groups. Every other
// group is still scanned; a zero-shared-trigram match cannot be lost here.
func (cache *SlugSearchCache) seedBlocks(
	ctx context.Context, query string, expansionSlack int, entries [2]int,
) (seeds [candidateSeedBlocks]int, count int, err error) {
	postings := cache.candidateSeedPostings(query, entries)
	var scores [candidateSeedBlocks]float32
	probes := min(candidateSeedProbes, len(postings))
	for probe := range probes {
		if err := ctx.Err(); err != nil {
			return seeds, count, err
		}
		position := int64(probe) * int64(len(postings)) / int64(probes)
		entry := int(postings[position])
		slug := cache.slugForEntry(entry)
		if outsideCandidateLengthWindow(len(slug), len(query), expansionSlack) {
			continue
		}
		score := candidateSimilarity(query, string(slug))
		if count == len(seeds) && score <= scores[candidateSeedBlocks-1] {
			continue
		}
		index := (entry - entries[0]) / candidateBlockEntries
		if existing := slices.Index(seeds[:count], index); existing >= 0 {
			if scores[existing] >= score {
				continue
			}
			copy(seeds[existing:], seeds[existing+1:count])
			copy(scores[existing:], scores[existing+1:count])
			count--
		}
		destination := min(count, len(seeds)-1)
		for destination > 0 && score > scores[destination-1] {
			seeds[destination], scores[destination] = seeds[destination-1], scores[destination-1]
			destination--
		}
		seeds[destination], scores[destination] = index, score
		count = min(count+1, len(seeds))
	}
	return seeds, count, nil
}

func (c *candidateCharacterBound) blockPossible(block *candidateBlock, cutoff float32) bool {
	upper := c.blockUpperBound(block)
	return upper > 0 && upper+0.000001 >= cutoff
}

func (c *candidateCharacterBound) blockUpperBound(block *candidateBlock) float32 {
	windowLow, windowHigh := matcher.FuzzyLengthWindow(len(c.query), c.expansionSlack)
	low := max(block.minLength, windowLow)
	high := min(block.maxLength, windowHigh)
	if low > high || high == 0 || c.query == "" {
		return 0
	}
	if block.unbounded || !c.ascii || !c.blockCompatible {
		return 1
	}
	matches, outside := 0, 0
	for _, ch := range c.letters[:c.letterCount] {
		index := trigramCharIndex(ch)
		matches += int(min(c.counts[ch], uint16(block.counts[index])))
		outside += max(0, int(block.minimum[index])-int(c.counts[ch]))
	}
	if matches == 0 {
		return 0
	}
	for _, ch := range c.outside[:c.outsideCount] {
		outside += int(block.minimum[ch])
	}
	// Every title has at least 'outside' unmatched characters, including
	// mandatory repeats beyond the query's multiplicity. For
	// m potential matches, the best length is m+outside, clamped to the
	// eligible range. Assume zero transpositions and the full prefix bonus.
	length := max(low, min(matches+outside, high))
	matches = min(matches, length-outside)
	if matches <= 0 {
		return 0
	}
	upper := (float32(matches)/float32(len(c.query)) + float32(matches)/float32(length) + 1) / 3
	upper += 0.4 * (1 - upper)
	return upper
}
