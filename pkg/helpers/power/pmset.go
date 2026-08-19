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

package power

import (
	"regexp"
	"strconv"
	"strings"
)

// pmsetPercentRe matches the charge pmset prints on a battery line, as in
// "-InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true".
var pmsetPercentRe = regexp.MustCompile(`(\d{1,3})%`)

// parsePmsetBatt resolves the four states the updater distinguishes from the
// output of `pmset -g batt`.
//
// It lives outside the darwin build tag so it can be tested on the machines
// that actually run the test suite. Only the command that produces its input
// is macOS-specific.
//
// A Mac with no battery line is desktop hardware such as a Mac mini, which is
// the common case and always safe to install on. A battery whose charge cannot
// be read is reported as unknown rather than assumed full, because the cost of
// being wrong is a laptop that dies mid-install.
func parsePmsetBatt(output string) Status {
	var (
		sawSource bool
		external  bool
		batteries int
		lowest    = -1
	)

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Now drawing from"):
			// pmset names the supply in quotes: 'AC Power' or 'Battery Power'.
			sawSource = true
			if strings.Contains(line, "'AC Power'") {
				external = true
			}
		case strings.Contains(line, "present: true"):
			// A battery pmset knows about but reports as absent belongs to a
			// removed or unseated pack, and must not count as a supply the
			// device is running on.
			batteries++
			// A battery charging or already charged is on external power even
			// when the source line says otherwise, which is what a Mac reports
			// in the moment a charger is plugged in.
			if strings.Contains(line, "; charging") || strings.Contains(line, "; charged") {
				external = true
			}
			percent, ok := parsePmsetPercent(line)
			if !ok {
				continue
			}
			if lowest < 0 || percent < lowest {
				lowest = percent
			}
		}
	}

	if !sawSource && batteries == 0 {
		// Not pmset output at all. Saying "no battery" here would hand the
		// updater a green light it has no reading to support.
		return Status{Source: SourceUnknown}
	}
	if batteries == 0 {
		return Status{Source: SourceNoBattery}
	}
	if external {
		return Status{Source: SourceExternal}
	}
	if lowest < 0 {
		return Status{Source: SourceUnknown}
	}
	return Status{Source: SourceBattery, Percent: lowest}
}

// parsePmsetPercent reads the charge from one pmset battery line.
func parsePmsetPercent(line string) (int, bool) {
	match := pmsetPercentRe.FindStringSubmatch(line)
	if match == nil {
		return 0, false
	}
	percent, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	// pmset is supposed to report 0-100, and junk must not read as a full
	// battery.
	if percent < 0 || percent > 100 {
		return 0, false
	}
	return percent, true
}
