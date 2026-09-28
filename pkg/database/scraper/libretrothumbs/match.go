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

package libretrothumbs

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// thumbnailName applies libretro's file-name rule: these characters cannot
// appear in a thumbnail name and are replaced with an underscore.
func thumbnailName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|", r) {
			return '_'
		}
		return r
	}, name)
}

// mediaBaseName is a media row's file name without its extension. Source and
// other URI paths are percent-encoded; plain filesystem paths are not.
func mediaBaseName(mediaPath string) string {
	base := path.Base(strings.ReplaceAll(mediaPath, "\\", "/"))
	if strings.Contains(mediaPath, "://") {
		if unescaped, err := url.PathUnescape(base); err == nil {
			base = unescaped
		}
	}
	if ext := path.Ext(base); ext != "" && len(ext) <= 5 {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// mediaFolderName is the decoded name of the folder holding a media file.
func mediaFolderName(mediaPath string) string {
	folder := path.Base(path.Dir(strings.ReplaceAll(mediaPath, "\\", "/")))
	if strings.Contains(mediaPath, "://") {
		if unescaped, err := url.PathUnescape(folder); err == nil {
			folder = unescaped
		}
	}
	return folder
}

var (
	parenthetical = regexp.MustCompile(`\(([^()]*)\)`)
	// tosecDate is TOSEC's release-date group, such as "1987-09".
	tosecDate = regexp.MustCompile(`^(19|20)\d\d`)
)

// regionAliases folds the region spellings of No-Intro, TOSEC and GoodTools.
var regionAliases = map[string]string{
	"us": "usa", "u": "usa", "eu": "europe", "e": "europe", "jp": "japan", "j": "japan",
	"w": "world", "uk": "uk",
}

func regionTokens(name string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, group := range parenthetical.FindAllStringSubmatch(name, -1) {
		for _, token := range strings.Split(group[1], ",") {
			token = strings.ToLower(strings.TrimSpace(token))
			if alias, ok := regionAliases[token]; ok {
				token = alias
			}
			if token != "" {
				tokens[token] = struct{}{}
			}
		}
	}
	return tokens
}

var unwantedTags = []string{"beta", "proto", "demo", "sample", "hack", "pirate", "unl", "aftermarket"}

// defaultRegions breaks ties when a file names no region the index shares.
var defaultRegions = map[string]int{"usa": 6, "world": 5, "europe": 4, "japan": 2}

// score ranks one thumbnail candidate for a file. Shared regions win, then a
// clean No-Intro-style name over dump flags, pre-release builds and TOSEC
// dates, then the default region order.
func score(candidate string, wanted map[string]struct{}) int {
	s := 0
	tokens := regionTokens(candidate)
	for token := range tokens {
		if _, ok := wanted[token]; ok {
			s += 20
		}
		s += defaultRegions[token]
	}
	lower := strings.ToLower(candidate)
	if strings.Contains(lower, "[") {
		s -= 50
	}
	for token := range tokens {
		if tosecDate.MatchString(token) {
			s -= 20
			break
		}
	}
	for _, tag := range unwantedTags {
		for token := range tokens {
			if strings.HasPrefix(token, tag) {
				s -= 30
			}
		}
	}
	return s
}

// index is one playlist's thumbnail names, for exact and title matching.
type index struct {
	exact  map[string]string
	folded map[string]string
	bySlug map[string][]string
}

func newIndex(names []string) *index {
	ix := &index{
		exact:  make(map[string]string, len(names)),
		folded: make(map[string]string, len(names)),
		bySlug: make(map[string][]string),
	}
	for _, name := range names {
		ix.exact[name] = name
		if _, taken := ix.folded[strings.ToLower(name)]; !taken {
			ix.folded[strings.ToLower(name)] = name
		}
		if slug := slugs.Slugify(slugs.MediaTypeGame, name); slug != "" {
			ix.bySlug[slug] = append(ix.bySlug[slug], name)
		}
	}
	return ix
}

// match finds the thumbnail for a file: its own name when the set is named
// the libretro way, otherwise the best-ranked name with the same title.
func (ix *index) match(fileBase, titleSlug string) (string, bool) {
	name := thumbnailName(fileBase)
	if found, ok := ix.exact[name]; ok {
		return found, true
	}
	if found, ok := ix.folded[strings.ToLower(name)]; ok {
		return found, true
	}
	wanted := regionTokens(fileBase)
	for _, slug := range []string{titleSlug, slugs.Slugify(slugs.MediaTypeGame, fileBase)} {
		candidates := ix.bySlug[slug]
		if slug == "" || len(candidates) == 0 {
			continue
		}
		best, bestScore := "", 0
		for _, candidate := range candidates {
			s := score(candidate, wanted)
			if best == "" || s > bestScore || (s == bestScore && candidate < best) {
				best, bestScore = candidate, s
			}
		}
		return best, true
	}
	return "", false
}
