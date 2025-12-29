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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Theme defines all colors used in the TUI.
type Theme struct {
	HighlightFgName          string
	SecondaryColor           string
	BgColorName              string
	Name                     string
	HighlightBgName          string
	AccentColorName          string
	TextColorName            string
	DisplayName              string
	SuccessColorName         string
	WarningColorName         string
	ErrorColorName           string
	LabelColorName           string
	ContrastBackgroundColor  tcell.Color
	BorderColor              tcell.Color
	PrimitiveBackgroundColor tcell.Color
	FieldFocusedBg           tcell.Color
	FieldUnfocusedBg         tcell.Color
	ProgressFillColor        tcell.Color
	ProgressEmptyColor       tcell.Color
	ErrorColor               tcell.Color
	PrimaryTextColor         tcell.Color
	WarningColor             tcell.Color
	SecondaryTextColor       tcell.Color
	SuccessColor             tcell.Color
	InverseTextColor         tcell.Color
	LabelColor               tcell.Color
}

// ThemeDefault is the original dark blue/yellow theme.
var ThemeDefault = Theme{
	Name:        "default",
	DisplayName: "Default (Dark Blue)",

	PrimitiveBackgroundColor: tcell.ColorDarkBlue,
	ContrastBackgroundColor:  tcell.ColorBlue,
	BorderColor:              tcell.ColorLightYellow,
	PrimaryTextColor:         tcell.ColorWhite,
	SecondaryTextColor:       tcell.ColorGray,
	InverseTextColor:         tcell.ColorDarkBlue,

	BgColorName:     "darkblue",
	AccentColorName: "yellow",
	TextColorName:   "white",
	HighlightBgName: "yellow",
	HighlightFgName: "black",
	SecondaryColor:  "gray",

	FieldFocusedBg:     tcell.ColorBlue,
	FieldUnfocusedBg:   tcell.ColorDarkBlue,
	ProgressFillColor:  tcell.ColorGreen,
	ProgressEmptyColor: tcell.ColorGray,

	ErrorColor:       tcell.ColorRed,
	ErrorColorName:   "red",
	WarningColor:     tcell.ColorYellow,
	WarningColorName: "yellow",
	SuccessColor:     tcell.ColorGreen,
	SuccessColorName: "green",

	LabelColor:     tcell.ColorGray,
	LabelColorName: "gray",
}

// ThemeHighContrast uses true black background with bright yellow for accessibility.
var ThemeHighContrast = Theme{
	Name:        "high_contrast",
	DisplayName: "High Contrast",

	PrimitiveBackgroundColor: tcell.NewHexColor(0x000000),
	ContrastBackgroundColor:  tcell.NewHexColor(0x000000),
	BorderColor:              tcell.ColorYellow,
	PrimaryTextColor:         tcell.ColorWhite,
	SecondaryTextColor:       tcell.ColorWhite,
	InverseTextColor:         tcell.NewHexColor(0x000000),

	BgColorName:     "#000000",
	AccentColorName: "yellow",
	TextColorName:   "white",
	HighlightBgName: "yellow",
	HighlightFgName: "#000000",
	SecondaryColor:  "white",

	FieldFocusedBg:     tcell.ColorYellow,
	FieldUnfocusedBg:   tcell.NewHexColor(0x000000),
	ProgressFillColor:  tcell.ColorYellow,
	ProgressEmptyColor: tcell.ColorWhite,

	ErrorColor:       tcell.ColorRed,
	ErrorColorName:   "red",
	WarningColor:     tcell.ColorYellow,
	WarningColorName: "yellow",
	SuccessColor:     tcell.ColorLime,
	SuccessColorName: "lime",

	LabelColor:     tcell.ColorWhite,
	LabelColorName: "white",
}

