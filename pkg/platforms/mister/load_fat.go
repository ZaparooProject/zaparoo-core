//go:build linux && !android

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

package mister

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/fatimg"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// fatWalkDepth bounds how deep the games inside an image are searched for.
	fatWalkDepth = 10
	// volumePrefix names a volume in the path of a game inside an image that
	// holds more than one.
	volumePrefix = "Volume "
	// fatBootSettle is how long a machine booting from an SD card image gets
	// before its first command is typed.
	fatBootSettle = 8 * time.Second
	// msxMenuSettle covers Nextor booting and BASIC drawing the card's menu.
	msxMenuSettle = 40 * time.Second
)

var errNoSuchFile = errors.New("no such file in the image")

// fatSelection is one game inside a FAT image, as the commands that load it
// need to name it.
type fatSelection struct {
	image string
	// long is the path inside the volume with the names the card shows, short
	// the same path in 8.3 names.
	long, short string
	ext         string
	volume      int
}

// fatCore describes how the games inside a core's FAT images are found and
// started.
type fatCore struct {
	plan func(sel fatSelection) loadPlan
	// skipDirs are top-level folders that hold the machine's own files, not
	// games.
	skipDirs []string
	exts     []string
}

func (c *fatCore) wants(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, e := range c.exts {
		if e == ext {
			return true
		}
	}
	return false
}

func (c *fatCore) skips(name string) bool {
	for _, d := range c.skipDirs {
		if strings.EqualFold(d, name) {
			return true
		}
	}
	return false
}

// list returns the games inside an image as paths within it.
func (c *fatCore) list(fs afero.Fs, image string) []string {
	img, err := fatimg.Open(fs, image)
	if err != nil {
		return nil
	}
	defer func() {
		if closeErr := img.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("image", image).Msg("closing disk image")
		}
	}()

	volumes := img.Volumes()
	var games []string
	for i := range volumes {
		prefix := ""
		if len(volumes) > 1 {
			prefix = volumePrefix + strconv.Itoa(i+1) + "/"
		}
		top, readErr := volumes[i].ReadDir("/")
		if readErr != nil {
			continue
		}
		for _, entry := range top {
			if entry.IsDir && c.skips(entry.Name) {
				continue
			}
			if !entry.IsDir {
				if c.wants(entry.Name) {
					games = append(games, prefix+entry.Name)
				}
				continue
			}
			walkErr := volumes[i].Walk("/"+entry.Name, fatimg.WalkOptions{MaxDepth: fatWalkDepth},
				func(p fatimg.Path, e fatimg.Entry) error {
					if !e.IsDir && c.wants(e.Name) {
						games = append(games, prefix+strings.TrimPrefix(p.Long, "/"))
					}
					return nil
				})
			if walkErr != nil {
				log.Debug().Err(walkErr).Str("image", image).Msg("walking disk image")
			}
		}
	}
	return games
}

// selection resolves a path within an image to the file it names.
func selectFAT(fs afero.Fs, image, inner string) (fatSelection, error) {
	img, err := fatimg.Open(fs, image)
	if err != nil {
		return fatSelection{}, fmt.Errorf("open %s: %w", image, err)
	}
	defer func() {
		if closeErr := img.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("image", image).Msg("closing disk image")
		}
	}()

	volumes := img.Volumes()
	index := 0
	if strings.HasPrefix(inner, volumePrefix) {
		head, rest, _ := strings.Cut(inner, "/")
		n, convErr := strconv.Atoi(strings.TrimPrefix(head, volumePrefix))
		if convErr != nil || n < 1 || n > len(volumes) {
			return fatSelection{}, fmt.Errorf("%w: %s", errNoSuchFile, inner)
		}
		index, inner = n-1, rest
	}
	if index >= len(volumes) {
		return fatSelection{}, fmt.Errorf("%w: %s", errNoSuchFile, inner)
	}

	long, short := "", ""
	dir := "/"
	for _, part := range strings.Split(inner, "/") {
		entries, readErr := volumes[index].ReadDir(dir)
		if readErr != nil {
			return fatSelection{}, fmt.Errorf("read %s: %w", dir, readErr)
		}
		at := slices.IndexFunc(entries, func(e fatimg.Entry) bool {
			return strings.EqualFold(e.Name, part) || strings.EqualFold(e.ShortName, part)
		})
		if at < 0 {
			return fatSelection{}, fmt.Errorf("%w: %s", errNoSuchFile, inner)
		}
		long += "/" + entries[at].Name
		short += "/" + entries[at].ShortName
		dir = long
	}
	return fatSelection{
		image:  image,
		volume: index,
		long:   long,
		short:  short,
		ext:    strings.ToLower(path.Ext(long)),
	}, nil
}

