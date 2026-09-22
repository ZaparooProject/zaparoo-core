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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

// TestCommandPolicyZeroValueIsUnrestricted pins the default. Every channel
// with no allowlist of its own — a reader scan, the run API, a hook — leaves
// the field alone, so the zero value has to mean "no bound" rather than
// "nothing allowed".
func TestCommandPolicyZeroValueIsUnrestricted(t *testing.T) {
	t.Parallel()

	var policy tokens.CommandPolicy
	assert.True(t, policy.Unrestricted())
	assert.True(t, policy.Allows("execute"))
	assert.Nil(t, policy.Names())
}

// TestCommandPolicyEmptyNamesIsUnrestricted pins that a policy cannot be
// built that forbids everything. A caller passing nothing has bounded nothing,
// and the alternative — a policy that refuses every command — would break a
// device quietly rather than loudly.
func TestCommandPolicyEmptyNamesIsUnrestricted(t *testing.T) {
	t.Parallel()

	for name, policy := range map[string]tokens.CommandPolicy{
		"no names":         tokens.NewCommandPolicy(),
		"empty name":       tokens.NewCommandPolicy(""),
		"whitespace names": tokens.NewCommandPolicy("  ", "\t"),
	} {
		assert.True(t, policy.Unrestricted(), name)
		assert.True(t, policy.Allows("execute"), name)
	}
}

// TestCommandPolicyMatchesCaseInsensitively pins that the bound matches the
// way the parser does. A card is free to say **LAUNCH: and the bound has to
// recognise it as the same verb.
func TestCommandPolicyMatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	policy := tokens.NewCommandPolicy("Launch", " playlist.open ")
	assert.True(t, policy.Allows("launch"))
	assert.True(t, policy.Allows("LAUNCH"))
	assert.True(t, policy.Allows(" Launch "))
	assert.True(t, policy.Allows("playlist.open"))
	assert.False(t, policy.Allows("launch.system"))
	assert.False(t, policy.Allows(""))
}

// TestCommandPolicyNamesAreSortedForLogs pins the shape of what a refusal
// logs. It is the only way a user sees why a card link would not run, so the
// order has to be stable rather than map order.
func TestCommandPolicyNamesAreSortedForLogs(t *testing.T) {
	t.Parallel()

	policy := tokens.NewCommandPolicy("stop", "launch", "playlist.open", "launch")
	assert.Equal(t, []string{"launch", "playlist.open", "stop"}, policy.Names())
}
