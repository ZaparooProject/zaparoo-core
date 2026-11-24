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
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
)

// generateSystem plans and writes one system folder.
func generateSystem(ctx context.Context, opts *Options, t target, dats datIndex) (*SystemReport, error) {
	plan, source, err := planSystem(opts, t, dats)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(opts.Root, t.folder)
	report := &SystemReport{
		Folder: t.folder, SystemID: t.info.SystemID, Source: source,
		Extension: plan.ext, Titles: len(plan.titles),
	}
	for i := range plan.files {
		if i%256 == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("write files: %w", ctxErr)
			}
		}
		f := &plan.files[i]
		if writeErr := writePlanned(dir, f); writeErr != nil {
			return nil, err
		}
		report.Files++
		switch {
		case f.media:
			report.Media++
			report.MediaPaths = append(report.MediaPaths, filepath.Join(dir, filepath.FromSlash(f.rel)))
			if len(report.Samples) < maxSamples {
				report.Samples = append(report.Samples, f.rel)
			}
		case path.Base(f.rel) != ".zaparooignore":
			report.Skipped++
		}
	}
	slices.Sort(report.MediaPaths)

	var images map[int]map[string]string
	if opts.MediaImages {
		images, report.Images, err = writeImages(ctx, dir, plan, systemRand(opts.Seed, t.folder+"/media"))
		if err != nil {
			return nil, err
		}
		report.Files += report.Images
	}
	if opts.Gamelist {
		if err := writeGamelist(dir, plan, images, systemRand(opts.Seed, t.folder+"/gamelist")); err != nil {
			return nil, err
		}
		report.Gamelist = true
		report.Files++
	}
	return report, nil
}

func writePlanned(dir string, f *plannedFile) error {
	full := filepath.Join(dir, filepath.FromSlash(f.rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("create folder for %s: %w", f.rel, err)
	}
	var data []byte
	switch f.kind {
	case kindZip:
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if _, err := zw.Create(f.inner); err != nil {
			return fmt.Errorf("zip %s: %w", f.rel, err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("zip %s: %w", f.rel, err)
		}
		data = buf.Bytes()
	case kindText:
		data = []byte(f.content)
	case kindEmpty:
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", f.rel, err)
	}
	return nil
}

// artKind is one media folder Generate fills, with the share of titles that
// get art there and the image size.
type artKind struct {
	dir     string
	gamelst string // the gamelist.xml element that points at the file
	percent int
	w, h    int
}

// artKinds use ES-DE's media folder names, which the media-folder scraper
// reads (esmedia.ArtworkDirCandidates).
var artKinds = []artKind{ //nolint:gochecknoglobals // Static table.
	{dir: "covers", gamelst: "thumbnail", percent: 100, w: 24, h: 32},
	{dir: "screenshots", gamelst: "image", percent: 80, w: 32, h: 24},
	{dir: "marquees", gamelst: "marquee", percent: 50, w: 40, h: 12},
	{dir: "miximages", gamelst: "", percent: 40, w: 32, h: 32},
	{dir: "titlescreens", gamelst: "titlescreen", percent: 30, w: 32, h: 24},
	{dir: "3dboxes", gamelst: "boxart3d", percent: 20, w: 24, h: 32},
	{dir: "fanart", gamelst: "fanart", percent: 10, w: 48, h: 27},
}

// writeImages writes artwork for each title's primary file and returns, per
// title, the gamelist path of each image by media folder name.
func writeImages(
	ctx context.Context, dir string, plan *systemPlan, rng *rand.Rand,
) (byGame map[int]map[string]string, written int, err error) {
	byGame = make(map[int]map[string]string)
	for i := range plan.files {
		f := &plan.files[i]
		if !f.primary || f.game < 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("write images: %w", err)
		}
		stem := strings.TrimSuffix(f.rel, path.Ext(f.rel))
		for _, kind := range artKinds {
			if rng.IntN(100) >= kind.percent {
				continue
			}
			rel := "media/" + kind.dir + "/" + stem + ".png"
			data, err := placeholderPNG(rel, kind.w, kind.h)
			if err != nil {
				return nil, 0, err
			}
			full := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
				return nil, 0, fmt.Errorf("create media folder: %w", err)
			}
			if err := os.WriteFile(full, data, 0o600); err != nil {
				return nil, 0, fmt.Errorf("write %s: %w", rel, err)
			}
			if byGame[f.game] == nil {
				byGame[f.game] = make(map[string]string)
			}
			byGame[f.game][kind.dir] = "./" + rel
			written++
		}
	}
	return byGame, written, nil
}

