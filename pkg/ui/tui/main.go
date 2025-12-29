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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	PageMain                  = "main"
	PageSettingsMain          = "settings_main"
	PageSettingsBasic         = "settings_basic"
	PageSettingsAdvanced      = "settings_advanced"
	PageSettingsReaderList    = "settings_reader_list"
	PageSettingsReaderEdit    = "settings_reader_edit"
	PageSettingsIgnoreSystems = "settings_ignore_systems"
	PageSettingsTagsWrite     = "settings_tags_write"
	PageSettingsAudio         = "settings_audio"
	PageSettingsReaders       = "settings_readers"
	PageSettingsScanMode      = "settings_readers_scanMode"
	PageSettingsAudioMenu     = "settings_audio_menu"
	PageSettingsReadersMenu   = "settings_readers_menu"
	PageSettingsTUI           = "settings_tui"
	PageSettingsAbout         = "settings_about"
	PageSearchMedia           = "search_media"
	PageExportLog             = "export_log"
	PageGenerateDB            = "generate_db"
)

func getTokens(ctx context.Context, cfg *config.Instance) (models.TokensResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodTokens, "")
	if err != nil {
		return models.TokensResponse{}, fmt.Errorf("failed to get tokens from local client: %w", err)
	}
	var tokens models.TokensResponse
	err = json.Unmarshal([]byte(resp), &tokens)
	if err != nil {
		return models.TokensResponse{}, fmt.Errorf("failed to unmarshal tokens response: %w", err)
	}
	return tokens, nil
}

func getReaders(ctx context.Context, cfg *config.Instance) (models.ReadersResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodReaders, "")
	if err != nil {
		return models.ReadersResponse{}, fmt.Errorf("failed to get readers from local client: %w", err)
	}
	var readers models.ReadersResponse
	err = json.Unmarshal([]byte(resp), &readers)
	if err != nil {
		return models.ReadersResponse{}, fmt.Errorf("failed to unmarshal readers response: %w", err)
	}
	return readers, nil
}

// ButtonGridItem represents a button in the grid with its help text.
type ButtonGridItem struct {
	Button   *tview.Button
	HelpText string
	Disabled bool
}

// ButtonGrid is a 2-row button grid for the main menu.
type ButtonGrid struct {
	*tview.Box
	app        *tview.Application
	onHelp     func(string)
	onEscape   func()
	buttons    [][]*ButtonGridItem
	focusedRow int
	focusedCol int
	cols       int
}

// NewButtonGrid creates a new 2-row button grid.
func NewButtonGrid(app *tview.Application, cols int) *ButtonGrid {
	return &ButtonGrid{
		Box:     tview.NewBox(),
		app:     app,
		buttons: make([][]*ButtonGridItem, 2),
		cols:    cols,
	}
}

// AddRow adds a row of buttons to the grid.
func (bg *ButtonGrid) AddRow(items ...*ButtonGridItem) *ButtonGrid {
	row := 0
	if len(bg.buttons[0]) > 0 {
		row = 1
	}
	bg.buttons[row] = items
	return bg
}

// SetOnHelp sets the callback for help text changes.
func (bg *ButtonGrid) SetOnHelp(fn func(string)) *ButtonGrid {
	bg.onHelp = fn
	return bg
}

// SetOnEscape sets the callback for escape key.
func (bg *ButtonGrid) SetOnEscape(fn func()) *ButtonGrid {
	bg.onEscape = fn
	return bg
}

// triggerHelp calls the help callback with current button's help text.
func (bg *ButtonGrid) triggerHelp() {
	if bg.onHelp != nil && len(bg.buttons) > bg.focusedRow && len(bg.buttons[bg.focusedRow]) > bg.focusedCol {
		item := bg.buttons[bg.focusedRow][bg.focusedCol]
		if item != nil {
			bg.onHelp(item.HelpText)
		}
	}
}

// FocusFirst sets focus to first enabled button.
func (bg *ButtonGrid) FocusFirst() {
	bg.focusedRow = 0
	bg.focusedCol = 0
	// Only search for next enabled if current position is disabled
	if !bg.isCurrentEnabled() {
		bg.findNextEnabled(1, 0)
	}
	bg.triggerHelp()
}

