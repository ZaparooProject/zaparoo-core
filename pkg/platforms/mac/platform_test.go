//go:build darwin

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

package mac

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchersIncludesWebBrowser(t *testing.T) {
	t.Parallel()

	launchers := (&Platform{}).Launchers(&config.Instance{})
	var browser *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "WebBrowser" {
			browser = &launchers[i]
			break
		}
	}

	require.NotNil(t, browser)
	require.ElementsMatch(t, []string{"http", "https"}, browser.Schemes)
	require.Equal(t, platforms.LifecycleFireAndForget, browser.Lifecycle)
	require.NotNil(t, browser.Launch)
}

func TestWebBrowserLauncherLaunch(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"http://example.com", "https://example.com/path"} {
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			var openedURL string
			launcher := newWebBrowserLauncher(func(path string) error {
				openedURL = path
				return nil
			})

			proc, err := launcher.Launch(nil, url, nil)

			require.NoError(t, err)
			assert.Nil(t, proc)
			assert.Equal(t, url, openedURL)
		})
	}
}

func TestWebBrowserLauncherReturnsOpenError(t *testing.T) {
	t.Parallel()

	launcher := newWebBrowserLauncher(func(string) error {
		return assert.AnError
	})

	proc, err := launcher.Launch(nil, "https://example.com", nil)

	assert.Nil(t, proc)
	require.ErrorContains(t, err, "failed to open URL in browser")
	require.ErrorIs(t, err, assert.AnError)
}
