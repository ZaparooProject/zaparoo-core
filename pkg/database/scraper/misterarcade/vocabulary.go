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

package misterarcade

import (
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

// categoryKey folds a catalog category onto its genreTable key.
func categoryKey(category string) string {
	key := strings.ToLower(field(category))
	key = strings.TrimSpace(strings.TrimSuffix(key, "[mature]"))
	return key
}

// genreTags resolves a category to its genre values, each followed by its
// parent where the canonical list has one. known is false for a category in
// neither genreTable nor notGenres, which the caller reports.
func genreTags(category string) (values []tags.TagValue, known bool) {
	key := categoryKey(category)
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

func appendNew(values []tags.TagValue, value tags.TagValue) []tags.TagValue {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// notFranchises are catalog series that deliberately write no franchise.
//
//nolint:gochecknoglobals // Static lookup set.
var notFranchises = map[string]string{
	"marvel - capcom": "Capcom's Marvel licence line, spanning Marvel Super Heroes, " +
		"X-Men vs. Street Fighter and Marvel vs. Capcom",
	"moero!!": "a Jaleco title prefix shared by unrelated series",
	"othello": "a board game rather than a series; the genre says board:othello",
}

// boardSkips are catalog platforms that name no single board: a CPU used
// across unrelated boards, a vendor's catch-all for one-off hardware, a
// licensing arrangement, or discrete logic built per game. They write no
// arcadeboard and are not reported as unmapped.
//
//nolint:gochecknoglobals // Static lookup set.
var boardSkips = map[string]string{
	"atari 6502":              "a CPU used across unrelated Atari boards",
	"atari 68000":             "a CPU, not one board",
	"atari discrete hardware": "discrete logic, one circuit per game",
	"atari vector":            "a display technology spanning several Atari boards",
	"capcom cps-0":            "a community label for Capcom's many unrelated pre-CPS boards",
	"computer space":          "a single discrete-logic game",
	"data east unique":        "a catch-all for one-off boards",
	"data east z80 based":     "a CPU, not one board",
	"exidy licensed":          "a licensing arrangement, not hardware",
	"irem unique":             "a catch-all for one-off boards",
	"jaleco unique":           "a catch-all for one-off boards",
	"konami 6309 based":       "a CPU, not one board",
	"konami 6809 based":       "a CPU, not one board",
	"konami dual 6809 based":  "a CPU arrangement, not one board",
	"konami unique":           "a catch-all for one-off boards",
	"konami z80":              "a CPU, not one board",
	"mylstar":                 "a company name, not a board",
	"namco unique":            "a catch-all for one-off boards",
	"nintendo arcade":         "a company-wide grouping, not one board",
	"rare unique hardware":    "a catch-all for one-off boards",
	"snk unique":              "a catch-all for one-off boards",
	"sega unique":             "a catch-all for one-off boards",
	"sega z80":                "a CPU, not one board",
	"seta 68000 based":        "a CPU grouping across Seta's boards, not one board",
	"taito 68000 based":       "a CPU, not one board",
	"taito licensed":          "a licensing arrangement, not hardware",
	"taito unique":            "a catch-all for one-off boards",
	"taito z80":               "a CPU, not one board",
	"technos 6309 based":      "a CPU, not one board",
	"technos unique":          "a catch-all for one-off boards",
	"upl unique":              "a catch-all for one-off boards",
}

// skipKey folds a catalog value onto a skip-set key.
func skipKey(value string) string {
	return strings.ToLower(field(value))
}
