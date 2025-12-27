// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// buildTUISettingsMenu creates the TUI settings menu with theme and mouse settings.
func buildTUISettingsMenu(
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildPrevious func(),
) {
	currentThemeName := CurrentTheme().Name
	themeIndex := 0
	for i, name := range ThemeNames {
		if name == currentThemeName {
			themeIndex = i
			break
		}
	}

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - TUI")
	if rebuildPrevious != nil {
		menu.SetRebuildPrevious(rebuildPrevious)
	}

	themeIdx := menu.GetItemCount()

	applyTheme := func() {
		applyAndSaveTheme(ThemeNames[themeIndex], pages, app, pl, rebuildPrevious)
	}

	menu.AddCycle("Theme", "Visual theme for the TUI", themeDisplayNames(), &themeIndex, func(_ string, _ int) {
		applyTheme()
	})

	mouseEnabled := config.GetTUIConfig().Mouse
	menu.AddToggle("Mouse support", "Enable mouse input in TUI", &mouseEnabled, func(value bool) {
		app.EnableMouse(value)
		tuiCfg := config.GetTUIConfig()
		tuiCfg.Mouse = value
		config.SetTUIConfig(tuiCfg)
		go func() {
			if err := config.SaveTUIConfig(helpers.ConfigDir(pl)); err != nil {
				log.Error().Err(err).Msg("failed to save TUI config")
			}
		}()
	})

	menu.AddBack()

	cycleIndices := map[int]func(delta int){
		themeIdx: func(delta int) {
			themeIndex = (themeIndex + delta + len(ThemeNames)) % len(ThemeNames)
			applyTheme()
		},
	}

	menu.SetupCycleKeys(cycleIndices)

	pageDefaults(PageSettingsTUI, pages, menu.List)
}

// themeDisplayNames returns display names for all available themes.
func themeDisplayNames() []string {
	names := make([]string, len(ThemeNames))
	for i, name := range ThemeNames {
		names[i] = AvailableThemes[name].DisplayName
	}
	return names
}

// pagesToClearOnThemeChange lists pages to remove when theme changes.
// Add new Page... constants here to ensure they rebuild with new theme colors.
var pagesToClearOnThemeChange = []string{
	PageMain,
	PageSettingsMain,
	PageSettingsBasic,
	PageSettingsAdvanced,
	PageSettingsReaderList,
	PageSettingsReaderEdit,
	PageSettingsIgnoreSystems,
	PageSettingsTagsRead,
	PageSettingsTagsWrite,
	PageSettingsAudio,
	PageSettingsReaders,
	PageSettingsScanMode,
	PageSettingsAudioMenu,
	PageSettingsReadersMenu,
	PageSettingsTUI,
	PageSearchMedia,
	PageGenerateDB,
}

func clearPagesForThemeChange(pages *tview.Pages, exceptPage string) {
	for _, pageName := range pagesToClearOnThemeChange {
		if pageName != exceptPage && pages.HasPage(pageName) {
			pages.RemovePage(pageName)
		}
	}
}

func applyAndSaveTheme(
	themeName string,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildPrevious func(),
) {
	tuiCfg := config.GetTUIConfig()
	tuiCfg.Theme = themeName
	config.SetTUIConfig(tuiCfg)
	SetCurrentTheme(themeName)

	clearPagesForThemeChange(pages, "")
	buildTUISettingsMenu(pages, app, pl, rebuildPrevious)

	go func() {
		if err := config.SaveTUIConfig(helpers.ConfigDir(pl)); err != nil {
			log.Error().Err(err).Msg("failed to save TUI config")
		}
	}()
}
