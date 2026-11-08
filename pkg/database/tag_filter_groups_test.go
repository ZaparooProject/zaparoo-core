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

package database_test

import (
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
)

func TestGroupORFiltersByType(t *testing.T) {
	t.Parallel()

	or := func(tagType, value string) zapscript.TagFilter {
		return zapscript.TagFilter{Type: tagType, Value: value, Operator: zapscript.TagOperatorOR}
	}

	assert.Nil(t, database.GroupORFiltersByType(nil))

	got := database.GroupORFiltersByType([]zapscript.TagFilter{
		or("genre", "rpg"),
		or("region", "us"),
		or("genre", "action"),
		or("region", "jp"),
		or("lang", "en"),
	})
	assert.Equal(t, [][]zapscript.TagFilter{
		{or("genre", "rpg"), or("genre", "action")},
		{or("region", "us"), or("region", "jp")},
		{or("lang", "en")},
	}, got)
}

func TestGroupORFiltersByTypeCanonicalizesAliases(t *testing.T) {
	t.Parallel()

	or := func(tagType, value string) zapscript.TagFilter {
		return zapscript.TagFilter{Type: tagType, Value: value, Operator: zapscript.TagOperatorOR}
	}

	// addon:controller:rumble is a deprecated alias of embedded:vibration:rumble.
	got := database.GroupORFiltersByType([]zapscript.TagFilter{
		or("addon", "controller:rumble"),
		or("embedded", "slot:jcart"),
		or("addon", "barcode:barcodeboy"),
	})
	assert.Equal(t, [][]zapscript.TagFilter{
		{or("addon", "controller:rumble"), or("embedded", "slot:jcart")},
		{or("addon", "barcode:barcodeboy")},
	}, got)
}
