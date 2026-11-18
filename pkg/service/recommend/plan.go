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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// Section kinds say what a section of recommendations is built around.
const (
	SectionSeed      = "seed"
	SectionPlayLater = "playLater"
	SectionSeries    = "series"
	// SectionQueue is the one section of a queue plan: every pick, best
	// first, whatever it was picked for.
	SectionQueue = "queue"
)

const (
	// maxSeeds is how many games a plan is built from, and maxSeedsPerSeries
	// how many of them may come from one series, so a long-running series
	// cannot be every seed.
	maxSeeds          = 10
	maxSeedsPerSeries = 2
	// seedPool is how many of the most-liked games are read to choose the
	// seeds from.
	seedPool = 30
	// topSeedContributions is how many seeds a candidate's score adds up, so
	// one much-played game cannot supply the whole list.
	topSeedContributions = 3

	// seedCandidates is how many titles are scored for each seed, and
	// seedPostings bounds the tag index entries read to find them.
	seedCandidates = 60
	seedPostings   = 3000
	// seriesCandidates is how many titles of a series are read for its
	// section.
	seriesCandidates = 60

	// playedExcludeSecs is the play past which a game is no longer suggested:
	// the profile already knows it. A game played less than that is still
	// suggested, at brieflyPlayedFactor of its score.
	playedExcludeSecs   = 30 * 60
	brieflyPlayedFactor = 0.5
	// playLaterBoost lifts a game the profile already marked to play.
	playLaterBoost = 0.25
	// dislikedSeeds is how many disliked games are compared, and
	// dislikedNeighbourFactor what a game like one of them keeps of its
	// score.
	dislikedSeeds           = 5
	dislikedNeighbourFactor = 0.7

	// maxPerSeries and maxPerSystem keep one section from being one series
	// or one system. The system cap gives way when a section would otherwise
	// run short.
	maxPerSeries = 2
	maxPerSystem = 4

	minSeedSection      = 3
	minSeriesSection    = 2
	minPlayLaterSection = 1

	// planSpare is how many more items a section holds than were asked for,
	// to stand in for items that turn out to be hidden when they are read.
	planSpare = 4

	// DefaultMaxResults and DefaultMaxSections apply when a request names
	// neither.
	DefaultMaxResults  = 12
	DefaultMaxSections = 6
)

// Options shapes a plan.
type Options struct {
	// Systems limits picks to these systems. Nil allows every system.
	Systems     map[string]struct{}
	MaxResults  int
	MaxSections int
	// Queue lays the plan out as one ranked list instead of sections.
	Queue bool
}

// PlanItem is one recommended title and why it was picked. Reasons is empty
// for an item that is in a section for what it is, not for what it shares.
type PlanItem struct {
	// Signal says why a queue item was picked: the signal of the seed it is
	// like, or SignalPlayLater for a game the profile marked.
	Signal  string
	Reasons []Reason
	// SeedTitleDBID is the seed a queue item is most like.
	SeedTitleDBID int64
	TitleDBID     int64
}

// PlanSection is one group of recommendations.
type PlanSection struct {
	// Tag is the series a series section is about.
	Tag  *database.TagInfo
	Kind string
	// Signal is the sign of interest that made a seed section's seed.
	Signal string
	Items  []PlanItem
	// SeedTitleDBID is the title a seed section is built around.
	SeedTitleDBID int64
}

// MinItems is the fewest items the section is worth showing with.
func (s *PlanSection) MinItems() int {
	switch s.Kind {
	case SectionPlayLater:
		return minPlayLaterSection
	case SectionSeries:
		return minSeriesSection
	case SectionQueue:
		return 1
	default:
		return minSeedSection
	}
}

// Plan is a profile's recommendations before the media rows are read: which
// titles, in which sections, and why.
type Plan struct {
	Sections []PlanSection
}

// chosenSeed is a game the plan is built from.
type chosenSeed struct {
	seed     *seed
	game     *GameSignal
	signal   string
	affinity float64
}

// gameState is the profile's interest in one game, gathered from every
// version group the game spans.
type gameState struct {
	// playLater are the signals marked to play later, by title row.
	playLater []*GameSignal
	playSecs  int64
	disliked  bool
}

// candidate is a game being weighed for the plan.
type candidate struct {
	profile *titleProfile
	// bySeed is the most each seed says about the game. A seed can reach a
	// game through several of its versions, and only the best counts.
	bySeed map[int]float64
	// best is the pick from the seed that contributed most.
	best     Pick
	group    int64
	topSeed  int
	topScore float64
	total    float64
}

