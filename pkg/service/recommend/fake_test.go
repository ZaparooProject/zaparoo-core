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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// fakeSession is one recorded play session.
type fakeSession struct {
	started   time.Time
	profileID string
	titleDBID int64
	secs      int
}

type fakeTitle struct {
	system string
	slug   string
	tags   []string
	id     int64
	group  int64
}

// fakeLibrary is an in-memory index and history that answers the store
// interfaces the way the databases do.
type fakeLibrary struct {
	titles   map[int64]*fakeTitle
	tagIDs   map[string]int64
	userTags []database.UserTagMedia
	sessions []fakeSession
	decks    []database.Deck
	order    []int64
}

func newFakeLibrary() *fakeLibrary {
	return &fakeLibrary{titles: map[int64]*fakeTitle{}, tagIDs: map[string]int64{}}
}

// add indexes a title. Tags are written "type=value".
func (l *fakeLibrary) add(id int64, system string, titleTags ...string) {
	l.titles[id] = &fakeTitle{id: id, system: system, slug: fmt.Sprintf("title-%d", id), tags: titleTags, group: id}
	l.order = append(l.order, id)
	for _, tag := range titleTags {
		if _, ok := l.tagIDs[tag]; !ok {
			l.tagIDs[tag] = int64(len(l.tagIDs) + 1)
		}
	}
}

// port marks the given titles as one game held on several systems.
func (l *fakeLibrary) port(ids ...int64) {
	for _, id := range ids {
		l.titles[id].slug = l.titles[ids[0]].slug
	}
}

// link makes the given titles versions of one game.
func (l *fakeLibrary) link(ids ...int64) {
	group := slices.Min(ids)
	for _, id := range ids {
		l.titles[id].group = group
	}
}

func (l *fakeLibrary) flag(titleDBID int64, tag tags.TagValue) {
	l.userTags = append(l.userTags, database.UserTagMedia{
		Tag: string(tag), MediaDBID: titleDBID * 10, TitleDBID: titleDBID,
	})
}

func (l *fakeLibrary) play(profileID string, titleDBID int64, secs int, started time.Time) {
	l.sessions = append(l.sessions, fakeSession{
		profileID: profileID, titleDBID: titleDBID, secs: secs, started: started,
	})
}

func (l *fakeLibrary) deps() *Deps {
	return &Deps{MediaDB: l, UserDB: l, ResolvePlays: l.resolvePlays}
}

func (l *fakeLibrary) resolvePlays(_ context.Context, plays []database.MediaPlayTotal) (map[int]int64, error) {
	resolved := make(map[int]int64, len(plays))
	for i := range plays {
		id, err := strconv.ParseInt(plays[i].MediaPath, 10, 64)
		if err != nil {
			return nil, err //nolint:wrapcheck // test fake
		}
		if _, indexed := l.titles[id]; indexed {
			resolved[i] = id
		}
	}
	return resolved, nil
}

func (l *fakeLibrary) GetTitleFacetTags(
	_ context.Context, titleDBIDs []int64, tagTypes []string,
) ([]database.TitleFacetTag, error) {
	var rows []database.TitleFacetTag
	for _, id := range titleDBIDs {
		title, ok := l.titles[id]
		if !ok {
			continue
		}
		for _, tag := range title.tags {
			tagType, value, _ := strings.Cut(tag, "=")
			if !slices.Contains(tagTypes, tagType) {
				continue
			}
			rows = append(rows, database.TitleFacetTag{
				TitleDBID: id, SystemID: title.system, Slug: title.slug,
				TagDBID: l.tagIDs[tag], Type: tagType, Tag: value,
			})
		}
	}
	return rows, nil
}

func (l *fakeLibrary) CountTitlesByTag(_ context.Context, tagDBIDs []int64, limit int) (map[int64]int, error) {
	counts := make(map[int64]int, len(tagDBIDs))
	for _, tagDBID := range tagDBIDs {
		count := 0
		for _, title := range l.titles {
			for _, tag := range title.tags {
				if l.tagIDs[tag] == tagDBID {
					count++
				}
			}
		}
		counts[tagDBID] = min(count, limit)
	}
	return counts, nil
}

func (l *fakeLibrary) FindTitlesSharingTags(
	_ context.Context, weighted []database.WeightedTag, exclude []int64, limit int,
) ([]database.TagSharingTitle, error) {
	var found []database.TagSharingTitle
	for _, id := range l.order {
		if slices.Contains(exclude, id) {
			continue
		}
		score := 0.0
		for _, tag := range l.titles[id].tags {
			for i := range weighted {
				if weighted[i].TagDBID == l.tagIDs[tag] {
					score += weighted[i].Weight
				}
			}
		}
		if score > 0 {
			found = append(found, database.TagSharingTitle{TitleDBID: id, Score: score})
		}
	}
	slices.SortFunc(found, func(a, b database.TagSharingTitle) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.TitleDBID, b.TitleDBID))
	})
	return found[:min(len(found), limit)], nil
}

