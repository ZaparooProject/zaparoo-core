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

// Package ssgenre maps ScreenScraper genre names onto Core's genre
// vocabulary. ScreenScraper is where most EmulationStation gamelists and the
// MiSTer artwork packs get their genres, so both scrapers share this table.
package ssgenre

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// maxJoinedSegments bounds how many separator-split pieces one name can
// span: "Shooter / Vehicle, Horizontal" is three.
const maxJoinedSegments = 4

// sourceGenres maps a source genre name onto canonical genre values. A value
// with a parent in the vocabulary also gets the parent, so a name needs to
// list only its most specific value. An empty list drops the name on purpose:
// it is not a genre, or the vocabulary has no honest value for it.
//
// ScreenScraper writes a subgenre as "Parent / Child". The English names are
// the ones gamelists and the artwork packs carry; the list was built from the
// distinct names in the published MiSTer artwork pack gameinfo.tsv files, plus
// the names other EmulationStation scrapers (TheGamesDB, LaunchBox,
// Skyscraper) write.
var sourceGenres = map[string][]tags.TagValue{
	// Pieces seen in real gamelists, written as ES-DE's "Parent-Child" or
	// "Parent / Child" compounds. A perspective or format piece on its own
	// ("2D", "Versus") is not a genre; the compound around it is mapped.
	"Shooter / 1st person": {tags.TagGameGenreShootingFPS},
	"Shooter / 3rd person": {tags.TagGameGenreShootingTPS},
	"Soccer":               {tags.TagGameGenreSportsSoccer},
	"Motorcycle":           {tags.TagGameGenreRacing},
	"Race":                 {tags.TagGameGenreRacing},
	"1st person":           {},
	"3rd person":           {},
	"2D":                   {},
	"3D":                   {},
	"Versus":               {},
	"Vehicle":              {},

	// Action
	"Action":                          {tags.TagGameGenreAction},
	"Action / Labyrinth":              {tags.TagGameGenreActionMaze},
	"Action / Breakout games":         {tags.TagGameGenreActionBlockbreaker},
	"Action / Climbing":               {tags.TagGameGenreAction},
	"Action / Adventure":              {tags.TagGameGenreAction, tags.TagGameGenreAdventure},
	"Action / Platform":               {tags.TagGameGenreActionPlatformer},
	"Platform":                        {tags.TagGameGenreActionPlatformer},
	"Platform / Run & Jump":           {tags.TagGameGenreActionPlatformer},
	"Platform / Run & Jump Scrolling": {tags.TagGameGenreActionPlatformer},
	"Platform / Fighter Scrolling":    {tags.TagGameGenreActionPlatformer},
	"Platform / Shooter Scrolling":    {tags.TagGameGenreActionPlatformer, tags.TagGameGenreActionRunAndGun},
	"Platformer":                      {tags.TagGameGenreActionPlatformer},
	"Shooter / Run and Gun":           {tags.TagGameGenreActionRunAndGun},
	"Run and Gun":                     {tags.TagGameGenreActionRunAndGun},
	"Run & Gun":                       {tags.TagGameGenreActionRunAndGun},
	"Maze":                            {tags.TagGameGenreActionMaze},
	"Labyrinth":                       {tags.TagGameGenreActionMaze},
	"Breakout":                        {tags.TagGameGenreActionBlockbreaker},
	"Hack and Slash":                  {tags.TagGameGenreActionHackAndSlash},
	"Hack'n Slash":                    {tags.TagGameGenreActionHackAndSlash},
	"Metroidvania":                    {tags.TagGameGenreActionMetroidvania},
	"Roguelike":                       {tags.TagGameGenreActionRoguelite},
	"Roguelite":                       {tags.TagGameGenreActionRoguelite},

	// Adventure
	"Adventure":                     {tags.TagGameGenreAdventure},
	"Adventure / Graphic":           {tags.TagGameGenreAdventure},
	"Adventure / Interactive Movie": {tags.TagGameGenreAdventure},
	"Adventure / Point and Click":   {tags.TagGameGenreAdventurePointClick},
	"Adventure / Survival Horror":   {tags.TagGameGenreAdventureSurvivalHorror},
	"Adventure / Text":              {tags.TagGameGenreAdventureText},
	"Adventure / Visual Novel":      {tags.TagGameGenreAdventureVisualNovel},
	"Point and Click":               {tags.TagGameGenreAdventurePointClick},
	"Point & Click":                 {tags.TagGameGenreAdventurePointClick},
	"Visual Novel":                  {tags.TagGameGenreAdventureVisualNovel},
	"Survival Horror":               {tags.TagGameGenreAdventureSurvivalHorror},
	"Text Adventure":                {tags.TagGameGenreAdventureText},
	"Interactive Fiction":           {tags.TagGameGenreAdventureText},

	// Board and card games
	"Board game":         {tags.TagGameGenreBoard},
	"Board":              {tags.TagGameGenreBoard},
	"Asiatic board game": {tags.TagGameGenreBoard},
	"Renju":              {tags.TagGameGenreBoard},
	"Mahjong":            {tags.TagGameGenreBoardMahjong},
	"Shougi":             {tags.TagGameGenreBoardShougi},
	"Shogi":              {tags.TagGameGenreBoardShougi},
	"Go":                 {tags.TagGameGenreBoardGo},
	"Hanafuda":           {tags.TagGameGenreBoardHanafuda},
	"Othello":            {tags.TagGameGenreBoardOthello},
	"Reversi":            {tags.TagGameGenreBoardReversi},
	"Chess":              {tags.TagGameGenreBoardChess},
	"Backgammon":         {tags.TagGameGenreBoardBackgammon},
	"Playing cards":      {tags.TagGameGenreBoardCards},
	"Card Game":          {tags.TagGameGenreBoardCards},
	"Cards":              {tags.TagGameGenreBoardCards},
	"Casino / Cards":     {tags.TagGameGenreBoardCards},
	"Party":              {tags.TagGameGenreBoardParty},

	// Fighting and brawlers
	"Fight":               {tags.TagGameGenreFighting},
	"Fighting":            {tags.TagGameGenreFighting},
	"Fighting / 2D":       {tags.TagGameGenreFighting},
	"Fighting / 2.5D":     {tags.TagGameGenreFighting},
	"Fighting / 3D":       {tags.TagGameGenreFighting},
	"Fighting / Versus":   {tags.TagGameGenreFighting},
	"Fighting / Vs Co-op": {tags.TagGameGenreFighting},
	"Fighting / Vertical": {tags.TagGameGenreFighting},
	"Beat'em Up":          {tags.TagGameGenreBrawler},
	"Beat 'em Up":         {tags.TagGameGenreBrawler},
	"Brawler":             {tags.TagGameGenreBrawler},

	// Parlor and casino
	"Casino":                        {tags.TagGameGenreParlor},
	"Casino / Lottery":              {tags.TagGameGenreParlor},
	"Casino / Race":                 {tags.TagGameGenreParlor},
	"Casino / Roulette":             {tags.TagGameGenreParlor},
	"Casino / Slot machine":         {tags.TagGameGenreParlorJackpot},
	"Pachinko":                      {tags.TagGameGenreParlorPachinko},
	"Pinball":                       {tags.TagGameGenreParlorPinball},
	"Sports / Pool":                 {tags.TagGameGenreParlorBilliards},
	"Billiards":                     {tags.TagGameGenreParlorBilliards},
	"Sports / Bowling":              {tags.TagGameGenreParlorBowling},
	"Bowling":                       {tags.TagGameGenreParlorBowling},
	"Sports / Darts":                {tags.TagGameGenreParlorDarts},
	"Various / Electro- Mechanical": {tags.TagGameGenreParlorMechanical},

	// Quiz
	"Quiz":                  {tags.TagGameGenreQuiz},
	"Quiz / English":        {tags.TagGameGenreQuiz},
	"Quiz / French":         {tags.TagGameGenreQuiz},
	"Quiz / German":         {tags.TagGameGenreQuiz},
	"Quiz / Italian":        {tags.TagGameGenreQuiz},
	"Quiz / Japanese":       {tags.TagGameGenreQuiz},
	"Quiz / Korean":         {tags.TagGameGenreQuiz},
	"Quiz / Spanish":        {tags.TagGameGenreQuiz},
	"Quiz / Music English":  {tags.TagGameGenreQuiz},
	"Quiz / Music Japanese": {tags.TagGameGenreQuiz},
	"Trivia":                {tags.TagGameGenreQuiz},

	// Racing
	"Racing, Driving":                {tags.TagGameGenreRacing},
	"Race, Driving":                  {tags.TagGameGenreRacing},
	"Racing, Driving / Racing":       {tags.TagGameGenreRacing},
	"Racing, Driving / Motorcycle":   {tags.TagGameGenreRacing},
	"Racing, Driving / Boat":         {tags.TagGameGenreRacing},
	"Racing, Driving / Plane":        {tags.TagGameGenreRacing},
	"Racing, Driving / Hang Gliding": {tags.TagGameGenreRacing},
	"Racing FPV":                     {tags.TagGameGenreRacing},
	"Racing TPV":                     {tags.TagGameGenreRacing},
	"Motorcycle race FPV":            {tags.TagGameGenreRacing},
	"Motorcycle race TPV":            {tags.TagGameGenreRacing},
	"Racing":                         {tags.TagGameGenreRacing},
	"Driving":                        {tags.TagGameGenreRacing},

	// Role-playing
	"Role Playing Game":   {tags.TagGameGenreRPG},
	"Role-Playing":        {tags.TagGameGenreRPG},
	"Role Playing":        {tags.TagGameGenreRPG},
	"Role playing games":  {tags.TagGameGenreRPG},
	"RPG":                 {tags.TagGameGenreRPG},
	"Party-Based RPG":     {tags.TagGameGenreRPG},
	"Action RPG":          {tags.TagGameGenreRPGAction},
	"Japanese RPG":        {tags.TagGameGenreRPGJapanese},
	"Tactical RPG":        {tags.TagGameGenreRPGStrategy},
	"Dungeon Crawler RPG": {tags.TagGameGenreRPGDungeonCrawler},
	"Dungeon Crawler":     {tags.TagGameGenreRPGDungeonCrawler},
	"MMORPG":              {tags.TagGameGenreRPGMMO},

	// Rhythm
	"Rhythm":            {tags.TagGameGenreRhythm},
	"Rhythm / Music":    {tags.TagGameGenreRhythm},
	"Music":             {tags.TagGameGenreRhythm},
	"Music and Dancing": {tags.TagGameGenreRhythm},
	"Music and Dance":   {tags.TagGameGenreRhythmDance},
	"Dancing":           {tags.TagGameGenreRhythmDance},
	"Dance":             {tags.TagGameGenreRhythmDance},
	"Singing":           {tags.TagGameGenreRhythmKaraoke},
	"Karaoke":           {tags.TagGameGenreRhythmKaraoke},

	// Shoot'em ups. Space Invaders style fixed shooters are shmups too;
	// vehicle shooters that scroll are placed by their scroll direction.
	"Shoot'em Up":                   {tags.TagGameGenreShmup},
	"Shoot 'em Up":                  {tags.TagGameGenreShmup},
	"Shmup":                         {tags.TagGameGenreShmup},
	"Shoot'em Up / Vertical":        {tags.TagGameGenreShmupVertical},
	"Shoot'em Up / Horizontal":      {tags.TagGameGenreShmupHorizontal},
	"Shoot'em Up / Diagonal":        {tags.TagGameGenreShmupIsometric},
	"Shooter / Vertical":            {tags.TagGameGenreShmupVertical},
	"Shooter / Horizontal":          {tags.TagGameGenreShmupHorizontal},
	"Shooter / Vehicle, Vertical":   {tags.TagGameGenreShmupVertical},
	"Shooter / Vehicle, Horizontal": {tags.TagGameGenreShmupHorizontal},
	"Shooter / Vehicle, Diagonal":   {tags.TagGameGenreShmupIsometric},
	"Shooter / Space Invaders Like": {tags.TagGameGenreShmup},

	// Shooting
	"Shooter":                        {tags.TagGameGenreShooting},
	"Shooter Small":                  {tags.TagGameGenreShooting},
	"Shooter / Top view":             {tags.TagGameGenreShooting},
	"Shooter / Missile Command Like": {tags.TagGameGenreShooting},
	"Shooter / Plane":                {tags.TagGameGenreShooting},
	"Shooter / Plane, FPV":           {tags.TagGameGenreShooting},
	"Shooter / Plane, TPV":           {tags.TagGameGenreShooting},
	"Shooter / Vehicle, FPV":         {tags.TagGameGenreShooting},
	"Shooter / Vehicle, TPV":         {tags.TagGameGenreShooting},
	"Shooter / FPV":                  {tags.TagGameGenreShootingFPS},
	"Shooter / TPV":                  {tags.TagGameGenreShootingTPS},
	"First-Person Shooter":           {tags.TagGameGenreShootingFPS},
	"FPS":                            {tags.TagGameGenreShootingFPS},
	"Third-Person Shooter":           {tags.TagGameGenreShootingTPS},
	"Lightgun Shooter":               {tags.TagGameGenreShootingGallery},
	"Light Gun":                      {tags.TagGameGenreShootingGallery},
	"Lightgun":                       {tags.TagGameGenreShootingGallery},
	"Rail Shooter":                   {tags.TagGameGenreShootingRail},

	// Puzzle
	"Puzzle":            {tags.TagGameGenrePuzzle},
	"Puzzle-Game":       {tags.TagGameGenrePuzzle},
	"Puzzle / Equalize": {tags.TagGameGenrePuzzle},
	"Puzzle / Glide":    {tags.TagGameGenrePuzzle},
	"Puzzle / Throw":    {tags.TagGameGenrePuzzle},
	"Puzzle / Fall":     {tags.TagGameGenrePuzzleDrop},
	"Thinking":          {tags.TagGameGenrePuzzleMind},
	"Logic":             {tags.TagGameGenrePuzzle},

	// Simulation and strategy. The vocabulary files strategy under
	// simulation.
	"Simulation":                             {tags.TagGameGenreSim},
	"Simulation / Life":                      {tags.TagGameGenreSimLife},
	"Simulation / SciFi":                     {tags.TagGameGenreSim},
	"Simulation / Vehicle":                   {tags.TagGameGenreSim},
	"Build And Management":                   {tags.TagGameGenreSimBuilding},
	"Construction and Management Simulation": {tags.TagGameGenreSimBuilding},
	"Life Simulation":                        {tags.TagGameGenreSimLife},
	"Vehicle Simulation":                     {tags.TagGameGenreSim},
	"Flight Simulator":                       {tags.TagGameGenreSimFlight},
	"Hunting and Fishing":                    {tags.TagGameGenreSim},
	"Hunting":                                {tags.TagGameGenreSim},
	"Fishing":                                {tags.TagGameGenreSimFishing},
	"Horse racing":                           {tags.TagGameGenreSimDerby},
	"Strategy":                               {tags.TagGameGenreSimStrategy},

	// Sports
	"Sports":                       {tags.TagGameGenreSports},
	"Sport":                        {tags.TagGameGenreSports},
	"Sports with animals":          {tags.TagGameGenreSports},
	"Sports / Arm wrestling":       {tags.TagGameGenreSports},
	"Sports / Extreme":             {tags.TagGameGenreSports},
	"Sports / Fighting":            {tags.TagGameGenreSports},
	"Sports / Fitness":             {tags.TagGameGenreSports},
	"Sports / Football":            {tags.TagGameGenreSports},
	"Sports / Handball":            {tags.TagGameGenreSports},
	"Sports / Multisports":         {tags.TagGameGenreSports},
	"Sports / Shuffleboard":        {tags.TagGameGenreSports},
	"Sports / Skydiving":           {tags.TagGameGenreSports},
	"Sports / Water":               {tags.TagGameGenreSports},
	"Sports / Baseball":            {tags.TagGameGenreSportsBaseball},
	"Sports / Basketball":          {tags.TagGameGenreSportsBasketball},
	"Sports / Boxing":              {tags.TagGameGenreSportsBoxing},
	"Sports / Cycling":             {tags.TagGameGenreSportsCycling},
	"Sports / Dodgeball":           {tags.TagGameGenreSportsDodgeball},
	"Sports / Football (American)": {tags.TagGameGenreSportsFootball},
	"Sports / Football (Soccer)":   {tags.TagGameGenreSportsSoccer},
	"Sports / Golf":                {tags.TagGameGenreSportsGolf},
	"Sports / Hockey":              {tags.TagGameGenreSportsHockey},
	"Sports / Rugby":               {tags.TagGameGenreSportsRugby},
	"Sports / Running trails":      {tags.TagGameGenreSportsRunning},
	"Sports / Skateboard":          {tags.TagGameGenreSportsSkateboarding},
	"Sports / Skiing":              {tags.TagGameGenreSportsSkiing},
	"Sports / Sumo":                {tags.TagGameGenreSportsSumo},
	"Sports / Swimming":            {tags.TagGameGenreSportsSwimming},
	"Sports / Table tennis":        {tags.TagGameGenreSportsPingpong},
	"Sports / Tennis":              {tags.TagGameGenreSportsTennis},
	"Sports / Volleyball":          {tags.TagGameGenreSportsVolleyball},
	"Sports / Wrestling":           {tags.TagGameGenreSportsWrestling},

	// Not games
	"Educational":          {tags.TagGameGenreNotAGameEducational},
	"Education":            {tags.TagGameGenreNotAGameEducational},
	"Various / Utilities":  {tags.TagGameGenreNotAGameApplication},
	"Utility":              {tags.TagGameGenreNotAGameApplication},
	"Productivity":         {tags.TagGameGenreNotAGameApplication},
	"Various / Print Club": {tags.TagGameGenreNotAGamePurikura},

	// Dropped on purpose.
	"Compilation": nil, // a release format, not a genre
	"Various":     nil, // ScreenScraper's catch-all, says nothing
	"Adults":      nil, // a content rating, not a genre
	"Demo":        nil, // tech and scene demos; no "demo" value in the vocabulary
	"Casual Game": nil, // no casual value in the vocabulary
	"Family":      nil, // an audience, not a genre
	"Sandbox":     nil, // no sandbox value in the vocabulary
	"Stealth":     nil, // no stealth value in the vocabulary
	"Horror":      nil, // a theme; survival horror is listed separately
	"MMO":         nil, // a player mode, not a genre
}