// BuildPlan gathers the profile's signals and turns them into sections. With
// no signals there is nothing to recommend from, and the plan is empty.
func BuildPlan(ctx context.Context, deps *Deps, profileID string, now time.Time, opts Options) (*Plan, error) {
	if opts.MaxResults <= 0 {
		opts.MaxResults = DefaultMaxResults
	}
	if opts.MaxSections <= 0 {
		opts.MaxSections = DefaultMaxSections
	}
	signals, err := GatherSignals(ctx, deps, profileID, now)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Sections: []PlanSection{}}
	if len(signals.ByGroup) == 0 {
		return plan, nil
	}
	builder := &planBuilder{deps: deps, signals: signals, opts: opts, now: now, ports: make(map[string]int64)}
	if err := builder.chooseSeeds(ctx); err != nil {
		return nil, err
	}
	if err := builder.gatherCandidates(ctx); err != nil {
		return nil, err
	}
	if opts.Queue {
		plan.Sections = builder.queue()
	} else {
		plan.Sections = builder.sections()
	}
	return plan, nil
}

type planBuilder struct {
	now        time.Time
	deps       *Deps
	signals    *Signals
	candidates map[int64]*candidate
	// seriesTitles are the titles of the series section's series, in the
	// order the index returned them.
	seriesTitles []int64
	profiles     map[int64]*titleProfile
	groups       map[int64]int64
	// ports maps a title slug to the game that claimed it, so the same game
	// on another system counts as that game. Seeds claim first, then titles
	// by row, so a game's identity never depends on the order it is met in.
	ports map[string]int64
	// games is what the signals say about each game, across its versions
	// and its ports.
	games     map[int64]*gameState
	seriesTag string
	seeds     []chosenSeed
	disliked  []*seed
	opts      Options
}

// chooseSeeds picks the games the plan is built from: the most-liked ones the
// index can describe, a few per series at most.
func (b *planBuilder) chooseSeeds(ctx context.Context) error {
	type ranked struct {
		game     *GameSignal
		signal   string
		affinity float64
	}
	var pool []ranked
	var hated []*GameSignal
	for _, game := range b.signals.ByGroup {
		if game.Disliked {
			hated = append(hated, game)
			continue
		}
		affinity, signal := game.Affinity(b.now)
		if affinity >= minSeedAffinity {
			pool = append(pool, ranked{game: game, signal: signal, affinity: affinity})
		}
	}
	slices.SortFunc(pool, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.affinity, a.affinity), cmp.Compare(a.game.TitleDBID, b.game.TitleDBID))
	})
	pool = pool[:min(len(pool), seedPool)]
	slices.SortFunc(hated, func(a, b *GameSignal) int { return cmp.Compare(a.TitleDBID, b.TitleDBID) })
	hated = hated[:min(len(hated), dislikedSeeds)]

	titleDBIDs := make([]int64, 0, len(pool)+len(hated))
	for i := range pool {
		titleDBIDs = append(titleDBIDs, pool[i].game.TitleDBID)
	}
	for _, game := range hated {
		titleDBIDs = append(titleDBIDs, game.TitleDBID)
	}
	prepared, err := prepareSeeds(ctx, b.deps.MediaDB, titleDBIDs)
	if err != nil {
		return err
	}

	perSeries := make(map[string]int)
	for i := range pool {
		if len(b.seeds) == maxSeeds {
			break
		}
		// A game the scrapers know nothing about has nothing to be like.
		ready, ok := prepared[pool[i].game.TitleDBID]
		if !ok || !ready.canGenerate() {
			continue
		}
		crowded := false
		for _, series := range ready.profile.series {
			if perSeries[series] >= maxSeedsPerSeries {
				crowded = true
			}
		}
		if crowded {
			continue
		}
		for _, series := range ready.profile.series {
			perSeries[series]++
		}
		b.seeds = append(b.seeds, chosenSeed{
			seed: ready, game: pool[i].game, signal: pool[i].signal, affinity: pool[i].affinity,
		})
	}
	for _, game := range hated {
		if ready, ok := prepared[game.TitleDBID]; ok && ready.canGenerate() {
			b.disliked = append(b.disliked, ready)
		}
	}
	b.chooseSeries()
	return nil
}

