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

package simfixtures

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// fileKind says how a planned file is written.
type fileKind int

const (
	kindEmpty fileKind = iota // zero-byte placeholder
	kindZip                   // zip archive holding one empty ROM named inner
	kindText                  // content written as is (playlists, markers)
)

// plannedFile is one file Generate writes under a system folder.
type plannedFile struct {
	// rel is the path under the system folder, with forward slashes.
	rel     string
	inner   string
	content string
	// game is the index of the title the file belongs to, -1 for none.
	game int
	kind fileKind
	// media marks a file Core is expected to index.
	media bool
	// primary marks the file that stands for its title in artwork.
	primary bool
	// hidden marks a disc an .m3u playlist stands for; the gamelist hides it.
	hidden bool
}

// systemPlan is everything one system folder gets.
type systemPlan struct {
	ext    string
	titles []title
	files  []plannedFile
}

// planner builds a systemPlan.
type planner struct {
	opts  *Options
	rng   *rand.Rand
	used  map[string]bool
	plan  *systemPlan
	exts  []string
	disc  bool
	m3u   bool
	zipOK bool
}

// subfolders are where Subfolders puts titles; nested ones exercise deeper
// walks and mirrored media paths.
var subfolders = []string{ //nolint:gochecknoglobals // Static table.
	"Favorites", "Collections/Best Of", "Imports", "RPG", "Multiplayer/Party Games",
}

func (p *planner) add(f plannedFile) {
	key := strings.ToLower(f.rel)
	if p.used[key] {
		return
	}
	p.used[key] = true
	p.plan.files = append(p.plan.files, f)
}

// romFile adds one ROM, as a zip when the system only takes archives or, with
// Zips, for some titles.
func (p *planner) romFile(dir, stem string, game int, primary bool) {
	f := plannedFile{rel: dir + stem + p.plan.ext, game: game, media: true, primary: primary}
	switch {
	case isArchiveExt(p.plan.ext):
		f.rel = dir + stem + ".zip"
		f.kind, f.inner = kindZip, stem+".bin"
	case p.opts.Zips && p.zipOK && p.rng.IntN(10) < 3:
		f.inner = stem + p.plan.ext
		f.rel = dir + stem + ".zip"
		f.kind = kindZip
	}
	p.add(f)
}

// planTitle adds every file for one title.
func (p *planner) planTitle(game int) {
	t := &p.plan.titles[game]
	dir := ""
	if p.opts.Subfolders && !t.set && p.rng.IntN(4) == 0 {
		dir = subfolders[p.rng.IntN(len(subfolders))] + "/"
	}
	switch {
	case t.set:
		p.romFile(dir, t.stem, game, true)
		if p.opts.Realistic {
			for i, clone := range t.clones {
				if i >= 2 || p.rng.IntN(3) == 0 {
					break
				}
				p.romFile(dir, clone, game, false)
			}
		}
	case !p.opts.Realistic || t.decorated:
		p.discsOrROM(dir, t.stem, game, true)
	default:
		p.realisticTitle(dir, t, game)
	}
}

// discsOrROM adds a title as a multi-disc set on a disc system (sometimes,
// with Realistic) or as one ROM.
func (p *planner) discsOrROM(dir, stem string, game int, primary bool) {
	if !p.opts.Realistic || !p.disc || p.rng.IntN(6) != 0 {
		p.romFile(dir, stem, game, primary)
		return
	}
	discs := 2 + p.rng.IntN(3)
	var playlist strings.Builder
	for n := 1; n <= discs; n++ {
		name := stem + " (Disc " + strconv.Itoa(n) + ")" + p.plan.ext
		p.add(plannedFile{
			rel: dir + name, game: game, media: true,
			primary: primary && !p.m3u && n == 1, hidden: p.m3u,
		})
		playlist.WriteString(name + "\n")
	}
	if p.m3u {
		p.add(plannedFile{
			rel: dir + stem + ".m3u", content: playlist.String(), kind: kindText,
			game: game, media: true, primary: primary,
		})
	}
}

