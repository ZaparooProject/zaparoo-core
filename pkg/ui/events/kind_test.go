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

package events

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

type plainRenderer struct{}

func (plainRenderer) PresentUI(context.Context, *models.UIEvent) (func() error, error) {
	return nil, nil //nolint:nilnil // test double
}

type noticeOnlyRenderer struct{ plainRenderer }

func (noticeOnlyRenderer) RendersUIKind(kind models.UIEventKind) bool {
	return kind == models.UIEventKindNotice || kind == models.UIEventKindLoader
}

func TestRendersKind(t *testing.T) {
	t.Parallel()

	assert.False(t, RendersKind(nil, models.UIEventKindPicker), "no renderer shows nothing")
	assert.True(t, RendersKind(plainRenderer{}, models.UIEventKindPicker),
		"a renderer that does not say which kinds it shows is assumed to show all")
	assert.True(t, RendersKind(noticeOnlyRenderer{}, models.UIEventKindNotice))
	assert.True(t, RendersKind(noticeOnlyRenderer{}, models.UIEventKindLoader))
	assert.False(t, RendersKind(noticeOnlyRenderer{}, models.UIEventKindPicker))
	assert.False(t, RendersKind(noticeOnlyRenderer{}, models.UIEventKindConfirm))
}

type replacingRenderer struct{ plainRenderer }

func (replacingRenderer) RendersOverMedia() bool { return false }

func TestRendersOverMedia(t *testing.T) {
	t.Parallel()

	assert.False(t, RendersOverMedia(nil))
	assert.True(t, RendersOverMedia(plainRenderer{}), "assumed harmless unless it says otherwise")
	assert.False(t, RendersOverMedia(replacingRenderer{}))
}