// placeholderPNG draws a small two-tone image whose colours come from key,
// so each file differs but the output is deterministic.
func placeholderPNG(key string, w, h int) ([]byte, error) {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(key))
	v := sum.Sum32()
	//nolint:gosec // Truncation intended.
	bg := color.NRGBA{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 0xff}
	fg := color.NRGBA{R: ^bg.R, G: ^bg.G, B: ^bg.B, A: 0xff}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if (x/4+y/4)%2 == 0 {
				img.SetNRGBA(x, y, bg)
			} else {
				img.SetNRGBA(x, y, fg)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode placeholder image: %w", err)
	}
	return buf.Bytes(), nil
}

// writeGamelist writes an ES-DE gamelist.xml for the system's media files.
func writeGamelist(dir string, plan *systemPlan, images map[int]map[string]string, rng *rand.Rand) error {
	list := esapi.GameList{}
	for i := range plan.files {
		f := &plan.files[i]
		if !f.media || f.game < 0 {
			continue
		}
		t := &plan.titles[f.game]
		g := esapi.Game{
			Path:        "./" + f.rel,
			Name:        t.name,
			Desc:        "Placeholder description for " + t.name + ", written by the Zaparoo simulator.",
			Developer:   orPick(t.developer, synthCompanies, rng),
			Publisher:   orPick(t.publisher, synthCompanies, rng),
			Genre:       orPick(t.genre, synthGenres, rng),
			Players:     orPick(t.players, []string{"1", "1-2", "1-4"}, rng),
			ReleaseDate: esDate(t.date, rng),
			Rating:      strconv.FormatFloat(float64(rng.IntN(11))/10, 'f', 1, 64),
			Hidden:      f.hidden,
		}
		if f.primary {
			art := images[f.game]
			for _, kind := range artKinds {
				if p, ok := art[kind.dir]; ok {
					setArt(&g, kind.gamelst, p)
				}
			}
		}
		list.Games = append(list.Games, g)
	}
	data, err := xml.MarshalIndent(&list, "", "\t")
	if err != nil {
		return fmt.Errorf("encode gamelist: %w", err)
	}
	data = append([]byte(xml.Header), data...)
	data = append(data, '\n')
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create system folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gamelist.xml"), data, 0o600); err != nil {
		return fmt.Errorf("write gamelist: %w", err)
	}
	return nil
}

func setArt(g *esapi.Game, element, p string) {
	switch element {
	case "thumbnail":
		g.Thumbnail = p
	case "image":
		g.Image = p
	case "marquee":
		g.Marquee = p
	case "titlescreen":
		g.TitleScreen = p
	case "boxart3d":
		g.Boxart3D = p
	case "fanart":
		g.FanArt = p
	}
}

func orPick(v string, pool []string, rng *rand.Rand) string {
	pick := pool[rng.IntN(len(pool))]
	if v != "" {
		return v
	}
	return pick
}

// esDate turns a GameDataBase date (YYYY, YYYY-MM or YYYY-MM-DD) into the
// gamelist format, inventing one for a title without.
func esDate(date string, rng *rand.Rand) string {
	invented := fmt.Sprintf("%04d%02d%02dT000000", 1985+rng.IntN(20), 1+rng.IntN(12), 1+rng.IntN(28))
	parts := strings.Split(date, "-")
	if date == "" || len(parts[0]) != 4 {
		return invented
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for i := 1; i < 3; i++ {
		if i < len(parts) && len(parts[i]) == 2 {
			b.WriteString(parts[i])
		} else {
			b.WriteString("01")
		}
	}
	b.WriteString("T000000")
	return b.String()
}