// ThemeDracula uses the Dracula color scheme with purple accents.
var ThemeDracula = Theme{
	Name:        "dracula",
	DisplayName: "Dracula",

	PrimitiveBackgroundColor: tcell.NewHexColor(0x282A36),
	ContrastBackgroundColor:  tcell.NewHexColor(0x44475A),
	BorderColor:              tcell.NewHexColor(0xBD93F9),
	PrimaryTextColor:         tcell.NewHexColor(0xF8F8F2),
	SecondaryTextColor:       tcell.NewHexColor(0x6272A4),
	InverseTextColor:         tcell.NewHexColor(0x282A36),

	BgColorName:     "#282a36",
	AccentColorName: "#bd93f9",
	TextColorName:   "#f8f8f2",
	HighlightBgName: "#bd93f9",
	HighlightFgName: "#282a36",
	SecondaryColor:  "#6272a4",

	FieldFocusedBg:     tcell.NewHexColor(0x44475A),
	FieldUnfocusedBg:   tcell.NewHexColor(0x282A36),
	ProgressFillColor:  tcell.NewHexColor(0x50FA7B),
	ProgressEmptyColor: tcell.NewHexColor(0x44475A),

	ErrorColor:       tcell.NewHexColor(0xFF5555), // Dracula red
	ErrorColorName:   "#ff5555",
	WarningColor:     tcell.NewHexColor(0xF1FA8C), // Dracula yellow
	WarningColorName: "#f1fa8c",
	SuccessColor:     tcell.NewHexColor(0x50FA7B), // Dracula green
	SuccessColorName: "#50fa7b",

	LabelColor:     tcell.NewHexColor(0x6272A4),
	LabelColorName: "#6272a4",
}

// ThemeNord uses the Nord arctic color palette with cool blue tones.
var ThemeNord = Theme{
	Name:        "nord",
	DisplayName: "Nord",

	PrimitiveBackgroundColor: tcell.NewHexColor(0x2E3440),
	ContrastBackgroundColor:  tcell.NewHexColor(0x3B4252),
	BorderColor:              tcell.NewHexColor(0x88C0D0),
	PrimaryTextColor:         tcell.NewHexColor(0xECEFF4),
	SecondaryTextColor:       tcell.NewHexColor(0xD8DEE9),
	InverseTextColor:         tcell.NewHexColor(0x2E3440),

	BgColorName:     "#2e3440",
	AccentColorName: "#88c0d0",
	TextColorName:   "#eceff4",
	HighlightBgName: "#88c0d0",
	HighlightFgName: "#2e3440",
	SecondaryColor:  "#d8dee9",

	FieldFocusedBg:     tcell.NewHexColor(0x3B4252),
	FieldUnfocusedBg:   tcell.NewHexColor(0x2E3440),
	ProgressFillColor:  tcell.NewHexColor(0xA3BE8C),
	ProgressEmptyColor: tcell.NewHexColor(0x4C566A),

	ErrorColor:       tcell.NewHexColor(0xBF616A), // Nord red
	ErrorColorName:   "#bf616a",
	WarningColor:     tcell.NewHexColor(0xEBCB8B), // Nord yellow
	WarningColorName: "#ebcb8b",
	SuccessColor:     tcell.NewHexColor(0xA3BE8C), // Nord green
	SuccessColorName: "#a3be8c",

	LabelColor:     tcell.NewHexColor(0x4C566A),
	LabelColorName: "#4c566a",
}

