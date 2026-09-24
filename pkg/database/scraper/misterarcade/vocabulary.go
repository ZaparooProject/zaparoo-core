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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/arcadegenre"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// categoryKey folds a catalog category onto its arcadegenre key.
func categoryKey(category string) string {
	key := strings.ToLower(field(category))
	key = strings.TrimSpace(strings.TrimSuffix(key, "[mature]"))
	return key
}

// genreTags resolves a category to its genre values, each followed by its
// parent where the canonical list has one. known is false for a category the
// arcadegenre tables do not list, which the caller reports.
func genreTags(category string) (values []tags.TagValue, known bool) {
	return arcadegenre.Lookup(categoryKey(category))
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