func (c *fatCore) planner(fs afero.Fs) loadPlanner {
	return func(_ *config.Instance, p string) (loadPlan, bool, error) {
		image, inner, ok := splitImagePath(fs, p)
		if !ok {
			return loadPlan{}, false, nil
		}
		sel, err := selectFAT(fs, image, inner)
		if err != nil {
			return loadPlan{}, false, err
		}
		plan := c.plan(sel)
		plan.target = image
		return plan, true, nil
	}
}

func (c *fatCore) planners(pl platforms.Platform, fs afero.Fs) loadPlannerSet {
	hasExt := func(name string) bool { return path.Ext(name) != "" }
	return loadPlannerSet{
		plan:   c.planner(fs),
		extend: extendForImages(fs, c.list, hasExt, discoverImages(pl, fs)),
	}
}

// dosPath turns a path inside a volume into a DOS path with backslashes.
func dosPath(slashed string) string {
	return strings.ReplaceAll(strings.TrimPrefix(slashed, "/"), "/", `\`)
}

// atomCore runs programs from an AtoMMC card. Break leaves any menu for the
// BASIC prompt, where *RUN loads a program by path.
func atomCore() *fatCore {
	return &fatCore{
		exts:     []string{"", ".atm", ".abc"},
		skipDirs: []string{"System Volume Information", "LIB"},
		plan: func(sel fatSelection) loadPlan {
			return loadPlan{
				settle: fatBootSettle,
				layout: atomLayout(),
				steps: []loadStep{
					typeText("{f10}"),
					typeAfter(time.Second, fmt.Sprintf("*RUN %q{enter}", strings.ToUpper(sel.short))),
				},
			}
		},
	}
}

// spectrumCore runs tapes and snapshots from an esxDOS card. A warm reset and
// the 128K menu's 48 BASIC give a plain prompt, where esxDOS dot commands load
// the file.
func spectrumCore() *fatCore {
	return &fatCore{
		exts:     []string{".tap", ".z80", ".sna"},
		skipDirs: []string{"System Volume Information", "BIN", "SYS", "TMP"},
		plan: func(sel fatSelection) loadPlan {
			steps := []loadStep{
				typeText(chord("ctrl", "f11")),
				typeAfter(5*time.Second, "{down}{down}{down}{enter}"),
			}
			if sel.ext == ".tap" {
				steps = append(steps,
					typeAfter(2*time.Second, fmt.Sprintf(".tapein %s{enter}", sel.short)),
					typeAfter(time.Second, `J""{enter}`),
				)
			} else {
				steps = append(steps,
					typeAfter(2*time.Second, fmt.Sprintf(".snapload %s{enter}", sel.short)))
			}
			return loadPlan{settle: fatBootSettle, steps: steps, layout: spectrumLayout()}
		},
	}
}

// nextCore runs NEX programs from a NextZXOS card: after a hard reset the card
// boots to its menu, whose L opens the command line, where .nexload
// takes the path from the root of the card. Tapes and snapshots go through
// BASIC loaders that the command line's editor does not take typed, so they are
// not listed.
func nextCore() *fatCore {
	return &fatCore{
		exts: []string{".nex"},
		skipDirs: []string{
			"System Volume Information", "nextzxos", "sys", "dot", "machines", "rpi", "tbblue", "tmp", "tools", "docs",
		},
		plan: func(sel fatSelection) loadPlan {
			steps := []loadStep{
				// A launch onto a Next already running finds it wherever the last
				// program left it, so it is hard reset first.
				typeText("{f1}"),
				// Space is not used: once the menu is up it opens the menu's
				// second page, where L starts the tape loader instead.
				typeAfter(21*time.Second, "l"),
				// The line starts with whatever the menu left; an empty entry clears it.
				typeAfter(2*time.Second, strings.Repeat("{backspace}", 20)+"{enter}"),
				// A colon ends a statement there, so no drive letter is used.
				typeAfter(time.Second, ".nexload "+strings.ToLower(sel.short)+"{enter}"),
			}
			return loadPlan{settle: 6 * time.Second, steps: steps, layout: nextLayout()}
		},
	}
}

// msxCore runs ROMs from a Nextor card at its DOS prompt. Disk images are left
// out: the card's EMUFILE predates its Nextor kernel and does not boot them.
func msxCore() *fatCore {
	return &fatCore{
		exts:     []string{".rom"},
		skipDirs: []string{"System Volume Information", "BIN", "UTILS", "MM"},
		plan: func(sel fatSelection) loadPlan {
			file := `a:\` + strings.ToUpper(dosPath(sel.short))
			// The card's boot menu comes first, drawn by BASIC well after the
			// machine starts; keys typed before it is up are lost or start the
			// default entry. Its third entry is MSX-DOS, and Space is fire. The
			// menu polls the keyboard slowly, so the keys go a second apart.
			return loadPlan{
				settle:  msxMenuSettle,
				options: []mgls.Setting{mgls.Option{Name: "Slot1", Value: "MegaSCC+ 1MB"}},
				steps: []loadStep{
					typeText("{down}"),
					typeAfter(time.Second, "{down}"),
					typeAfter(time.Second, "{space}"),
					typeAfter(8*time.Second, fmt.Sprintf("romload %s /r{enter}", file)),
				},
			}
		},
	}
}

// enterpriseDrives are the drive letters EXDOS gives the volumes of an SD
// adapter card, in order.
const enterpriseDrives = "FGHIJKLMNOP"

// enterpriseCore runs programs from an SD adapter card: a cold reset takes the
// new card, any key leaves the start screen, and EXDOS runs the program from
// the drive its volume was given.
func enterpriseCore() *fatCore {
	return &fatCore{
		exts:     []string{".com"},
		skipDirs: []string{"System Volume Information"},
		plan: func(sel fatSelection) loadPlan {
			drive := string(enterpriseDrives[min(sel.volume, len(enterpriseDrives)-1)])
			steps := []loadStep{
				typeText(chord("ctrl", "f9")),
				typeAfter(6*time.Second, "{enter}"),
				typeAfter(3*time.Second, ":EXDOS{enter}"),
				typeAfter(2*time.Second, drive+":{enter}"),
			}
			dir, name := path.Split(sel.short)
			for _, part := range strings.Split(strings.Trim(dir, "/"), "/") {
				if part != "" {
					steps = append(steps, typeAfter(time.Second, "CD "+part+"{enter}"))
				}
			}
			steps = append(steps, typeAfter(time.Second, "LOAD "+name+"{enter}"))
			return loadPlan{settle: fatBootSettle, steps: steps, layout: enterpriseLayout()}
		},
	}
}

// fatPlanners returns the planner set of every core whose images are FAT
// cards holding many programs.
func fatPlanners(pl platforms.Platform, fs afero.Fs) map[string]loadPlannerSet {
	return map[string]loadPlannerSet{
		systemdefs.SystemAcornAtom:  atomCore().planners(pl, fs),
		systemdefs.SystemZXSpectrum: spectrumCore().planners(pl, fs),
		systemdefs.SystemZXNext:     nextCore().planners(pl, fs),
		systemdefs.SystemMSX:        msxCore().planners(pl, fs),
		systemdefs.SystemEnterprise: enterpriseCore().planners(pl, fs),
	}
}
