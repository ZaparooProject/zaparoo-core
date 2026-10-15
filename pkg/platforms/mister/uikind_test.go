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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/stretchr/testify/assert"
)

func TestPlatform_RendersUIKind(t *testing.T) {
	t.Parallel()

	var renderer uievents.Renderer = &Platform{}
	for _, kind := range []models.UIEventKind{
		models.UIEventKindNotice, models.UIEventKindLoader, models.UIEventKindPicker, models.UIEventKindConfirm,
	} {
		assert.True(t, uievents.RendersKind(renderer, kind), string(kind))
	}
	assert.False(t, uievents.RendersKind(renderer, models.UIEventKind("other")))
}

func TestPlatform_DoesNotRenderOverRunningMedia(t *testing.T) {
	t.Parallel()

	var renderer uievents.Renderer = &Platform{}
	assert.False(t, uievents.RendersOverMedia(renderer),
		"the menu widget replaces the running core, which ends the game")
}