// chooseSeries picks the series the seeds lean on most, for its own section.
func (b *planBuilder) chooseSeries() {
	affinity := make(map[string]float64)
	for i := range b.seeds {
		for _, series := range b.seeds[i].seed.profile.series {
			affinity[series] += b.seeds[i].affinity
		}
	}
	best := 0.0
	for series, total := range affinity {
		if total > best || (total == best && series < b.seriesTag) {
			best, b.seriesTag = total, series
		}
	}
}

// seriesTagDBID returns the row of the chosen series tag.
func (b *planBuilder) seriesTagDBID() (int64, bool) {
	for i := range b.seeds {
		for _, tag := range b.seeds[i].seed.profile.tags {
			if tag.facet.code == ReasonSeries && tag.tag == b.seriesTag {
				return tag.tagDBID, true
			}
		}
	}
	return 0, false
}

// gatherCandidates finds the titles like each seed and adds up, per game,
// what the seeds say about it.
func (b *planBuilder) gatherCandidates(ctx context.Context) error {
	b.candidates = make(map[int64]*candidate)
	if len(b.seeds) == 0 && !b.hasPlayLater() {
		return nil
	}
	found := make([][]int64, len(b.seeds))
	var all []int64
	for i := range b.seeds {
		ids, err := b.findLike(ctx, b.seeds[i].seed, seedCandidates)
		if err != nil {
			return err
		}
		found[i] = ids
		all = append(all, ids...)
	}
	hatedFound := make([][]int64, len(b.disliked))
	for i, hated := range b.disliked {
		ids, err := b.findLike(ctx, hated, seedCandidates)
		if err != nil {
			return err
		}
		hatedFound[i] = ids
		all = append(all, ids...)
	}
	if tagDBID, ok := b.seriesTagDBID(); ok {
		rows, err := b.deps.MediaDB.FindTitlesSharingTags(ctx,
			[]database.WeightedTag{{TagDBID: tagDBID, Weight: 1}}, nil, systemList(b.opts.Systems), seriesCandidates)
		if err != nil {
			return fmt.Errorf("find series titles: %w", err)
		}
		for i := range rows {
			b.seriesTitles = append(b.seriesTitles, rows[i].TitleDBID)
		}
		all = append(all, b.seriesTitles...)
	}
	// A game marked, disliked or played is read too, so its ports on other
	// systems are known to be the same game.
	for _, game := range b.signals.ByGroup {
		if game.PlayLater || game.Disliked || game.PlaySecs > 0 {
			all = append(all, game.TitleDBID)
		}
	}
	slices.Sort(all)
	all = slices.Compact(all)

	var err error
	b.profiles, b.groups, err = loadCandidates(ctx, b.deps.MediaDB, all)
	if err != nil {
		return err
	}
	b.claimPorts(all)
	b.gatherGames()

	hatedGroups := make(map[int64]struct{})
	for i, hated := range b.disliked {
		for _, id := range hatedFound[i] {
			if profile, ok := b.profiles[id]; ok {
				if _, alike := hated.score(profile); alike {
					hatedGroups[b.gameOf(id)] = struct{}{}
				}
			}
		}
	}
	for i := range b.seeds {
		for _, id := range found[i] {
			profile, ok := b.profiles[id]
			if !ok || !b.allowedSystem(profile.systemID) {
				continue
			}
			pick, alike := b.seeds[i].seed.score(profile)
			if !alike {
				continue
			}
			group := b.gameOf(id)
			if !b.suggestable(group) {
				continue
			}
			weighed := pick.Score * b.seeds[i].affinity
			entry, held := b.candidates[group]
			if !held {
				entry = &candidate{group: group, topSeed: -1, bySeed: make(map[int]float64)}
				b.candidates[group] = entry
			}
			entry.bySeed[i] = max(entry.bySeed[i], weighed)
			if entry.topSeed < 0 || weighed > entry.topScore ||
				(weighed == entry.topScore && comparePicks(&pick, &entry.best) < 0) {
				entry.topSeed, entry.topScore, entry.best, entry.profile = i, weighed, pick, profile
			}
		}
	}
	for group, entry := range b.candidates {
		scores := make([]float64, 0, len(entry.bySeed))
		for _, score := range entry.bySeed {
			scores = append(scores, score)
		}
		slices.SortFunc(scores, func(a, b float64) int { return cmp.Compare(b, a) })
		for _, score := range scores[:min(len(scores), topSeedContributions)] {
			entry.total += score
		}
		if game, known := b.games[group]; known {
			if game.playSecs > 0 {
				entry.total *= brieflyPlayedFactor
			}
			if len(game.playLater) > 0 {
				entry.total += playLaterBoost
			}
		}
		if _, near := hatedGroups[group]; near {
			entry.total *= dislikedNeighbourFactor
		}
	}
	return nil
}

