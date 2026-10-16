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

package bazzite

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
)

// Steam Input can hide controllers here, so even when the desktop idle source
// and event nodes both work the platform must not claim full coverage.
func TestInputCoverageNeverAll(t *testing.T) {
	t.Parallel()
	got := withoutControllers(platforms.InputAll)
	assert.NotEqual(t, platforms.InputAll, got)
	assert.False(t, got.Has(platforms.InputController))
	assert.True(t, got.Has(platforms.InputKeyboard|platforms.InputMouse))
	assert.Equal(t, platforms.InputClass(0), withoutControllers(platforms.InputController))
}