func (l *fakeLibrary) GetUserTagMedia(context.Context) ([]database.UserTagMedia, error) {
	return l.userTags, nil
}

func (l *fakeLibrary) MediaTitleCountEstimate(context.Context) (int64, error) {
	return int64(len(l.titles)), nil
}

func (l *fakeLibrary) GetTitleVersionGroups(_ context.Context, titleDBIDs []int64) (map[int64]int64, error) {
	groups := make(map[int64]int64, len(titleDBIDs))
	for _, id := range titleDBIDs {
		if title, ok := l.titles[id]; ok {
			groups[id] = title.group
		}
	}
	return groups, nil
}

func (l *fakeLibrary) GetVersionGroupMembers(_ context.Context, titleDBIDs []int64) (map[int64][]int64, error) {
	members := make(map[int64][]int64)
	for _, id := range titleDBIDs {
		title, ok := l.titles[id]
		if !ok {
			continue
		}
		var group []int64
		for _, other := range l.order {
			if l.titles[other].group == title.group {
				group = append(group, other)
			}
		}
		if len(group) > 1 {
			members[id] = group
		}
	}
	return members, nil
}

func (l *fakeLibrary) GetMediaPlayTotals(
	_ context.Context, profileID string, since time.Time, minSessionSecs, limit int,
) ([]database.MediaPlayTotal, error) {
	byTitle := make(map[int64]*database.MediaPlayTotal)
	var order []int64
	for _, session := range l.sessions {
		if session.profileID != profileID || session.secs < minSessionSecs || session.started.Before(since) {
			continue
		}
		total, ok := byTitle[session.titleDBID]
		if !ok {
			total = &database.MediaPlayTotal{
				SystemID: "any", MediaPath: strconv.FormatInt(session.titleDBID, 10),
			}
			byTitle[session.titleDBID] = total
			order = append(order, session.titleDBID)
		}
		total.PlayTime += session.secs
		total.SessionCount++
		if session.started.After(total.LastPlayedAt) {
			total.LastPlayedAt = session.started
		}
	}
	totals := make([]database.MediaPlayTotal, 0, len(order))
	for _, id := range order {
		totals = append(totals, *byTitle[id])
	}
	slices.SortStableFunc(totals, func(a, b database.MediaPlayTotal) int {
		return b.LastPlayedAt.Compare(a.LastPlayedAt)
	})
	return totals[:min(len(totals), limit)], nil
}

func (l *fakeLibrary) ListDecks() ([]database.Deck, error) {
	return l.decks, nil
}

const (
	tagPlatformer = "genre=action:platformer"
	tagMaze       = "genre=action:maze"
	tagShooter    = "genre=shooter:vertical"
	tagRPG        = "genre=rpg:j"
	tagPuzzle     = "genre=puzzle"
	tagMario      = "search=franchise:mario"
	tagZelda      = "search=franchise:zelda"
	tagNintendo   = "developer=nintendo"
)

// seriesLibrary is a library with enough of each kind of game for full
// sections: Mario platformers, Zelda adventures, shooters, RPGs and puzzle
// games across two systems, plus titles the scrapers know nothing about.
//
//	 1-10  Mario platformers (NES 1-6, SNES 7-10)
//	11-18  Zelda RPGs (NES)
//	21-32  shooters (Genesis)
//	41-50  RPGs (SNES)
//	61-66  puzzle games (NES)
//	71-80  platformers with no series (Genesis)
//	91-95  untagged
func seriesLibrary() *fakeLibrary {
	lib := newFakeLibrary()
	for id := int64(1); id <= 10; id++ {
		system := "NES"
		if id > 6 {
			system = "SNES"
		}
		lib.add(id, system, tagPlatformer, tagMario, tagNintendo)
	}
	for id := int64(11); id <= 18; id++ {
		lib.add(id, "NES", tagRPG, tagZelda, tagNintendo)
	}
	for id := int64(21); id <= 32; id++ {
		lib.add(id, "Genesis", tagShooter)
	}
	for id := int64(41); id <= 50; id++ {
		lib.add(id, "SNES", tagRPG)
	}
	for id := int64(61); id <= 66; id++ {
		lib.add(id, "NES", tagPuzzle)
	}
	for id := int64(71); id <= 80; id++ {
		lib.add(id, "Genesis", tagPlatformer)
	}
	for id := int64(91); id <= 95; id++ {
		lib.add(id, "NES")
	}
	return lib
}
