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

package tokens_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

func TestTraitsBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw         map[string]any
		name        string
		key         string
		wantValue   bool
		wantPresent bool
	}{
		{name: "absent", raw: map[string]any{"other": true}, key: "tap"},
		{name: "bool true", raw: map[string]any{"tap": true}, key: "tap", wantValue: true, wantPresent: true},
		{name: "bool false", raw: map[string]any{"tap": false}, key: "tap", wantPresent: true},
		{name: "string true", raw: map[string]any{"tap": "true"}, key: "tap", wantValue: true, wantPresent: true},
		{name: "string yes", raw: map[string]any{"tap": "YES"}, key: "tap", wantValue: true, wantPresent: true},
		{name: "string no", raw: map[string]any{"tap": "no"}, key: "tap", wantPresent: true},
		{name: "unreadable string", raw: map[string]any{"tap": "maybe"}, key: "tap"},
		{name: "int non zero", raw: map[string]any{"tap": int64(2)}, key: "tap", wantValue: true, wantPresent: true},
		{name: "int zero", raw: map[string]any{"tap": int64(0)}, key: "tap", wantPresent: true},
		{name: "float", raw: map[string]any{"tap": 0.5}, key: "tap", wantValue: true, wantPresent: true},
		{name: "array", raw: map[string]any{"tap": []any{"a"}}, key: "tap"},
		{
			name: "key lookup is normalized", raw: map[string]any{"tap": true},
			key: "TAP", wantValue: true, wantPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, present := tokens.ResolveTraits(tt.raw).Bool(tt.key)
			assert.Equal(t, tt.wantPresent, present)
			assert.Equal(t, tt.wantValue, value)
		})
	}
}

// Keys are normalized as the set is built, so a trait means the same thing
// however the script spelled it.
func TestResolveTraitsNormalizesKeys(t *testing.T) {
	t.Parallel()

	traits := tokens.ResolveTraits(map[string]any{"Hold": true, "Some_Flag": false})
	assert.Equal(t, []string{"hold", "some_flag"}, traits.Names())

	value, present := traits.Bool("HOLD")
	assert.True(t, present)
	assert.True(t, value)
}

func TestResolveTraitsEmpty(t *testing.T) {
	t.Parallel()

	for _, raw := range []map[string]any{nil, {}} {
		traits := tokens.ResolveTraits(raw)
		assert.True(t, traits.IsEmpty())
		assert.Nil(t, traits.Names())
		assert.Empty(t, traits.ScanMode())
	}
}

// A trait set is settled when it is built, so every read of it gives the same
// answer for the life of the token.
func TestTraitsAreStable(t *testing.T) {
	t.Parallel()

	traits := tokens.ResolveTraits(map[string]any{tokens.TraitHold: true})
	first := traits.ScanMode()
	for range 10 {
		assert.Equal(t, first, traits.ScanMode())
	}

	// Copying the token copies the set; the copy resolves the same.
	copied := traits
	assert.Equal(t, first, copied.ScanMode())
}

// Trait keys are case-insensitive, so "tap" and "TAP" normalize to the same
// key. The parser never emits both — its shorthand form folds them to one key
// holding the last value — but a caller that builds the map some other way gets
// the rule a #tap/#hold contradiction gets: disagreeing spellings mean the
// token declared nothing, so it inherits its reader's mode rather than forcing
// one, and the answer never turns on Go's map iteration order.
func TestResolveTraits_MixedCaseDuplicateKeysInherit(t *testing.T) {
	t.Parallel()

	for range 64 {
		traits := tokens.ResolveTraits(map[string]any{"tap": true, "TAP": false})
		assert.Empty(t, traits.ScanMode(),
			"a trait declared twice with different values must inherit, not pick one")
	}
}

// Three spellings, two of which agree, is still a contradiction: it must not
// resolve to whichever value happened to be seen twice.
func TestResolveTraits_MixedCaseTripleDuplicateKeysInherit(t *testing.T) {
	t.Parallel()

	for range 64 {
		traits := tokens.ResolveTraits(map[string]any{"hold": true, "HOLD": false, "Hold": true})
		assert.Empty(t, traits.ScanMode())
	}
}

// The same key twice with the same value is not a contradiction.
func TestResolveTraits_MixedCaseDuplicateKeysAgreeing(t *testing.T) {
	t.Parallel()

	traits := tokens.ResolveTraits(map[string]any{"hold": true, "Hold": true})
	assert.Equal(t, config.ScanModeHold, traits.ScanMode())
}
