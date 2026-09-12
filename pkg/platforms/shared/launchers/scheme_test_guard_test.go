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

//go:build linux

package launchers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DoLaunch stops the running media as soon as a launcher is selected, and a
// launcher declaring Schemes without a Test is selected for anything carrying
// the prefix. Any such launcher whose Launch parses an ID therefore takes down
// whatever is playing before reporting a malformed path, so each one must
// reject the unusable forms during selection.
//
// The browser launcher is the deliberate exception: it hands the whole URL to
// an external opener and parses nothing, so it has no path to reject.
func TestSchemeLaunchersRejectUnusablePathsDuringSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		good     string
		launcher platforms.Launcher
		wantTest bool
	}{
		{launcher: NewHeroicLauncher(HeroicOptions{}), good: "heroic://1234/Game", wantTest: true},
		{launcher: NewLutrisLauncher(LutrisOptions{}), good: "lutris://some-slug/Game", wantTest: true},
		{launcher: NewBottlesLauncher(), good: "bottles://bottle/Program", wantTest: true},
		{launcher: NewFaugusLauncher(), good: "faugus://1234/Game", wantTest: true},
		{launcher: NewWebBrowserLauncher(), good: "https://example.com/page", wantTest: false},
	} {
		t.Run(tc.launcher.ID, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, tc.launcher.Schemes, "expected a scheme launcher")
			if !tc.wantTest {
				assert.Nil(t, tc.launcher.Test,
					"a launcher that parses nothing must not filter valid paths")
				return
			}

			require.NotNil(t, tc.launcher.Test,
				"a scheme launcher whose Launch parses an ID must reject bad paths in Test")
			scheme := tc.launcher.Schemes[0]
			assert.True(t, tc.launcher.Test(nil, tc.good), tc.good)
			for _, bad := range []string{scheme + "://", scheme + ":///Game", "/not/a/uri"} {
				assert.False(t, tc.launcher.Test(nil, bad), bad)
			}
		})
	}
}