// ThemeGruvbox uses the Gruvbox retro groove color scheme with warm, earthy tones.
var ThemeGruvbox = Theme{
	Name:        "gruvbox",
	DisplayName: "Gruvbox",

	PrimitiveBackgroundColor: tcell.NewHexColor(0x282828),
	ContrastBackgroundColor:  tcell.NewHexColor(0x3C3836),
	BorderColor:              tcell.NewHexColor(0xFABD2F),
	PrimaryTextColor:         tcell.NewHexColor(0xEBDBB2),
	SecondaryTextColor:       tcell.NewHexColor(0xA89984),
	InverseTextColor:         tcell.NewHexColor(0x282828),

	BgColorName:     "#282828",
	AccentColorName: "#fabd2f",
	TextColorName:   "#ebdbb2",
	HighlightBgName: "#fabd2f",
	HighlightFgName: "#282828",
	SecondaryColor:  "#a89984",

	FieldFocusedBg:     tcell.NewHexColor(0x504945),
	FieldUnfocusedBg:   tcell.NewHexColor(0x282828),
	ProgressFillColor:  tcell.NewHexColor(0xB8BB26),
	ProgressEmptyColor: tcell.NewHexColor(0x504945),

	ErrorColor:       tcell.NewHexColor(0xFB4934), // Gruvbox red
	ErrorColorName:   "#fb4934",
	WarningColor:     tcell.NewHexColor(0xFABD2F), // Gruvbox yellow
	WarningColorName: "#fabd2f",
	SuccessColor:     tcell.NewHexColor(0xB8BB26), // Gruvbox green
	SuccessColorName: "#b8bb26",

	LabelColor:     tcell.NewHexColor(0xA89984),
	LabelColorName: "#a89984",
}

// ThemeMonogreen is a retro green-on-black theme inspired by classic CRT monitors.
var ThemeMonogreen = Theme{
	Name:        "monogreen",
	DisplayName: "Mono Green (Retro)",

	PrimitiveBackgroundColor: tcell.ColorBlack,
	ContrastBackgroundColor:  tcell.NewHexColor(0x0A1A0A),
	BorderColor:              tcell.ColorGreen,
	PrimaryTextColor:         tcell.ColorGreen,
	SecondaryTextColor:       tcell.ColorDarkGreen,
	InverseTextColor:         tcell.ColorBlack,

	BgColorName:     "black",
	AccentColorName: "green",
	TextColorName:   "green",
	HighlightBgName: "green",
	HighlightFgName: "black",
	SecondaryColor:  "darkgreen",

	FieldFocusedBg:     tcell.ColorDarkGreen,
	FieldUnfocusedBg:   tcell.ColorBlack,
	ProgressFillColor:  tcell.ColorLime,
	ProgressEmptyColor: tcell.ColorDarkGreen,

	ErrorColor:       tcell.ColorRed,
	ErrorColorName:   "red",
	WarningColor:     tcell.ColorYellow,
	WarningColorName: "yellow",
	SuccessColor:     tcell.ColorLime,
	SuccessColorName: "lime",

	LabelColor:     tcell.ColorDarkGreen,
	LabelColorName: "darkgreen",
}

// AvailableThemes maps theme names to theme definitions.
var AvailableThemes = map[string]*Theme{
	"default":       &ThemeDefault,
	"high_contrast": &ThemeHighContrast,
	"dracula":       &ThemeDracula,
	"nord":          &ThemeNord,
	"gruvbox":       &ThemeGruvbox,
	"monogreen":     &ThemeMonogreen,
}

// ThemeNames returns the list of available theme names in display order.
var ThemeNames = []string{
	"default",
	"high_contrast",
	"dracula",
	"nord",
	"gruvbox",
	"monogreen",
}

var (
	currentTheme = &ThemeDefault
	themeMu      syncutil.RWMutex
)

// CurrentTheme returns the currently active theme.
func CurrentTheme() *Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return currentTheme
}

// SetCurrentTheme sets the current theme by name.
// Returns false if the theme name is not found.
func SetCurrentTheme(name string) bool {
	theme, ok := AvailableThemes[name]
	if !ok {
		return false
	}
	themeMu.Lock()
	currentTheme = theme
	themeMu.Unlock()
	ApplyTheme(theme)
	return true
}

// ApplyTheme applies the given theme to tview's global styles.
func ApplyTheme(theme *Theme) {
	tview.Styles.PrimitiveBackgroundColor = theme.PrimitiveBackgroundColor
	tview.Styles.ContrastBackgroundColor = theme.ContrastBackgroundColor
	tview.Styles.BorderColor = theme.BorderColor
	tview.Styles.PrimaryTextColor = theme.PrimaryTextColor
	tview.Styles.SecondaryTextColor = theme.SecondaryTextColor
	tview.Styles.InverseTextColor = theme.InverseTextColor
}