// findNextEnabled moves to next enabled button in given direction.
func (bg *ButtonGrid) findNextEnabled(colDir, rowDir int) bool {
	startRow, startCol := bg.focusedRow, bg.focusedCol
	for {
		bg.focusedCol += colDir
		bg.focusedRow += rowDir

		// Wrap columns
		if bg.focusedCol >= bg.cols {
			bg.focusedCol = 0
			bg.focusedRow++
		} else if bg.focusedCol < 0 {
			bg.focusedCol = bg.cols - 1
			bg.focusedRow--
		}

		// Wrap rows
		if bg.focusedRow >= 2 {
			bg.focusedRow = 0
		} else if bg.focusedRow < 0 {
			bg.focusedRow = 1
		}

		// Check if we've wrapped around completely
		if bg.focusedRow == startRow && bg.focusedCol == startCol {
			return false
		}

		// Check if current button is enabled
		if len(bg.buttons) > bg.focusedRow && len(bg.buttons[bg.focusedRow]) > bg.focusedCol {
			item := bg.buttons[bg.focusedRow][bg.focusedCol]
			if item != nil && !item.Disabled {
				return true
			}
		}
	}
}

// Draw renders the button grid.
func (bg *ButtonGrid) Draw(screen tcell.Screen) {
	bg.DrawForSubclass(screen, bg)

	x, y, width, height := bg.GetInnerRect()
	if width <= 0 || height < 2 || len(bg.buttons) < 2 {
		return
	}

	hasFocus := bg.HasFocus()
	buttonWidth := width / bg.cols
	spacing := 1

	for rowIdx, row := range bg.buttons {
		if row == nil {
			continue
		}
		rowY := y + rowIdx
		if rowIdx >= height {
			break
		}

		for colIdx, item := range row {
			if item == nil || item.Button == nil {
				continue
			}

			btnX := x + colIdx*(buttonWidth+spacing)
			btnWidth := buttonWidth
			if colIdx == len(row)-1 {
				// Last button in row takes remaining space
				btnWidth = width - colIdx*(buttonWidth+spacing)
			}

			item.Button.SetRect(btnX, rowY, btnWidth, 1)

			// Show focus state
			if hasFocus && rowIdx == bg.focusedRow && colIdx == bg.focusedCol {
				item.Button.Focus(func(_ tview.Primitive) {})
			} else {
				item.Button.Blur()
			}

			item.Button.Draw(screen)
		}
	}
}

// Focus implements tview.Primitive.
func (bg *ButtonGrid) Focus(delegate func(p tview.Primitive)) {
	bg.triggerHelp()
	bg.Box.Focus(delegate)
}

// InputHandler handles keyboard input.
func (bg *ButtonGrid) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return bg.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		switch event.Key() {
		case tcell.KeyLeft, tcell.KeyBacktab:
			oldCol := bg.focusedCol
			bg.focusedCol--
			if bg.focusedCol < 0 {
				bg.focusedCol = bg.cols - 1
			}
			if !bg.isCurrentEnabled() {
				bg.focusedCol = oldCol
				bg.findNextEnabled(-1, 0)
			}
			bg.triggerHelp()

		case tcell.KeyRight, tcell.KeyTab:
			oldCol := bg.focusedCol
			bg.focusedCol++
			if bg.focusedCol >= bg.cols {
				bg.focusedCol = 0
			}
			if !bg.isCurrentEnabled() {
				bg.focusedCol = oldCol
				bg.findNextEnabled(1, 0)
			}
			bg.triggerHelp()

		case tcell.KeyUp:
			oldRow := bg.focusedRow
			bg.focusedRow--
			if bg.focusedRow < 0 {
				bg.focusedRow = 1
			}
			if !bg.isCurrentEnabled() {
				bg.focusedRow = oldRow
			}
			bg.triggerHelp()

		case tcell.KeyDown:
			oldRow := bg.focusedRow
			bg.focusedRow++
			if bg.focusedRow >= 2 {
				bg.focusedRow = 0
			}
			if !bg.isCurrentEnabled() {
				bg.focusedRow = oldRow
			}
			bg.triggerHelp()

		case tcell.KeyEnter:
			if bg.isCurrentEnabled() {
				item := bg.buttons[bg.focusedRow][bg.focusedCol]
				if handler := item.Button.InputHandler(); handler != nil {
					handler(event, setFocus)
				}
			}

		case tcell.KeyEscape:
			if bg.onEscape != nil {
				bg.onEscape()
			}

		default:
			// Ignore other keys
		}
	})
}