func (b *planBuilder) hasPlayLater() bool {
	for _, game := range b.signals.ByGroup {
		if game.PlayLater {
			return true
		}
	}
	return false
}

// findLike returns the titles sharing tags with the seed, leaving out the
// seed's own versions.
func (b *planBuilder) findLike(ctx context.Context, from *seed, limit int) ([]int64, error) {
	exclude, err := groupTitles(ctx, b.deps.MediaDB, from.profile.titleDBID)
	if err != nil {
		return nil, err
	}
	rows, err := b.deps.MediaDB.FindTitlesSharingTags(
		ctx, from.candidateTags(seedPostings), exclude, systemList(b.opts.Systems), limit)
	if err != nil {
		return nil, fmt.Errorf("find titles like seed: %w", err)
	}
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].TitleDBID
	}
	return ids, nil
}

func (b *planBuilder) allowedSystem(systemID string) bool {
	if b.opts.Systems == nil {
		return true
	}
	_, ok := b.opts.Systems[systemID]
	return ok
}

// claimPorts settles which game holds each slug: a seed's own first, so its
// ports are the seed and not picks, then the lowest title row's.
func (b *planBuilder) claimPorts(titleDBIDs []int64) {
	for i := range b.seeds {
		if slug := b.seeds[i].seed.profile.slug; slug != "" {
			if _, claimed := b.ports[slug]; !claimed {
				b.ports[slug] = b.seeds[i].game.Group
			}
		}
	}
	for _, id := range titleDBIDs {
		profile, ok := b.profiles[id]
		if !ok || profile.slug == "" {
			continue
		}
		if _, claimed := b.ports[profile.slug]; !claimed {
			b.ports[profile.slug] = versionGroupOf(b.groups, id)
		}
	}
}

// gatherGames rolls the signals up per game, so a dislike, a play-later mark
// or play time on one port counts for every port of the game.
func (b *planBuilder) gatherGames() {
	b.games = make(map[int64]*gameState, len(b.signals.ByGroup))
	for _, signal := range b.signals.ByGroup {
		id := b.gameOf(signal.TitleDBID)
		game, held := b.games[id]
		if !held {
			game = &gameState{}
			b.games[id] = game
		}
		game.playSecs += signal.PlaySecs
		game.disliked = game.disliked || signal.Disliked
		if signal.PlayLater {
			game.playLater = append(game.playLater, signal)
		}
	}
	for _, game := range b.games {
		slices.SortFunc(game.playLater, func(a, b *GameSignal) int { return cmp.Compare(a.TitleDBID, b.TitleDBID) })
	}
}

// gameOf returns the game a title belongs to: its version group, or the game
// holding its slug when the library has it on more than one system.
func (b *planBuilder) gameOf(titleDBID int64) int64 {
	if profile, ok := b.profiles[titleDBID]; ok && profile.slug != "" {
		if owner, claimed := b.ports[profile.slug]; claimed {
			return owner
		}
	}
	return versionGroupOf(b.groups, titleDBID)
}

// suggestable reports whether a game may be recommended: not one the plan is
// built from, not disliked, and not already played at length.
func (b *planBuilder) suggestable(group int64) bool {
	if b.isSeed(group) {
		return false
	}
	game, known := b.games[group]
	if !known {
		return true
	}
	return !game.disliked && game.playSecs < playedExcludeSecs
}

// isSeed reports whether the plan is built from the game.
func (b *planBuilder) isSeed(group int64) bool {
	for i := range b.seeds {
		if b.seeds[i].game.Group == group {
			return true
		}
	}
	return false
}