// realisticTitle adds a title with dump-set decoration and, sometimes, the
// other releases a full set holds: another region, a hack, a translation, a
// prototype.
func (p *planner) realisticTitle(dir string, t *title, game int) {
	region := t.region
	if region == "" {
		region = synthRegions[p.rng.IntN(len(synthRegions))]
	}
	main := t.stem + " (" + region + ")"
	if strings.Contains(region, "Europe") && p.rng.IntN(2) == 0 {
		main += " (En,Fr,De)"
	}
	if p.rng.IntN(8) == 0 {
		main += " (Rev " + strconv.Itoa(1+p.rng.IntN(2)) + ")"
	}
	if !p.disc && p.rng.IntN(10) == 0 {
		main += " [!]"
	}
	p.discsOrROM(dir, main, game, true)

	if p.rng.IntN(5) == 0 {
		other := "Japan"
		if region == "Japan" {
			other = "USA"
		}
		p.discsOrROM(dir, t.stem+" ("+other+")", game, false)
	}
	if p.disc {
		return
	}
	switch p.rng.IntN(16) {
	case 0:
		p.romFile(dir, t.stem+" ("+region+") [h1]", game, false)
	case 1:
		if region == "Japan" {
			p.romFile(dir, t.stem+" (Japan) [T+Eng1.0]", game, false)
		}
	case 2:
		p.romFile(dir, t.stem+" ("+region+") (Proto)", game, false)
	case 3:
		p.romFile(dir, t.stem+" ("+region+") (Beta)", game, false)
	}
}

// Edge-case names. Indexed ones must reach the index; skipped ones must not.
const (
	edgeCJK       = "ファミコン探偵倶楽部 (Japan)" //nolint:gosmopolitan // Non-Latin file name on purpose.
	edgeKorean    = "슈퍼 게임 모험 (Korea)"
	edgeAccented  = "Pokémon – Édition Spéciale (France)"
	edgeBrackets  = "Tom & Jerry - Cats [b1] (USA) {Promo}"
	edgePunct     = "Rock 'n' Roll #1 - 100% Fun! (USA)"
	edgeDotted    = "Sonic 3.0 (USA)"
	edgeUpperExt  = "Upper Case Extension (USA)"
	edgeLongWord  = "Extremely Long Title "
	edgeLongBytes = 200
)

// planEdgeCases adds the EdgeCases files.
func (p *planner) planEdgeCases() {
	for _, stem := range []string{edgeCJK, edgeKorean, edgeAccented, edgeBrackets, edgePunct, edgeDotted} {
		p.romFile("", stem, -1, false)
	}
	long := strings.Repeat(edgeLongWord, edgeLongBytes/len(edgeLongWord)) + "(USA)"
	p.romFile("", long, -1, false)
	if !isArchiveExt(p.plan.ext) {
		p.add(plannedFile{rel: edgeUpperExt + strings.ToUpper(p.plan.ext), game: -1, media: true})
	}

	ext := p.plan.ext
	if isArchiveExt(ext) {
		ext = ".zip"
	}
	skipped := []string{
		".hidden/Hidden Game (USA)" + ext,
		"Ignored/Ignored Game (USA)" + ext,
		"__MACOSX/Mac Resource (USA)" + ext,
		"._Apple Double (USA)" + ext,
		"Wrong Extension (USA).notarom",
		"Backup Copy (USA)" + ext + ".bak",
	}
	for _, rel := range skipped {
		p.add(plannedFile{rel: rel, game: -1})
	}
	p.add(plannedFile{rel: "Ignored/.zaparooignore", game: -1, kind: kindText})
}

// planSystem picks a system's titles and lays out every file it gets,
// returning the plan and the source that supplied the titles. The plan
// depends only on opts, the folder and the DATs, never on other systems.
func planSystem(opts *Options, t target, dats datIndex) (*systemPlan, string, error) {
	rng := systemRand(opts.Seed, t.folder)
	titles, source, err := pickTitles(opts, t.info.SystemID, dats, rng)
	if err != nil {
		return nil, "", err
	}
	exts := t.info.Extensions
	plan := &systemPlan{ext: romExtension(exts), titles: titles}
	p := &planner{
		opts: opts, rng: rng, used: make(map[string]bool), plan: plan, exts: exts,
		disc: isDiscSystem(exts), m3u: hasExt(exts, ".m3u"), zipOK: hasExt(exts, ".zip"),
	}
	for i := range titles {
		p.planTitle(i)
	}
	if opts.EdgeCases {
		p.planEdgeCases()
	}
	return plan, source, nil
}