// isCurrentEnabled checks if the currently focused button is enabled.
func (bg *ButtonGrid) isCurrentEnabled() bool {
	if len(bg.buttons) > bg.focusedRow && len(bg.buttons[bg.focusedRow]) > bg.focusedCol {
		item := bg.buttons[bg.focusedRow][bg.focusedCol]
		return item != nil && !item.Disabled
	}
	return false
}

// MouseHandler handles mouse input.
func (bg *ButtonGrid) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (bool, tview.Primitive) {
	return bg.WrapMouseHandler(func(
		action tview.MouseAction,
		event *tcell.EventMouse,
		setFocus func(p tview.Primitive),
	) (bool, tview.Primitive) {
		if action == tview.MouseLeftClick {
			mouseX, mouseY := event.Position()
			for rowIdx, row := range bg.buttons {
				for colIdx, item := range row {
					if item == nil || item.Button == nil || item.Disabled {
						continue
					}
					bx, by, bw, bh := item.Button.GetRect()
					if mouseX >= bx && mouseX < bx+bw && mouseY >= by && mouseY < by+bh {
						bg.focusedRow = rowIdx
						bg.focusedCol = colIdx
						setFocus(bg)
						bg.triggerHelp()
						if handler := item.Button.InputHandler(); handler != nil {
							handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), setFocus)
						}
						return true, bg
					}
				}
			}
		}
		return false, nil
	})
}

// MainFrame wraps the main page content and adds keyboard hints to the bottom border.
type MainFrame struct {
	*tview.Box
	content tview.Primitive
}

// NewMainFrame creates a wrapper that adds hints to the main page.
func NewMainFrame(content tview.Primitive) *MainFrame {
	return &MainFrame{
		Box:     tview.NewBox(),
		content: content,
	}
}

// Draw renders the wrapped content and adds hints to the bottom border.
func (mf *MainFrame) Draw(screen tcell.Screen) {
	x, y, width, height := mf.GetRect()
	if mf.content != nil {
		mf.content.SetRect(x, y, width, height)
		mf.content.Draw(screen)
	}

	// Draw hints in the bottom border
	if height > 2 && width > 4 {
		bottomY := y + height - 1

		// Get hints runes and calculate centering
		hints := defaultHintsRunes()
		availableWidth := width - 4
		if len(hints) > availableWidth {
			hints = hints[:availableWidth]
		}

		startX := x + (width-len(hints))/2

		// Get theme colors - use border color (same as title) on primitive background
		t := CurrentTheme()
		style := tcell.StyleDefault.
			Foreground(t.BorderColor).
			Background(t.PrimitiveBackgroundColor)

		// Clear just the text area with padding
		clearStart := startX - 1
		clearEnd := startX + len(hints) + 1
		for i := clearStart; i < clearEnd; i++ {
			screen.SetContent(i, bottomY, ' ', nil, style)
		}

		// Draw the hints
		for i, r := range hints {
			screen.SetContent(startX+i, bottomY, r, nil, style)
		}
	}
}

// Focus delegates to the wrapped content.
func (mf *MainFrame) Focus(delegate func(p tview.Primitive)) {
	if mf.content != nil {
		delegate(mf.content)
	}
}

// HasFocus returns whether the wrapped content has focus.
func (mf *MainFrame) HasFocus() bool {
	if mf.content != nil {
		return mf.content.HasFocus()
	}
	return false
}

// InputHandler delegates to the wrapped content.
func (mf *MainFrame) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	if mf.content != nil {
		return mf.content.InputHandler()
	}
	return nil
}

// MouseHandler delegates to the wrapped content.
func (mf *MainFrame) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (bool, tview.Primitive) {
	if mf.content != nil {
		return mf.content.MouseHandler()
	}
	return nil
}

var mainPageNotifyCancel context.CancelFunc

func BuildMainPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) tview.Primitive {
	if mainPageNotifyCancel != nil {
		mainPageNotifyCancel()
	}

	notifyCtx, notifyCancel := context.WithCancel(context.Background())
	mainPageNotifyCancel = notifyCancel

	svcRunning := isRunning()
	log.Debug().Bool("svcRunning", svcRunning).Msg("TUI: service status check")

	// Create main container
	main := tview.NewFlex().SetDirection(tview.FlexRow)
	main.SetTitle(" Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ") ").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	// Left column: intro + status
	introText := tview.NewTextView().
		SetText("Visit [::bu:https://zaparoo.org]zaparoo.org[::-:-] for guides and support.\n").
		SetDynamicColors(true).
		SetWordWrap(true)

	statusText := tview.NewTextView().SetDynamicColors(true)

	t := CurrentTheme()
	var svcStatus string
	if svcRunning {
		svcStatus = fmt.Sprintf("[%s]✓ RUNNING[-]", t.SuccessColorName)
	} else {
		svcStatus = fmt.Sprintf("[%s]✗ NOT RUNNING[-]", t.ErrorColorName) +
			"\nService may not have started.\nCheck Logs for details."
	}

	ip := helpers.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	webUI := fmt.Sprintf("http://%s:%d/app/", ip, cfg.APIPort())

	statusText.SetText(fmt.Sprintf(
		"%s %s\n%s %s\n%s\n[:::%s]%s[:::-]",
		FormatLabel("Service"), svcStatus,
		FormatLabel("Address"), ipDisplay,
		FormatLabel("Web UI"),
		webUI, webUI,
	))

	leftColumn := tview.NewFlex().SetDirection(tview.FlexRow)
	leftColumn.AddItem(introText, 3, 0, false)
	leftColumn.AddItem(statusText, 0, 1, false)

	// Right column: Scanning Area with animation
	scanningArea := NewScanningArea(app)

	if svcRunning {
		// Get initial reader count
		ctx, cancel := tuiContext()
		readers, err := getReaders(ctx, cfg)
		cancel()
		if err != nil {
			log.Error().Err(err).Msg("failed to get readers")
		} else {
			driver := ""
			if len(readers.Readers) > 0 {
				driver = readers.Readers[0].Driver
			}
			scanningArea.SetReaderInfo(len(readers.Readers), driver)
		}

		// Check for active token currently on reader
		ctx, cancel = tuiContext()
		tokens, err := getTokens(ctx, cfg)
		cancel()
		if err == nil && len(tokens.Active) > 0 {
			scanningArea.SetTokenInfo(
				tokens.Active[0].ScanTime.Format("2006-01-02 15:04:05"),
				tokens.Active[0].UID,
				tokens.Active[0].Text,
			)
		}

		go func() {
			log.Debug().Msg("starting notification listener")
			retryDelay := time.Second
			const maxRetryDelay = 30 * time.Second

			for {
				select {
				case <-notifyCtx.Done():
					log.Debug().Msg("notification listener cancelled")
					scanningArea.Stop()
					return
				default:
				}

				notifyType, resp, err := client.WaitNotifications(
					notifyCtx, -1, cfg,
					models.NotificationTokensAdded,
					models.NotificationTokensRemoved,
					models.NotificationReadersConnected,
					models.NotificationReadersDisconnected,
				)
				switch {
				case errors.Is(err, client.ErrRequestTimeout):
					retryDelay = time.Second
					continue
				case errors.Is(err, client.ErrRequestCancelled):
					log.Debug().Msg("notification listener: request cancelled")
					return
				case err != nil:
					log.Warn().Err(err).Dur("retry_in", retryDelay).Msg("notification listener error, retrying")
					select {
					case <-notifyCtx.Done():
						return
					case <-time.After(retryDelay):
					}
					retryDelay *= 2
					if retryDelay > maxRetryDelay {
						retryDelay = maxRetryDelay
					}
					continue
				}

				retryDelay = time.Second

				log.Debug().Str("type", notifyType).Msg("received notification")

				switch notifyType {
				case models.NotificationTokensAdded:
					var token models.TokenResponse
					if err := json.Unmarshal([]byte(resp), &token); err != nil {
						log.Error().Err(err).Str("resp", resp).Msg("error unmarshalling token notification")
						continue
					}
					app.QueueUpdateDraw(func() {
						scanningArea.SetTokenInfo(
							token.ScanTime.Format("2006-01-02 15:04:05"),
							token.UID,
							token.Text,
						)
					})

				case models.NotificationTokensRemoved:
					app.QueueUpdateDraw(func() {
						scanningArea.ClearToken()
					})

				case models.NotificationReadersConnected, models.NotificationReadersDisconnected:
					ctx, cancel := tuiContext()
					readers, err := getReaders(ctx, cfg)
					cancel()
					if err != nil {
						log.Error().Err(err).Msg("failed to refresh reader status")
						continue
					}
					driver := ""
					if len(readers.Readers) > 0 {
						driver = readers.Readers[0].Driver
					}
					app.QueueUpdateDraw(func() {
						scanningArea.SetReaderInfo(len(readers.Readers), driver)
					})
				}
			}
		}()
	}

	rightColumn := tview.NewFlex().SetDirection(tview.FlexRow)
	rightColumn.AddItem(scanningArea, 0, 1, false)

	// 2-column content layout
	divider := NewVerticalDivider()
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(leftColumn, 0, 1, false)
	contentFlex.AddItem(divider, 1, 0, false)
	contentFlex.AddItem(rightColumn, 0, 1, false)

	// Help text (centered like other pages)
	helpText := tview.NewTextView().SetTextAlign(tview.AlignCenter)

	// Create buttons
	rebuildMainPage := func() {
		BuildMainPage(cfg, pages, app, pl, isRunning, logDestPath, logDestName)
	}

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		BuildSearchMedia(cfg, pages, app)
	})
	writeButton := tview.NewButton("Custom write").SetSelectedFunc(func() {
		BuildTagsWriteMenu(cfg, pages, app)
	})
	updateDBButton := tview.NewButton("Update media").SetSelectedFunc(func() {
		BuildGenerateDBPage(cfg, pages, app)
	})
	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		BuildSettingsMainMenu(cfg, pages, app, pl, rebuildMainPage, logDestPath, logDestName)
	})
	exitButton := tview.NewButton("Exit").SetSelectedFunc(func() {
		notifyCancel()
		app.Stop()
	})

	// Disable buttons when service not running
	disableRow1 := !svcRunning
	if disableRow1 {
		searchButton.SetDisabled(true)
		writeButton.SetDisabled(true)
		updateDBButton.SetDisabled(true)
		settingsButton.SetDisabled(true)
	}

	// Help text for each button
	exitHelpText := "Exit TUI app"
	if svcRunning {
		exitHelpText = "Exit TUI app (service will continue running)"
	}

	// Create button grid (2 rows x 3 cols)
	buttonGrid := NewButtonGrid(app, 3)
	buttonGrid.AddRow(
		&ButtonGridItem{searchButton, "Search for media and write to an NFC tag", disableRow1},
		&ButtonGridItem{writeButton, "Write custom ZapScript to an NFC tag", disableRow1},
		&ButtonGridItem{updateDBButton, "Scan disk to create index of games", disableRow1},
	)
	buttonGrid.AddRow(
		&ButtonGridItem{settingsButton, "Manage settings for Core service", disableRow1},
		nil,
		&ButtonGridItem{exitButton, exitHelpText, false},
	)
	buttonGrid.SetOnHelp(func(text string) {
		helpText.SetText(text)
	})
	buttonGrid.SetOnEscape(func() {
		notifyCancel()
		app.Stop()
	})
	buttonGrid.FocusFirst()

	// Assemble main layout
	main.AddItem(contentFlex, 0, 1, false)
	main.AddItem(helpText, 1, 0, false)
	main.AddItem(buttonGrid, 2, 0, true)

	wrappedMain := NewMainFrame(main)
	pageDefaults(PageMain, pages, wrappedMain)
	return wrappedMain
}

func BuildMain(
	cfg *config.Instance,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) (*tview.Application, error) {
	if err := config.LoadTUIConfig(helpers.ConfigDir(pl), pl.ID()); err != nil {
		log.Warn().Err(err).Msg("failed to load TUI config, using defaults")
	}

	tuiCfg := config.GetTUIConfig()
	if !SetCurrentTheme(tuiCfg.Theme) {
		log.Warn().Str("theme", tuiCfg.Theme).Msg("unknown theme, using default")
		SetCurrentTheme("default")
	}

	app := tview.NewApplication()
	app.EnableMouse(tuiCfg.Mouse)

	pages := tview.NewPages()
	BuildMainPage(cfg, pages, app, pl, isRunning, logDestPath, logDestName)

	var rootWidget tview.Primitive
	if tuiCfg.CRTMode {
		rootWidget = CenterWidget(75, 15, pages)
	} else {
		rootWidget = ResponsiveMaxWidget(DefaultMaxWidth, DefaultMaxHeight, pages)
	}
	return app.SetRoot(rootWidget, true), nil
}