// sections lays the candidates out: the strongest seed first, then what the
// profile marked to play, then the series it leans on, then the other seeds.
// A game appears once, in the first section that takes it, and a seed in
// none: it is a game the profile already plays or loves, and it heads its own
// section.
func (b *planBuilder) sections() []PlanSection {
	used := make(map[int64]struct{}, len(b.seeds))
	for i := range b.seeds {
		used[b.seeds[i].game.Group] = struct{}{}
	}
	limit := b.opts.MaxResults + planSpare
	sections := make([]PlanSection, 0, len(b.seeds)+2)
	keep := func(section PlanSection) {
		if len(section.Items) >= section.MinItems() {
			sections = append(sections, section)
			for i := range section.Items {
				used[b.gameOf(section.Items[i].TitleDBID)] = struct{}{}
			}
		}
	}

	bySeed := make([][]*candidate, len(b.seeds))
	for _, entry := range b.candidates {
		bySeed[entry.topSeed] = append(bySeed[entry.topSeed], entry)
	}
	seedSection := func(index int) PlanSection {
		entries := bySeed[index]
		slices.SortFunc(entries, compareCandidates)
		section := PlanSection{
			Kind:          SectionSeed,
			Signal:        b.seeds[index].signal,
			SeedTitleDBID: b.seeds[index].game.TitleDBID,
		}
		for _, entry := range capCandidates(entries, used, limit) {
			section.Items = append(section.Items,
				PlanItem{TitleDBID: entry.best.TitleDBID, Reasons: entry.best.Reasons})
		}
		return section
	}

	if len(b.seeds) > 0 {
		keep(seedSection(0))
	}
	keep(b.playLaterSection(used, limit))
	keep(b.seriesSection(used, limit))
	for index := 1; index < len(b.seeds); index++ {
		keep(seedSection(index))
	}
	return sections[:min(len(sections), b.opts.MaxSections)]
}

// queue lists every pick in one list. The seeds take turns, a stronger seed
// more often, each giving its next match, so a seed whose matches score high
// cannot fill the list; every fourth place goes to a game marked to play
// later. Only a seed's close matches take turns, in an order that moves on
// each day so the list does not stay the same while the signals do; the rest
// fill the list once the turns run out. The series and system caps hold
// across the whole list.
func (b *planBuilder) queue() []PlanSection {
	used := make(map[int64]struct{}, len(b.seeds))
	for i := range b.seeds {
		used[b.seeds[i].game.Group] = struct{}{}
	}
	lanes := make([][]*candidate, len(b.seeds))
	weights := make([]float64, len(b.seeds))
	for i := range b.seeds {
		weights[i] = b.seeds[i].affinity
	}
	var later []*candidate
	for group, entry := range b.candidates {
		if state, ok := b.games[group]; ok && len(state.playLater) > 0 {
			later = append(later, entry)
			continue
		}
		lanes[entry.topSeed] = append(lanes[entry.topSeed], entry)
	}
	for group, state := range b.games {
		if _, scored := b.candidates[group]; scored || len(state.playLater) == 0 || !b.suggestable(group) {
			continue
		}
		game := b.playLaterTitle(state)
		if game == nil {
			continue
		}
		entry := &candidate{group: group, topSeed: -1, total: playLaterBoost, best: Pick{TitleDBID: game.TitleDBID}}
		if profile, ok := b.profiles[game.TitleDBID]; ok {
			entry.profile, entry.best.SystemID, entry.best.Rating = profile, profile.systemID, profile.rating
		}
		later = append(later, entry)
	}

	day := int(b.now.UTC().Unix() / secondsPerDay)
	var reserve []*candidate
	for i := range lanes {
		var weak []*candidate
		lanes[i], weak = queueLane(lanes[i], day)
		reserve = append(reserve, weak...)
	}
	slices.SortFunc(reserve, compareCandidates)
	slices.SortFunc(later, compareCandidates)
	later = rotateCandidates(later, day)

	section := PlanSection{Kind: SectionQueue}
	for _, entry := range interleaveCandidates(lanes, weights, reserve, later, used, b.opts.MaxResults+planSpare) {
		item := PlanItem{TitleDBID: entry.best.TitleDBID, Reasons: entry.best.Reasons}
		if state, ok := b.games[entry.group]; ok && len(state.playLater) > 0 {
			item.Signal = SignalPlayLater
		} else if entry.topSeed >= 0 {
			item.Signal = b.seeds[entry.topSeed].signal
			item.SeedTitleDBID = b.seeds[entry.topSeed].game.TitleDBID
		}
		section.Items = append(section.Items, item)
	}
	if len(section.Items) == 0 {
		return []PlanSection{}
	}
	return []PlanSection{section}
}

const (
	// queueLaterEvery is how often the queue gives a place to a game marked
	// to play later: every fourth.
	queueLaterEvery = 4
	// queueCloseMatch is how close to a seed's best match a game must score
	// to take the seed's turns.
	queueCloseMatch = 0.5
	secondsPerDay   = 24 * 60 * 60
)