// genresByKey indexes sourceGenres by lookup key.
var genresByKey = func() map[string][]tags.TagValue {
	byKey := make(map[string][]tags.TagValue, len(sourceGenres))
	for name, values := range sourceGenres {
		byKey[lookupKey(name)] = withParents(values)
	}
	return byKey
}()

// withParents adds each value's parent genre, when the vocabulary lists one,
// after the value.
func withParents(values []tags.TagValue) []tags.TagValue {
	out := make([]tags.TagValue, 0, len(values)*2)
	for _, v := range values {
		out = appendUnique(out, v)
		if parent, _, found := strings.Cut(string(v), ":"); found &&
			tags.IsCanonicalValue(tags.TagTypeGenre, tags.TagValue(parent)) {
			out = appendUnique(out, tags.TagValue(parent))
		}
	}
	return out
}

func appendUnique(values []tags.TagValue, v tags.TagValue) []tags.TagValue {
	for _, existing := range values {
		if existing == v {
			return values
		}
	}
	return append(values, v)
}

// lookupKey folds case, spacing and punctuation, so "Shoot'em Up",
// "shoot em up" and "SHOOT-EM-UP" meet the same key.
func lookupKey(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 0x7f {
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

// isSeparator reports the characters sources join genres and subgenres with.
func isSeparator(r rune) bool {
	switch r {
	case '/', ',', ';', '|', '-':
		return true
	default:
		return false
	}
}

// Lookup maps a genre field onto canonical genre values. The field may hold
// several names joined by "/", ",", ";", "|" or "-", and those characters
// also appear inside single names ("Racing, Driving", "Shoot'em Up /
// Vertical"), so the field is split at every separator and the pieces are
// regrouped into known names. Of the groupings, the one leaving the fewest
// pieces unknown wins, then the one with the fewest (so longest) names.
// unmapped holds the pieces no name covered, for the caller to report.
func Lookup(raw string) (values []tags.TagValue, unmapped []string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if found, ok := genresByKey[lookupKey(raw)]; ok {
		return append([]tags.TagValue(nil), found...), nil
	}
	var pieces, keys []string
	for _, piece := range strings.FieldsFunc(raw, isSeparator) {
		if key := lookupKey(piece); key != "" {
			pieces = append(pieces, strings.TrimSpace(piece))
			keys = append(keys, key)
		}
	}

	// best[i] is the cheapest grouping of the first i pieces; span[i] is how
	// many pieces its last group takes, zero for one unknown piece.
	type cost struct{ unknown, groups int }
	best := make([]cost, len(pieces)+1)
	span := make([]int, len(pieces)+1)
	for end := 1; end <= len(pieces); end++ {
		best[end] = cost{best[end-1].unknown + 1, best[end-1].groups + 1}
		span[end] = 0
		for n := 1; n <= min(maxJoinedSegments, end); n++ {
			if _, ok := genresByKey[strings.Join(keys[end-n:end], "")]; !ok {
				continue
			}
			c := cost{best[end-n].unknown, best[end-n].groups + 1}
			if c.unknown < best[end].unknown ||
				(c.unknown == best[end].unknown && c.groups < best[end].groups) {
				best[end] = c
				span[end] = n
			}
		}
	}

	var groups [][2]int
	for end := len(pieces); end > 0; {
		n := max(span[end], 1)
		groups = append(groups, [2]int{end - n, span[end]})
		end -= n
	}
	for i := len(groups) - 1; i >= 0; i-- {
		start, n := groups[i][0], groups[i][1]
		if n == 0 {
			unmapped = append(unmapped, pieces[start])
			continue
		}
		for _, v := range genresByKey[strings.Join(keys[start:start+n], "")] {
			values = appendUnique(values, v)
		}
	}
	return values, unmapped
}
