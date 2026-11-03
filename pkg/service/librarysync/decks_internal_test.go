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

package librarysync

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scripts(names ...string) []deckContentItem {
	out := make([]deckContentItem, 0, len(names))
	for _, name := range names {
		out = append(out, deckContentItem{Kind: "script", Name: name, ZapScript: "**" + name})
	}
	return out
}

func mergedScripts(content *deckContent) []string {
	out := make([]string, 0, len(content.Items))
	for _, item := range content.Items {
		out = append(out, item.key())
	}
	return out
}

func TestMergeDeckItems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		base, local, server []deckContentItem
		want                []string
	}{
		{
			name: "removals on both sides and additions on both sides",
			base: scripts("a", "b", "c"), local: scripts("a", "x", "c"), server: scripts("a", "b", "y"),
			want: []string{"script:**a", "script:**x", "script:**y"},
		},
		{
			name: "an addition with nothing before it goes first",
			base: scripts("a"), local: scripts("x", "a"), server: scripts("a", "b"),
			want: []string{"script:**x", "script:**a", "script:**b"},
		},
		{
			name: "the same script added twice here stays twice",
			base: scripts("a"), local: scripts("a", "a"), server: scripts("a"),
			want: []string{"script:**a", "script:**a"},
		},
		{
			name: "an item added on both sides appears once",
			base: scripts("a"), local: scripts("a", "b"), server: scripts("b", "a"),
			want: []string{"script:**b", "script:**a"},
		},
		{
			name: "the account's order is kept",
			base: scripts("a", "b"), local: scripts("a", "b"), server: scripts("b", "a"),
			want: []string{"script:**b", "script:**a"},
		},
	}
	for _, tt := range tests {
		merged := mergeDeck(
			&deckContent{Items: tt.base}, &deckContent{Items: tt.local}, &deckContent{Items: tt.server},
		)
		assert.Equal(t, tt.want, mergedScripts(&merged), tt.name)
	}
}

func TestMergeDeckFields(t *testing.T) {
	t.Parallel()
	base := deckContent{Name: "Base", Description: "old", Items: scripts("a")}
	local := deckContent{Name: "Mine", Description: "old", Items: []deckContentItem{
		{Kind: "script", Name: "Renamed here", ZapScript: "**a"},
	}}
	server := deckContent{Name: "Theirs", Description: "new there", Items: []deckContentItem{
		{Kind: "script", Name: "a", ZapScript: "**a"},
		{Kind: "card", CardID: "CARD1"},
	}}
	merged := mergeDeck(&base, &local, &server)
	assert.Equal(t, "Mine", merged.Name, "a name changed here wins")
	assert.Equal(t, "new there", merged.Description, "a description changed only there is taken")
	assert.Equal(t, "Renamed here", merged.Items[0].Name)
	assert.Equal(t, "CARD1", merged.Items[1].CardID)
}

func TestDeckPassError(t *testing.T) {
	t.Parallel()
	failure := errors.New("push failed")
	other := errors.New("list failed")

	require.NoError(t, deckPassError(nil, nil))
	require.ErrorIs(t, deckPassError(ErrDisabled, nil), ErrDisabled)
	require.ErrorIs(t, deckPassError(nil, ErrDisabled), ErrDisabled)

	err := deckPassError(ErrDisabled, failure)
	require.ErrorIs(t, err, failure)
	assert.False(t, IsIdleError(err), "an idle pass never hides a failed one")
	err = deckPassError(failure, ErrNotSettled)
	require.ErrorIs(t, err, failure)
	assert.False(t, IsIdleError(err))

	err = deckPassError(failure, other)
	require.ErrorIs(t, err, failure)
	assert.ErrorIs(t, err, other)
}