// queueLane splits one seed's matches into the close ones, which take the
// seed's turns starting at a place that moves on each day, and the rest.
func queueLane(entries []*candidate, day int) (closeMatches, rest []*candidate) {
	if len(entries) == 0 {
		return nil, nil
	}
	slices.SortFunc(entries, compareCandidates)
	cut := len(entries)
	for i, entry := range entries {
		if entry.total < entries[0].total*queueCloseMatch {
			cut = i
			break
		}
	}
	return rotateCandidates(entries[:cut], day), entries[cut:]
}

// rotateCandidates starts the list at a place that moves on by one each day,
// wrapping round, so each entry leads in turn.
func rotateCandidates(entries []*candidate, day int) []*candidate {
	if len(entries) < 2 {
		return entries
	}
	start := day % len(entries)
	if start < 0 {
		start += len(entries)
	}
	return append(slices.Clone(entries[start:]), entries[:start]...)
}

// interleaveCandidates fills a list from the lanes by smooth weighted turns:
// a lane with twice the weight takes twice the places, spread out, not in a
// run. Every queueLaterEvery-th place goes to the later lane while it lasts.
// A game already used or in a full series is skipped; one on a full system is
// passed over. When the lanes run dry, the reserve fills the list, then what
// was passed over.
func interleaveCandidates(
	lanes [][]*candidate, weights []float64, reserve, later []*candidate,
	used map[int64]struct{}, limit int,
) []*candidate {
	perSeries := make(map[string]int)
	perSystem := make(map[string]int)
	taken := make([]*candidate, 0, limit)
	var passedOver []*candidate
	seriesFull := func(entry *candidate) bool {
		for _, series := range seriesOf(entry) {
			if perSeries[series] >= maxPerSeries {
				return true
			}
		}
		return false
	}
	take := func(entry *candidate) {
		for _, series := range seriesOf(entry) {
			perSeries[series]++
		}
		perSystem[entry.best.SystemID]++
		used[entry.group] = struct{}{}
		taken = append(taken, entry)
	}
	// next takes the first game a lane can give, dropping what it skips.
	next := func(lane *[]*candidate) bool {
		for len(*lane) > 0 {
			entry := (*lane)[0]
			*lane = (*lane)[1:]
			if _, done := used[entry.group]; done || seriesFull(entry) {
				continue
			}
			if perSystem[entry.best.SystemID] >= maxPerSystem {
				passedOver = append(passedOver, entry)
				continue
			}
			take(entry)
			return true
		}
		return false
	}
	current := make([]float64, len(lanes))
	turn := func() bool {
		for {
			best, total := -1, 0.0
			for i := range lanes {
				if len(lanes[i]) == 0 {
					continue
				}
				current[i] += weights[i]
				total += weights[i]
				if best < 0 || current[i] > current[best] {
					best = i
				}
			}
			if best < 0 {
				return false
			}
			current[best] -= total
			if next(&lanes[best]) {
				return true
			}
		}
	}

	for len(taken) < limit {
		if (len(taken)+1)%queueLaterEvery == 0 && next(&later) {
			continue
		}
		if !turn() && !next(&reserve) && !next(&later) {
			break
		}
	}
	for _, entry := range passedOver {
		if len(taken) == limit {
			break
		}
		if _, done := used[entry.group]; done || seriesFull(entry) {
			continue
		}
		take(entry)
	}
	return taken
}

// playLaterTitle is the version of a marked game a list shows: the first
// marked one on a system the request may show.
func (b *planBuilder) playLaterTitle(state *gameState) *GameSignal {
	for _, signal := range state.playLater {
		if profile, ok := b.profiles[signal.TitleDBID]; !ok || b.allowedSystem(profile.systemID) {
			return signal
		}
	}
	return nil
}

// seriesOf is the series a candidate belongs to. A marked game the index
// could not describe belongs to none.
func seriesOf(entry *candidate) []string {
	if entry.profile == nil {
		return nil
	}
	return entry.profile.series
}

// compareCandidates orders candidates best first, deterministically.
func compareCandidates(a, b *candidate) int {
	return cmp.Or(
		cmp.Compare(b.total, a.total),
		cmp.Compare(b.best.Rating, a.best.Rating),
		cmp.Compare(a.best.TitleDBID, b.best.TitleDBID),
	)
}

