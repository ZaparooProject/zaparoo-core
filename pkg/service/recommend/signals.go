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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// Signal names say which sign of interest made a title a seed.
const (
	SignalPlayed   = "played"
	SignalFavorite = "favorite"
	SignalLiked    = "liked"
	SignalDeck     = "deck"
	// SignalPlayLater marks a queue item the profile marked to play later.
	SignalPlayLater = "playLater"
)

const (
	// MinSessionSecs is the shortest session that counts as play. Anything
	// shorter is a launch that was closed again.
	MinSessionSecs = 60
	// playWindow is how far back play is read, and maxPlayRows how many
	// played paths are weighed, most recent first.
	playWindow  = 365 * 24 * time.Hour
	maxPlayRows = 2000

	// Play counts for more the longer it ran, levelling off at
	// playSaturationHours, and for less the longer ago it was, halving every
	// playHalfLifeDays down to playDecayFloor.
	playSaturationHours = 20.0
	playHalfLifeDays    = 45.0
	playDecayFloor      = 0.15

	favoriteAffinity = 1.0
	likedAffinity    = 0.8
	// A deck can hold a hundred games, so being in one says little: less
	// than a quarter of an hour of recent play, or decks would take every
	// seed from what was actually played.
	ownedDeckAffinity = 0.07
	// minSeedAffinity is the least interest that makes a title a seed: about
	// ten minutes of recent play.
	minSeedAffinity = 0.05

	hoursPerDay     = 24.0
	secondsPerHour  = 3600.0
	halfLifeDecayTo = 0.5
)

// MediaStore is the part of the media index recommendations read.
type MediaStore interface {
	GetTitleFacetTags(ctx context.Context, titleDBIDs []int64, tagTypes []string) ([]database.TitleFacetTag, error)
	CountTitlesByTag(ctx context.Context, tagDBIDs []int64, limit int) (map[int64]int, error)
	FindTitlesSharingTags(
		ctx context.Context, weighted []database.WeightedTag, excludeTitleDBIDs []int64,
		systemIDs []string, limit int,
	) ([]database.TagSharingTitle, error)
	GetUserTagMedia(ctx context.Context) ([]database.UserTagMedia, error)
	MediaTitleCountEstimate(ctx context.Context) (int64, error)
	GetTitleVersionGroups(ctx context.Context, titleDBIDs []int64) (map[int64]int64, error)
	GetVersionGroupMembers(ctx context.Context, titleDBIDs []int64) (map[int64][]int64, error)
}

// UserStore is the part of the user database recommendations read.
type UserStore interface {
	GetMediaPlayTotals(
		ctx context.Context, profileID string, since time.Time, minSessionSecs, limit int,
	) ([]database.MediaPlayTotal, error)
	ListDecks() ([]database.Deck, error)
}

// Deps is what gathering signals and building a plan read from.
type Deps struct {
	MediaDB MediaStore
	UserDB  UserStore
	// ResolvePlays maps each play total, by its index, to the title its
	// path belongs to. Totals whose media is no longer indexed are absent.
	ResolvePlays func(ctx context.Context, plays []database.MediaPlayTotal) (map[int]int64, error)
}

// GameSignal is the interest shown in one game: every version of it added up.
type GameSignal struct {
	LastPlayed time.Time
	// TitleDBID is the title that stands for the game: the version with the
	// most play, or failing that a flagged one.
	TitleDBID int64
	Group     int64
	PlaySecs  int64
	Favorite  bool
	Liked     bool
	Disliked  bool
	Hidden    bool
	PlayLater bool
	OwnedDeck bool
}

// Signals is the interest a profile has shown, keyed by version group.
type Signals struct {
	ByGroup map[int64]*GameSignal
}

// titleSignal is the interest shown in one title before versions are merged.
type titleSignal struct {
	lastPlayed time.Time
	playSecs   int64
	favorite   bool
	liked      bool
	disliked   bool
	hidden     bool
	playLater  bool
	ownedDeck  bool
}

// GatherSignals collects every sign of interest for one profile: its play
// history, the flags on the library, and the decks made on this device. An
// empty profileID is the shared profile, whose play is what was played with
// no profile active. Flags and decks are read from the library's own copy of
// them, so they follow whatever that copy holds.
func GatherSignals(ctx context.Context, deps *Deps, profileID string, now time.Time) (*Signals, error) {
	byTitle := make(map[int64]*titleSignal)
	at := func(titleDBID int64) *titleSignal {
		signal, ok := byTitle[titleDBID]
		if !ok {
			signal = &titleSignal{}
			byTitle[titleDBID] = signal
		}
		return signal
	}

	if err := gatherPlay(ctx, deps, profileID, now, at); err != nil {
		return nil, err
	}
	if err := gatherFlags(ctx, deps, at); err != nil {
		return nil, err
	}

	signals := &Signals{ByGroup: make(map[int64]*GameSignal)}
	if len(byTitle) == 0 {
		return signals, nil
	}
	titleDBIDs := make([]int64, 0, len(byTitle))
	for titleDBID := range byTitle {
		titleDBIDs = append(titleDBIDs, titleDBID)
	}
	slices.Sort(titleDBIDs)
	groups, err := deps.MediaDB.GetTitleVersionGroups(ctx, titleDBIDs)
	if err != nil {
		return nil, fmt.Errorf("look up signal version groups: %w", err)
	}
	// Ascending title order makes the stand-in title of a game the same on
	// every run when two versions tie.
	standInPlay := make(map[int64]int64)
	for _, titleDBID := range titleDBIDs {
		title := byTitle[titleDBID]
		group := versionGroupOf(groups, titleDBID)
		game, ok := signals.ByGroup[group]
		if !ok {
			game = &GameSignal{Group: group, TitleDBID: titleDBID}
			signals.ByGroup[group] = game
			standInPlay[group] = title.playSecs
		} else if title.playSecs > standInPlay[group] {
			game.TitleDBID = titleDBID
			standInPlay[group] = title.playSecs
		}
		game.PlaySecs += title.playSecs
		if title.lastPlayed.After(game.LastPlayed) {
			game.LastPlayed = title.lastPlayed
		}
		game.Favorite = game.Favorite || title.favorite
		game.Liked = game.Liked || title.liked
		game.Disliked = game.Disliked || title.disliked
		game.Hidden = game.Hidden || title.hidden
		game.PlayLater = game.PlayLater || title.playLater
		game.OwnedDeck = game.OwnedDeck || title.ownedDeck
	}
	for _, game := range signals.ByGroup {
		// A game someone favorited or liked in one version is not disliked
		// because another version was.
		if game.Favorite || game.Liked {
			game.Disliked = false
		}
	}
	return signals, nil
}

