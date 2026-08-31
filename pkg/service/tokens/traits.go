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

package tokens

import (
	"sort"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/boolutil"
	"github.com/rs/zerolog/log"
)

// Reserved traits that override the scan mode a token would otherwise inherit.
// Both are boolean, so "#tap" and "#hold" are the usual form and the value
// form ("#hold=false") means the opposite of the key.
const (
	TraitTap  = "tap"
	TraitHold = "hold"
)

// Traits is the set of traits a token declared, resolved once when the token
// enters the system and copied with the token from then on.
//
// Traits describe the token, not a step in running it, so nothing downstream
// may add to or reinterpret them: a command running half way through a script
// must see the same traits the first command saw. Every value derived from
// them, including contradictions between them, is settled here rather than at
// each place that reads them.
type Traits struct {
	values   map[string]any
	scanMode string
}

// ResolveTraits settles a parsed script's traits into the set a token carries.
// Call it once, where the token originates.
func ResolveTraits(parsed map[string]any) Traits {
	if len(parsed) == 0 {
		return Traits{}
	}

	// Trait keys are case-insensitive, so "tap" and "TAP" name one trait. The
	// parser has already folded them — its shorthand syntax lowercases keys and
	// merges later declarations over earlier ones, and its **traits JSON form
	// drops a key two members collide on — so this normalization only has to
	// hold if a caller hands over a map the parser did not build. Raw keys are
	// visited in sorted order so that, if one ever did, the winner is the same
	// on every run rather than whatever the map's iteration order landed on.
	values := make(map[string]any, len(parsed))
	rawKeys := make([]string, 0, len(parsed))
	for key := range parsed {
		rawKeys = append(rawKeys, key)
	}
	sort.Strings(rawKeys)
	for _, key := range rawKeys {
		values[gozapscript.NormalizeTraitKey(key)] = parsed[key]
	}

	return Traits{
		values:   values,
		scanMode: resolveScanMode(values),
	}
}

// IsEmpty reports whether the token declared no traits.
func (t Traits) IsEmpty() bool {
	return len(t.values) == 0
}

// Names lists the declared trait names in a stable order. Intended for
// diagnostics: it exposes what a token asked for without its values.
func (t Traits) Names() []string {
	if len(t.values) == 0 {
		return nil
	}
	names := make([]string, 0, len(t.values))
	for name := range t.values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Bool reads a trait as a boolean. Values arrive from the parser already
// type-inferred, so a bare "#flag" is a bool while "#flag=yes" is a string.
// present is false when the trait is absent or its value cannot be read as a
// boolean.
func (t Traits) Bool(name string) (value, present bool) {
	raw, ok := t.values[gozapscript.NormalizeTraitKey(name)]
	if !ok {
		return false, false
	}

	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		switch {
		case boolutil.IsTruthy(v):
			return true, true
		case boolutil.IsFalsey(v):
			return false, true
		default:
			return false, false
		}
	case int64:
		return v != 0, true
	case float64:
		return v != 0, true
	default:
		return false, false
	}
}

// ScanMode is the scan mode this token asked for, or "" when it asked for
// neither. Resolved when the set was built, so every reader gets the same
// answer and a contradiction is reported once rather than on each read.
func (t Traits) ScanMode() string {
	return t.scanMode
}

// resolveScanMode reads the reserved #tap and #hold traits. A token that
// declares both contradictory keys is treated as declaring neither, so it
// inherits the mode it would have had.
func resolveScanMode(values map[string]any) string {
	set := Traits{values: values}

	tap, tapSet := set.Bool(TraitTap)
	hold, holdSet := set.Bool(TraitHold)

	switch {
	case tapSet && holdSet:
		if tap == hold {
			log.Warn().Bool("tap", tap).Bool("hold", hold).
				Msg("token declares conflicting tap and hold traits, inheriting scan mode")
			return ""
		}
		if tap {
			return config.ScanModeTap
		}
		return config.ScanModeHold
	case tapSet:
		if tap {
			return config.ScanModeTap
		}
		return config.ScanModeHold
	case holdSet:
		if hold {
			return config.ScanModeHold
		}
		return config.ScanModeTap
	default:
		return ""
	}
}
