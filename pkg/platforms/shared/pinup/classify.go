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

package pinup

import (
	"path/filepath"
	"strings"
)

// EmulatorClass groups Popper emulators by the pinball program they run, which
// decides the executables Core watches when Popper does not name one.
type EmulatorClass int

const (
	// ClassNone is an emulator that does not run pinball tables.
	ClassNone EmulatorClass = iota
	ClassVisualPinball
	ClassFuturePinball
	ClassPinballFX
	ClassPinballM
	ClassZaccaria
	ClassProPinball
	// ClassOtherPinball is a pinball emulator with no known executable list;
	// only Popper's own ProcessName can identify its process.
	ClassOtherPinball
)

// classRule matches an emulator by substrings of its name, display name and
// launch script, or by one of its game file extensions.
type classRule struct {
	keywords []string
	exts     []string
	class    EmulatorClass
}

// classRules are checked in order; the first match wins, so specific programs
// come before the generic "pinball" rule.
var classRules = []classRule{ //nolint:gochecknoglobals // Static classification table.
	{class: ClassVisualPinball, keywords: []string{"vpx", "vpinball", "visual pinball"}, exts: []string{"vpx", "vpt"}},
	{class: ClassFuturePinball, keywords: []string{"future pinball", "fploader"}, exts: []string{"fpt"}},
	{class: ClassPinballFX, keywords: []string{"pinball fx", "pinballfx", "fx3", "fx2"}, exts: []string{"pxp"}},
	{class: ClassPinballM, keywords: []string{"pinball m", "pinballm", "2337640"}},
	{class: ClassZaccaria, keywords: []string{"zaccaria"}},
	{class: ClassProPinball, keywords: []string{"pro pinball", "propinball"}},
	{class: ClassOtherPinball, keywords: []string{"pinball"}},
}

// knownExecutables lists the process image names each pinball program runs
// under. A trailing * makes an entry a prefix, which covers the many renamed
// VPX builds cabinets keep side by side (VPinballX107_64bit.exe and so on).
// Stock Popper installs leave ProcessName empty, so these carry the tracking.
var knownExecutables = map[EmulatorClass][]string{ //nolint:gochecknoglobals // Static executable table.
	ClassVisualPinball: {"VPinballX*", "VPinball*"},
	ClassFuturePinball: {"Future Pinball.exe", "FPLoader.exe"},
	ClassPinballFX:     {"Pinball FX*", "PinballFX*"},
	ClassPinballM:      {"PinballM*", "Pinball M*"},
	ClassZaccaria:      {"Zaccaria*"},
	ClassProPinball:    {"ProPinball*", "Pro Pinball*"},
}

// prefixMarker ends a candidate that matches every image starting with it.
const prefixMarker = "*"

// popperHelpers are Popper's own launch-script tools, which appear in every
// emulator's script and so say nothing about the program it runs. They are
// removed before the script is searched, or VPXSTARTER.exe in a Future
// Pinball script would make it look like Visual Pinball.
var popperHelpers = []string{ //nolint:gochecknoglobals // Static list of Popper tool names.
	"vpxstarter", "pupcloser", "popperkeepfocus", "sendpupevent", "legacystarter", "forcereturn",
	"closevpxeditor", "startpuppack", "poprecordpup", "puplauncher",
}

// ClassifyEmulator decides which pinball program an emulator runs, or
// ClassNone when it is not a pinball emulator at all. The game file
// extension is the most reliable signal, then the emulator's own names; the
// launch script is only consulted when neither says anything.
func ClassifyEmulator(e *Emulator) EmulatorClass {
	exts := splitExtensions(e.GamesExt)
	for _, rule := range classRules {
		for _, ext := range rule.exts {
			if _, ok := exts[ext]; ok {
				return rule.class
			}
		}
	}
	if class := matchKeywords(strings.ToLower(e.Name + " " + e.Display)); class != ClassNone {
		return class
	}
	script := strings.ToLower(e.LaunchScript)
	for _, helper := range popperHelpers {
		script = strings.ReplaceAll(script, helper, "")
	}
	return matchKeywords(script)
}

// matchKeywords returns the first rule whose keywords appear in text.
func matchKeywords(text string) EmulatorClass {
	if strings.TrimSpace(text) == "" {
		return ClassNone
	}
	for _, rule := range classRules {
		for _, keyword := range rule.keywords {
			if strings.Contains(text, keyword) {
				return rule.class
			}
		}
	}
	return ClassNone
}

// IsPinball reports whether the emulator runs pinball tables.
func IsPinball(e *Emulator) bool {
	return ClassifyEmulator(e) != ClassNone
}

// splitExtensions parses Popper's GamesExt field, a list of extensions
// separated by commas, semicolons or spaces, with or without leading dots.
func splitExtensions(value string) map[string]struct{} {
	exts := make(map[string]struct{})
	for _, ext := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '|'
	}) {
		ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if ext != "" {
			exts[ext] = struct{}{}
		}
	}
	return exts
}

// CandidateExecutables returns the process images that may be the running
// table for an emulator, most specific first: the process Popper is
// configured to watch, then the executables its program is known to run
// under. Exact names carry the .exe suffix; entries ending in * are prefixes.
func CandidateExecutables(e *Emulator) []string {
	return TableExecutables(nil, e)
}

// TableExecutables is CandidateExecutables with the table's own alternate
// launcher executable, when Popper is configured to run it with one, placed
// first.
func TableExecutables(table *Table, e *Emulator) []string {
	seen := make(map[string]struct{})
	candidates := make([]string, 0, 4)
	add := func(name string) {
		name = normalizeCandidate(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, name)
	}
	if table != nil {
		add(table.AltExe)
	}
	add(e.ProcessName)
	for _, exe := range knownExecutables[ClassifyEmulator(e)] {
		add(exe)
	}
	return candidates
}

// normalizeCandidate keeps prefix entries as they are and normalises exact
// names to bare image names with the .exe suffix.
func normalizeCandidate(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(name, prefixMarker) {
		prefix := strings.TrimSpace(strings.TrimSuffix(name, prefixMarker))
		if prefix == "" {
			return ""
		}
		return prefix + prefixMarker
	}
	return NormalizeExecutable(name)
}

// NormalizeExecutable reduces a process name or path to a bare image name
// with the .exe suffix Popper's tools and Windows both report.
func NormalizeExecutable(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if idx := strings.LastIndexAny(name, `\/`); idx >= 0 {
		name = name[idx+1:]
	}
	name = filepath.Base(name)
	if name == "." || name == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	return name
}

// MatchesExecutable reports whether an image path or name is one of the
// candidate executables, by exact name or by prefix for entries ending in *.
func MatchesExecutable(exe string, candidates []string) bool {
	base := NormalizeExecutable(exe)
	if base == "" {
		return false
	}
	lower := strings.ToLower(base)
	for _, candidate := range candidates {
		if prefix, isPrefix := strings.CutSuffix(candidate, prefixMarker); isPrefix {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) {
				return true
			}
			continue
		}
		if strings.EqualFold(base, candidate) {
			return true
		}
	}
	return false
}
