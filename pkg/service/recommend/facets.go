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

// Package recommend finds media like other media from what the library's
// scraped tags say about each title, and builds a personal list from the
// titles a profile has shown interest in. Everything is computed on demand
// from the local index: nothing is precomputed and nothing leaves the device.
package recommend

import (
	"math"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// Reason codes name the kind of thing two titles share. Clients turn them
// into their own wording.
const (
	ReasonSeries      = "series"
	ReasonGenre       = "genre"
	ReasonPerspective = "perspective"
	ReasonArt         = "art"
	ReasonBased       = "based"
	ReasonDeveloper   = "developer"
	ReasonPlayers     = "players"
	ReasonYear        = "year"
	ReasonDecade      = "decade"
	ReasonPublisher   = "publisher"
)

// Facet weights say how much sharing one kind of tag counts for. A series
// outweighs everything, genre outweighs who made the game, and the developer
// stays low because a studio's whole catalog would otherwise cluster.
const (
	weightSeries      = 3.0
	weightGenre       = 1.0
	weightGenreFamily = 0.35
	weightPerspective = 0.7
	weightBased       = 0.6
	weightArt         = 0.5
	weightDeveloper   = 0.4
	weightPlayers     = 0.2
	weightYear        = 0.15
	weightDecade      = 0.08
	weightPublisher   = 0.1
)

const (
	franchisePrefix = "franchise:"
	singlePlayerTag = "1"

	// minUbiquitousTitles and ubiquitousDivisor set how many titles a tag
	// may be on before sharing it stops finding candidates: the larger of a
	// fixed count and one fiftieth of the library. The fixed count keeps a
	// library of a few thousand titles matching on its genres, where almost
	// every genre is on more than one title in fifty.
	minUbiquitousTitles = 500
	ubiquitousDivisor   = 50
	// ubiquitousDamping scales what such a tag adds to a score. Its real
	// count is not read past the cutoff, so it is credited as a fraction of
	// what a tag exactly at the cutoff would be.
	ubiquitousDamping = 0.25

	// sameSystemBoost favours a title on the system the seed is on.
	sameSystemBoost = 1.10
	// minSimilarity is the least two titles must share to count as alike.
	minSimilarity = 0.5
	// maxReasons is how many reasons a pick carries.
	maxReasons = 3
)

// facetTagTypes are the tag types read to describe a title.
var facetTagTypes = []string{ //nolint:gochecknoglobals // immutable list of tag types
	string(tags.TagTypeSearch),
	string(tags.TagTypeGenre),
	string(tags.TagTypePerspective),
	string(tags.TagTypeArt),
	string(tags.TagTypeBased),
	string(tags.TagTypeDeveloper),
	string(tags.TagTypePlayers),
	string(tags.TagTypeYear),
	string(tags.TagTypePublisher),
	string(tags.TagTypeRating),
}

// facet describes how one kind of tag takes part in a comparison.
type facet struct {
	code   string
	weight float64
	// strong facets are what make two titles alike on their own. A weak one
	// only adds to a match a strong one made.
	strong bool
}

// facetOf classifies a tag. ok is false for a tag that says nothing about
// what a title is like.
func facetOf(tagType, tag string) (f facet, ok bool) {
	switch tags.TagType(tagType) {
	case tags.TagTypeSearch:
		if strings.HasPrefix(tag, franchisePrefix) {
			return facet{code: ReasonSeries, weight: weightSeries, strong: true}, true
		}
	case tags.TagTypeGenre:
		return facet{code: ReasonGenre, weight: weightGenre, strong: true}, true
	case tags.TagTypePerspective:
		return facet{code: ReasonPerspective, weight: weightPerspective, strong: true}, true
	case tags.TagTypeArt:
		return facet{code: ReasonArt, weight: weightArt, strong: true}, true
	case tags.TagTypeBased:
		return facet{code: ReasonBased, weight: weightBased, strong: true}, true
	case tags.TagTypeDeveloper:
		return facet{code: ReasonDeveloper, weight: weightDeveloper, strong: true}, true
	case tags.TagTypePlayers:
		if tag != singlePlayerTag {
			return facet{code: ReasonPlayers, weight: weightPlayers}, true
		}
	case tags.TagTypePublisher:
		return facet{code: ReasonPublisher, weight: weightPublisher}, true
	default:
	}
	return facet{}, false
}

// profileTag is one comparable tag of a title.
type profileTag struct {
	tagType string
	tag     string
	facet   facet
	tagDBID int64
}

// titleProfile is what the index says a title is like.
type titleProfile struct {
	tagIDs     map[int64]struct{}
	facetCount map[string]int
	systemID   string
	slug       string
	tags       []profileTag
	series     []string
	titleDBID  int64
	year       int
	rating     int
}

func (p *titleProfile) genreFamilies() map[string]struct{} {
	families := make(map[string]struct{})
	for i := range p.tags {
		if p.tags[i].facet.code == ReasonGenre {
			families[genreFamily(p.tags[i].tag)] = struct{}{}
		}
	}
	return families
}

func genreFamily(tag string) string {
	family, _, _ := strings.Cut(tag, ":")
	return family
}

// buildProfiles groups facet rows into one profile per title.
func buildProfiles(rows []database.TitleFacetTag) map[int64]*titleProfile {
	profiles := make(map[int64]*titleProfile)
	for i := range rows {
		row := &rows[i]
		profile, ok := profiles[row.TitleDBID]
		if !ok {
			profile = &titleProfile{
				titleDBID:  row.TitleDBID,
				systemID:   row.SystemID,
				slug:       row.Slug,
				tagIDs:     make(map[int64]struct{}),
				facetCount: make(map[string]int),
			}
			profiles[row.TitleDBID] = profile
		}
		switch tags.TagType(row.Type) {
		case tags.TagTypeYear:
			if year, err := strconv.Atoi(row.Tag); err == nil && year > 0 && profile.year == 0 {
				profile.year = year
			}
			continue
		case tags.TagTypeRating:
			if rating, err := strconv.Atoi(row.Tag); err == nil && rating > profile.rating {
				profile.rating = rating
			}
			continue
		default:
		}
		f, ok := facetOf(row.Type, row.Tag)
		if !ok {
			continue
		}
		profile.tags = append(profile.tags, profileTag{
			tagDBID: row.TagDBID, tagType: row.Type, tag: row.Tag, facet: f,
		})
		profile.tagIDs[row.TagDBID] = struct{}{}
		profile.facetCount[f.code]++
		if f.code == ReasonSeries {
			profile.series = append(profile.series, row.Tag)
		}
	}
	return profiles
}

// rarity is how much a tag carried by df of n titles tells two titles apart:
// near zero for a tag most of the library has, high for a rare one.
func rarity(n, df int64) float64 {
	n = max(n, df)
	return math.Log(1 + (float64(n-df)+0.5)/(float64(df)+0.5))
}

// ubiquitousCutoff is the title count past which a tag no longer finds
// candidates.
func ubiquitousCutoff(titleCount int64) int {
	return int(max(minUbiquitousTitles, titleCount/ubiquitousDivisor))
}
