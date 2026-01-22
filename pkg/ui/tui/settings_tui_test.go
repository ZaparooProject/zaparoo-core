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

package tui

import (
	"testing"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThemeDisplayNames(t *testing.T) {
	t.Parallel()

	names := themeDisplayNames()

	require.NotEmpty(t, names)
	assert.Len(t, names, len(ThemeNames), "Should have same length as ThemeNames")

	// Verify each name is non-empty
	for i, name := range names {
		assert.NotEmpty(t, name, "Display name at index %d should not be empty", i)
	}

	// Verify display names match the themes
	for i, themeName := range ThemeNames {
		theme, ok := AvailableThemes[themeName]
		require.True(t, ok, "Theme %s should exist", themeName)
		assert.Equal(t, theme.DisplayName, names[i], "Display name should match theme")
	}
}

func TestClearPagesForThemeChange(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()

	// Add various pages
	pages.AddPage(PageMain, tview.NewTextView(), true, false)
	pages.AddPage(PageSettingsMain, tview.NewTextView(), true, false)
	pages.AddPage(PageSettingsTUI, tview.NewTextView(), true, false)
	pages.AddPage("custom_page", tview.NewTextView(), true, false)

	// Clear pages except PageSettingsTUI
	clearPagesForThemeChange(pages, PageSettingsTUI)

	// PageSettingsTUI should still exist
	assert.True(t, pages.HasPage(PageSettingsTUI), "Excepted page should still exist")

	// Other theme-change pages should be removed
	assert.False(t, pages.HasPage(PageMain), "PageMain should be removed")
	assert.False(t, pages.HasPage(PageSettingsMain), "PageSettingsMain should be removed")

	// Custom page should still exist (not in pagesToClearOnThemeChange)
	assert.True(t, pages.HasPage("custom_page"), "Custom page should still exist")
}

func TestClearPagesForThemeChange_NoExcept(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()

	// Add pages that should be cleared
	pages.AddPage(PageMain, tview.NewTextView(), true, false)
	pages.AddPage(PageSettingsMain, tview.NewTextView(), true, false)
	pages.AddPage(PageSearchMedia, tview.NewTextView(), true, false)

	// Clear all pages (empty except)
	clearPagesForThemeChange(pages, "")

	// All pages in the list should be removed
	assert.False(t, pages.HasPage(PageMain))
	assert.False(t, pages.HasPage(PageSettingsMain))
	assert.False(t, pages.HasPage(PageSearchMedia))
}

func TestClearPagesForThemeChange_NonExistentPages(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()

	// Only add one page
	pages.AddPage(PageMain, tview.NewTextView(), true, false)

	// This should not panic when trying to clear pages that don't exist
	clearPagesForThemeChange(pages, "")

	assert.False(t, pages.HasPage(PageMain))
}

func TestPagesToClearOnThemeChange(t *testing.T) {
	t.Parallel()

	// Verify the list contains expected pages
	expectedPages := []string{
		PageMain,
		PageSettingsMain,
		PageSettingsTUI,
		PageSearchMedia,
		PageGenerateDB,
	}

	for _, page := range expectedPages {
		found := false
		for _, p := range pagesToClearOnThemeChange {
			if p == page {
				found = true
				break
			}
		}
		assert.True(t, found, "pagesToClearOnThemeChange should contain %s", page)
	}
}
