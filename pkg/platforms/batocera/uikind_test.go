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

package batocera

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/stretchr/testify/assert"
)

// Batocera shows notices and loaders only, which is what PresentUI accepts, so
// pickers and confirmations are left to API clients.
func TestPlatform_RendersUIKind(t *testing.T) {
	t.Parallel()

	var renderer uievents.Renderer = &Platform{}
	assert.True(t, uievents.RendersKind(renderer, models.UIEventKindNotice))
	assert.True(t, uievents.RendersKind(renderer, models.UIEventKindLoader))
	assert.False(t, uievents.RendersKind(renderer, models.UIEventKindPicker))
	assert.False(t, uievents.RendersKind(renderer, models.UIEventKindConfirm))
}
