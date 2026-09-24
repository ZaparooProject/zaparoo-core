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

// Package arcadegenre maps the MiSTer arcade catalog's categories onto the
// canonical genre values. The mister-arcade scraper writes them, and MediaDB
// uses them to carry genres an earlier Core stored before the tag vocabulary
// was closed.
package arcadegenre

import (
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// genreTable maps each catalog category to the canonical genre values it
// states, most specific first. genreTags adds a value's parent ("shmup" for
// "shmup:v"), so the table lists only the specific value. Keys are
// categoryKey spellings: lower-case, with the catalog's "[Mature]" rating
// suffix removed because it states an audience, not a genre.
//
// The catalog writes "family - specialisation". The families line up with
// GameDataBase's genres as follows: a scrolling or fixed "Shooter" that flies
// or drives is a shmup (split by scroll direction, diagonal being isometric),
// one aimed at targets or through a lightgun is shooting, a "Fighter" that
// scrolls through crowds is a brawler and a one-on-one "Versus" fighter is
// fighting, "Platform" is action:platformer except the scrolling shooters,
// which are run-and-gun, every "Maze" is action:maze, "Ball & Paddle" is
// action:blockbreaker, "Driving" is racing, "Tabletop" is board. Where the
// specialisation says nothing a genre can carry ("Puzzle - Misc."), only the
// family's genre is written.
//
//nolint:gochecknoglobals // Static mapping table.
var genreTable = map[string][]tags.TagValue{
	"arcade - pinball":                 {tags.TagGameGenreParlorPinball},
	"ball & paddle - breakout":         {tags.TagGameGenreActionBlockbreaker},
	"ball & paddle - jump and touch":   {tags.TagGameGenreActionBlockbreaker},
	"casino - cards":                   {tags.TagGameGenreBoardCards},
	"climbing - building":              {tags.TagGameGenreAction},
	"climbing - tree - plant":          {tags.TagGameGenreAction},
	"driving - 1st person":             {tags.TagGameGenreRacing},
	"driving - boat":                   {tags.TagGameGenreRacingDriving},
	"driving - demolition derby":       {tags.TagGameGenreRacingCombat},
	"driving - landing":                {tags.TagGameGenreSimFlight},
	"driving - motorbike":              {tags.TagGameGenreRacing},
	"driving - race":                   {tags.TagGameGenreRacing},
	"driving - race (chase view)":      {tags.TagGameGenreRacing},
	"driving - race track":             {tags.TagGameGenreRacing},
	"fighter - 2.5d":                   {tags.TagGameGenreBrawler},
	"fighter - 2d":                     {tags.TagGameGenreBrawler},
	"fighter - field":                  {tags.TagGameGenreAction},
	"fighter - versus":                 {tags.TagGameGenreFighting},
	"fighter - versus co-op":           {tags.TagGameGenreFighting},
	"fighter - vertical":               {tags.TagGameGenreBrawler},
	"maze":                             {tags.TagGameGenreActionMaze},
	"maze - ball guide":                {tags.TagGameGenreActionMaze},
	"maze - blocks":                    {tags.TagGameGenreActionMaze},
	"maze - collect":                   {tags.TagGameGenreActionMaze},
	"maze - cross":                     {tags.TagGameGenreActionMaze},
	"maze - defeat enemies":            {tags.TagGameGenreActionMaze},
	"maze - digging":                   {tags.TagGameGenreActionMaze},
	"maze - driving":                   {tags.TagGameGenreActionMaze},
	"maze - escape":                    {tags.TagGameGenreActionMaze},
	"maze - fighter":                   {tags.TagGameGenreActionMaze},
	"maze - horizontal":                {tags.TagGameGenreActionMaze},
	"maze - move and sort":             {tags.TagGameGenreActionMaze},
	"maze - outline":                   {tags.TagGameGenreActionMaze},
	"maze - paint":                     {tags.TagGameGenreActionMaze},
	"maze - shooter":                   {tags.TagGameGenreActionMaze},
	"maze - shooter large":             {tags.TagGameGenreActionMaze},
	"maze - shooter small":             {tags.TagGameGenreActionMaze},
	"maze - surround":                  {tags.TagGameGenreActionMaze},
	"medal game - versus":              {tags.TagGameGenreParlor},
	"misc. - mini-games":               {tags.TagGameGenreMinigames},
	"multigame - mini-games":           {tags.TagGameGenreMinigames},
	"multiplay - mini-games":           {tags.TagGameGenreMinigames},
	"platform - fighter":               {tags.TagGameGenreActionPlatformer},
	"platform - fighter scrolling":     {tags.TagGameGenreActionPlatformer},
	"platform - run jump":              {tags.TagGameGenreActionPlatformer},
	"platform - run, jump & scrolling": {tags.TagGameGenreActionPlatformer},
	"platform - shooter":               {tags.TagGameGenreActionPlatformer},
	"platform - shooter scrolling":     {tags.TagGameGenreActionRunAndGun},
	"puzzle - cards":                   {tags.TagGameGenrePuzzle},
	"puzzle - drop":                    {tags.TagGameGenrePuzzleDrop},
	"puzzle - match":                   {tags.TagGameGenrePuzzle},
	"puzzle - maze":                    {tags.TagGameGenrePuzzle},
	"puzzle - misc.":                   {tags.TagGameGenrePuzzle},
	"puzzle - outline":                 {tags.TagGameGenrePuzzle},
	"puzzle - reconstruction":          {tags.TagGameGenrePuzzle},
	"puzzle - sliding":                 {tags.TagGameGenrePuzzleMind},
	"puzzle - toss":                    {tags.TagGameGenrePuzzle},
	"quiz - questions in english":      {tags.TagGameGenreQuiz},
	"quiz - questions in japanese":     {tags.TagGameGenreQuiz},
	"racing":                           {tags.TagGameGenreRacing},
	"shooter - 1st person":             {tags.TagGameGenreShooting},
	// Pang and Pooyan: a fixed player aiming upward, not target shooting.
	"shooter - 3rd person":           {tags.TagGameGenreAction},
	"shooter - command":              {tags.TagGameGenreShooting},
	"shooter - driving":              {tags.TagGameGenreRacingCombat},
	"shooter - driving 1st person":   {tags.TagGameGenreShooting},
	"shooter - driving diagonal":     {tags.TagGameGenreShmupIsometric},
	"shooter - driving horizontal":   {tags.TagGameGenreShmupHorizontal},
	"shooter - driving vertical":     {tags.TagGameGenreShmupVertical},
	"shooter - field":                {tags.TagGameGenreShmup},
	"shooter - flying":               {tags.TagGameGenreShmup},
	"shooter - flying (chase view)":  {tags.TagGameGenreShmup},
	"shooter - flying 1st person":    {tags.TagGameGenreShooting},
	"shooter - flying diagonal":      {tags.TagGameGenreShmupIsometric},
	"shooter - flying horizontal":    {tags.TagGameGenreShmupHorizontal},
	"shooter - flying vertical":      {tags.TagGameGenreShmupVertical},
	"shooter - gallery":              {tags.TagGameGenreShootingGallery},
	"shooter - gun":                  {tags.TagGameGenreShooting},
	"shooter - misc. horizontal":     {tags.TagGameGenreShmupHorizontal},
	"shooter - misc. vertical":       {tags.TagGameGenreShmupVertical},
	"shooter - versus":               {tags.TagGameGenreShooting},
	"shooter - walking":              {tags.TagGameGenreActionRunAndGun},
	"slot machine - video slot":      {tags.TagGameGenreParlorJackpot},
	"sports - baseball":              {tags.TagGameGenreSportsBaseball},
	"sports - basketball":            {tags.TagGameGenreSportsBasketball},
	"sports - boxing":                {tags.TagGameGenreSportsBoxing},
	"sports - bull fighting":         {tags.TagGameGenreSports},
	"sports - cards":                 {tags.TagGameGenreSports},
	"sports - fishing":               {tags.TagGameGenreSimFishing},
	"sports - football":              {tags.TagGameGenreSportsFootball},
	"sports - golf":                  {tags.TagGameGenreSportsGolf},
	"sports - hockey":                {tags.TagGameGenreSportsHockey},
	"sports - misc.":                 {tags.TagGameGenreSports},
	"sports - multiplay":             {tags.TagGameGenreSports},
	"sports - ping pong":             {tags.TagGameGenreSportsPingpong},
	"sports - pool":                  {tags.TagGameGenreParlorBilliards},
	"sports - shuffleboard":          {tags.TagGameGenreParlor},
	"sports - skiing":                {tags.TagGameGenreSportsSkiing},
	"sports - soccer":                {tags.TagGameGenreSportsSoccer},
	"sports - tennis":                {tags.TagGameGenreSportsTennis},
	"sports - track & field":         {tags.TagGameGenreSports},
	"sports - volleyball":            {tags.TagGameGenreSportsVolleyball},
	"sports - wrestling":             {tags.TagGameGenreSportsWrestling},
	"ttl - ball & paddle - breakout": {tags.TagGameGenreActionBlockbreaker},
	"ttl - ball & paddle - pong":     {tags.TagGameGenreSportsPingpong},
	"tabletop - hanafuda":            {tags.TagGameGenreBoardHanafuda},
	"tabletop - mahjong":             {tags.TagGameGenreBoardMahjong},
	"tabletop - othello - reversi":   {tags.TagGameGenreBoardOthello},
	// Shanghai is tile-matching solitaire played with mahjong tiles.
	"tabletop - shanghai": {tags.TagGameGenrePuzzle},
}

// notGenres are catalog categories that deliberately write no genre. They are
// dropped without being reported as unmapped.
//
//nolint:gochecknoglobals // Static lookup set.
var notGenres = map[string]string{
	"system - bios":           "a BIOS is not a game",
	"multigame - compilation": "a compilation of unrelated games has no one genre",
	"misc. - hot-air balloon": "names a theme, not how the game plays",
	"shooter - misc.":         "does not say whether it is a shmup or a shooting game",
	"shooter":                 "the bare family does not say whether it is a shmup or a shooting game",
}

// Lookup resolves a category key (lower-case, rating suffix removed) to its
// genre values, each followed by its parent where the canonical list has one.
// known is false for a key in neither table, which the caller reports.
func Lookup(key string) (values []tags.TagValue, known bool) {
	if key == "" {
		return nil, true
	}
	if _, skip := notGenres[key]; skip {
		return nil, true
	}
	mapped, ok := genreTable[key]
	if !ok {
		return nil, false
	}
	for _, value := range mapped {
		values = appendNew(values, value)
		if parent, _, nested := strings.Cut(string(value), ":"); nested &&
			tags.IsCanonicalValue(tags.TagTypeGenre, tags.TagValue(parent)) {
			values = appendNew(values, tags.TagValue(parent))
		}
	}
	return values, true
}

// IsSkipped reports whether a category key deliberately writes no genre.
func IsSkipped(key string) bool {
	_, skip := notGenres[key]
	return skip
}

// Keys lists the mapped and the deliberately skipped category keys, sorted.
func Keys() (mapped, skipped []string) {
	for key := range genreTable {
		mapped = append(mapped, key)
	}
	for key := range notGenres {
		skipped = append(skipped, key)
	}
	sort.Strings(mapped)
	sort.Strings(skipped)
	return mapped, skipped
}

// legacyKeys indexes every category key by its letters and digits alone, the
// part of a category an earlier Core kept when it slugified the category into
// a free-form genre value: "Shooter - Flying Vertical" was stored as
// "shooter-flying-vertical".
//
//nolint:gochecknoglobals // Static lookup index derived from the tables above.
var legacyKeys = func() map[string]string {
	keys := make(map[string]string, len(genreTable)+len(notGenres))
	for key := range genreTable {
		keys[fold(key)] = key
	}
	for key := range notGenres {
		keys[fold(key)] = key
	}
	return keys
}()

func fold(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 0x7f {
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

// LegacyGenre resolves a genre value an earlier Core stored by slugifying a
// catalog category to the genre values a scrape writes for that category
// now. known is false for a value that matches no category, and true with no
// values for a category that deliberately writes none.
func LegacyGenre(value string) (values []tags.TagValue, known bool) {
	key, ok := legacyKeys[fold(value)]
	if !ok {
		return nil, false
	}
	return Lookup(key)
}

func appendNew(values []tags.TagValue, value tags.TagValue) []tags.TagValue {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