func gatherPlay(
	ctx context.Context, deps *Deps, profileID string, now time.Time, at func(int64) *titleSignal,
) error {
	if deps.UserDB == nil || deps.ResolvePlays == nil {
		return nil
	}
	plays, err := deps.UserDB.GetMediaPlayTotals(ctx, profileID, now.Add(-playWindow), MinSessionSecs, maxPlayRows)
	if err != nil {
		return fmt.Errorf("read play totals: %w", err)
	}
	if len(plays) == 0 {
		return nil
	}
	resolved, err := deps.ResolvePlays(ctx, plays)
	if err != nil {
		return fmt.Errorf("resolve played media: %w", err)
	}
	for i := range plays {
		titleDBID, ok := resolved[i]
		if !ok || titleDBID <= 0 {
			continue
		}
		signal := at(titleDBID)
		signal.playSecs += int64(plays[i].PlayTime)
		if plays[i].LastPlayedAt.After(signal.lastPlayed) {
			signal.lastPlayed = plays[i].LastPlayedAt
		}
	}
	return nil
}

func gatherFlags(ctx context.Context, deps *Deps, at func(int64) *titleSignal) error {
	tagged, err := deps.MediaDB.GetUserTagMedia(ctx)
	if err != nil {
		return fmt.Errorf("read user tags: %w", err)
	}
	var ownedDecks map[string]struct{}
	for i := range tagged {
		row := &tagged[i]
		switch tags.TagValue(row.Tag) {
		case tags.TagUserFavorite:
			at(row.TitleDBID).favorite = true
		case tags.TagUserLiked:
			at(row.TitleDBID).liked = true
		case tags.TagUserDisliked:
			at(row.TitleDBID).disliked = true
		case tags.TagUserPlayLater:
			at(row.TitleDBID).playLater = true
		case tags.TagUserHidden:
			at(row.TitleDBID).hidden = true
		default:
			deckID, isDeck := strings.CutPrefix(row.Tag, tags.TagUserDeckPrefix)
			if !isDeck || deps.UserDB == nil {
				continue
			}
			if ownedDecks == nil {
				ownedDecks, err = listOwnedDecks(deps.UserDB)
				if err != nil {
					return err
				}
			}
			// A deck fetched from someone else's link says what they like.
			if _, owned := ownedDecks[deckID]; owned {
				at(row.TitleDBID).ownedDeck = true
			}
		}
	}
	return nil
}

func listOwnedDecks(userDB UserStore) (map[string]struct{}, error) {
	decks, err := userDB.ListDecks()
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	owned := make(map[string]struct{}, len(decks))
	for i := range decks {
		if decks[i].Owned {
			owned[decks[i].DeckID] = struct{}{}
		}
	}
	return owned, nil
}

// playAffinity is how much a game's play says it is liked: more for longer
// play, less for older play.
func (g *GameSignal) playAffinity(now time.Time) float64 {
	if g.PlaySecs <= 0 {
		return 0
	}
	hours := float64(g.PlaySecs) / secondsPerHour
	amount := min(1, math.Log1p(hours)/math.Log1p(playSaturationHours))
	days := max(now.Sub(g.LastPlayed).Hours()/hoursPerDay, 0)
	return amount * max(playDecayFloor, math.Pow(halfLifeDecayTo, days/playHalfLifeDays))
}

// Affinity is how strongly the profile is drawn to the game, from 0 to 1, and
// the sign of interest that says so. The strongest sign decides: a favorite
// barely played is still a favorite. A disliked game has none, and neither
// does one with a hidden version: hiding it says not to bring it up.
func (g *GameSignal) Affinity(now time.Time) (affinity float64, signal string) {
	if g.Disliked || g.Hidden {
		return 0, ""
	}
	type candidate struct {
		signal string
		value  float64
	}
	// Listed strongest claim first, so a tie goes to the more deliberate sign.
	candidates := []candidate{
		{signal: SignalFavorite, value: boolAffinity(g.Favorite, favoriteAffinity)},
		{signal: SignalLiked, value: boolAffinity(g.Liked, likedAffinity)},
		{signal: SignalPlayed, value: g.playAffinity(now)},
		{signal: SignalDeck, value: boolAffinity(g.OwnedDeck, ownedDeckAffinity)},
	}
	best := slices.MaxFunc(candidates, func(a, b candidate) int { return cmp.Compare(a.value, b.value) })
	if best.value <= 0 {
		return 0, ""
	}
	return best.value, best.signal
}

func boolAffinity(set bool, value float64) float64 {
	if set {
		return value
	}
	return 0
}