// capCandidates takes candidates in order, skipping games already used and
// holding each series and system to its cap. When that leaves the section
// short, games passed over for their system fill it.
func capCandidates(entries []*candidate, used map[int64]struct{}, limit int) []*candidate {
	perSeries := make(map[string]int)
	perSystem := make(map[string]int)
	taken := make([]*candidate, 0, min(len(entries), limit))
	var passedOver []*candidate
	for _, entry := range entries {
		if len(taken) == limit {
			break
		}
		if _, done := used[entry.group]; done {
			continue
		}
		crowded := false
		for _, series := range seriesOf(entry) {
			if perSeries[series] >= maxPerSeries {
				crowded = true
			}
		}
		if crowded {
			continue
		}
		if perSystem[entry.best.SystemID] >= maxPerSystem {
			passedOver = append(passedOver, entry)
			continue
		}
		for _, series := range seriesOf(entry) {
			perSeries[series]++
		}
		perSystem[entry.best.SystemID]++
		taken = append(taken, entry)
	}
	for _, entry := range passedOver {
		if len(taken) == limit {
			break
		}
		crowded := false
		for _, series := range seriesOf(entry) {
			if perSeries[series] >= maxPerSeries {
				crowded = true
			}
		}
		if crowded {
			continue
		}
		for _, series := range seriesOf(entry) {
			perSeries[series]++
		}
		taken = append(taken, entry)
	}
	slices.SortFunc(taken, compareCandidates)
	return taken
}

// playLaterSection lists what the profile marked to play, the ones most like
// its seeds first.
func (b *planBuilder) playLaterSection(used map[int64]struct{}, limit int) PlanSection {
	section := PlanSection{Kind: SectionPlayLater}
	type marked struct {
		game    *GameSignal
		reasons []Reason
		total   float64
	}
	var list []marked
	for group, state := range b.games {
		// A game played at length is not suggested, marked or not.
		if len(state.playLater) == 0 || state.disliked || state.playSecs >= playedExcludeSecs {
			continue
		}
		if _, done := used[group]; done {
			continue
		}
		// The game is listed once, as the first marked version the request
		// may show.
		game := b.playLaterTitle(state)
		if game == nil {
			continue
		}
		entry := marked{game: game}
		if scored, ok := b.candidates[group]; ok {
			entry.total, entry.reasons = scored.total, scored.best.Reasons
		}
		list = append(list, entry)
	}
	slices.SortFunc(list, func(a, b marked) int {
		return cmp.Or(cmp.Compare(b.total, a.total), cmp.Compare(a.game.TitleDBID, b.game.TitleDBID))
	})
	for _, entry := range list[:min(len(list), limit)] {
		section.Items = append(section.Items, PlanItem{TitleDBID: entry.game.TitleDBID, Reasons: entry.reasons})
	}
	return section
}

// seriesSection lists more of the series the seeds lean on most.
func (b *planBuilder) seriesSection(used map[int64]struct{}, limit int) PlanSection {
	tag := database.TagInfo{Type: string(tags.TagTypeSearch), Tag: b.seriesTag}
	section := PlanSection{Kind: SectionSeries, Tag: &tag}
	if b.seriesTag == "" {
		return section
	}
	type member struct {
		total     float64
		titleDBID int64
	}
	taken := make(map[int64]struct{})
	var members []member
	for _, titleDBID := range b.seriesTitles {
		profile, ok := b.profiles[titleDBID]
		if !ok || !b.allowedSystem(profile.systemID) {
			continue
		}
		group := b.gameOf(titleDBID)
		if _, done := used[group]; done || !b.suggestable(group) {
			continue
		}
		if _, dup := taken[group]; dup {
			continue
		}
		taken[group] = struct{}{}
		entry := member{titleDBID: titleDBID}
		if scored, held := b.candidates[group]; held {
			entry.total, entry.titleDBID = scored.total, scored.best.TitleDBID
		}
		members = append(members, entry)
	}
	slices.SortFunc(members, func(a, b member) int {
		return cmp.Or(cmp.Compare(b.total, a.total), cmp.Compare(a.titleDBID, b.titleDBID))
	})
	for _, entry := range members[:min(len(members), limit)] {
		section.Items = append(section.Items, PlanItem{
			TitleDBID: entry.titleDBID,
			Reasons:   []Reason{{Code: ReasonSeries, Tags: []database.TagInfo{tag}}},
		})
	}
	return section
}
