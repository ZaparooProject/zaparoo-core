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

package shared_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
)

// SchemeIDTest must accept exactly the paths an ExtractSchemeID-based Launch
// accepts. Selection matching on the scheme alone lets DoLaunch stop the
// running media before Launch rejects the path.
func TestSchemeIDTestAcceptsOnlyUsableIDs(t *testing.T) {
	t.Parallel()

	accept := shared.SchemeIDTest(shared.SchemeHeroic)

	for _, path := range []string{
		"heroic://1234/Some Game",
		"heroic://abc-def/Another",
		"HEROIC://1234/Case Insensitive Scheme",
		"heroic://1234",
	} {
		assert.True(t, accept(nil, path), path)
	}

	for _, path := range []string{
		"heroic://",
		"heroic:///Game",
		"steam://1234/Wrong Scheme",
		"/media/fat/games/NES/game.nes",
		"",
	} {
		assert.False(t, accept(nil, path), path)
	}
}
