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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func getMediaState(ctx context.Context, cfg *config.Instance) (models.MediaResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodMedia, "")
	if err != nil {
		return models.MediaResponse{}, fmt.Errorf("failed to get media state from local client: %w", err)
	}
	var tokens models.MediaResponse
	err = json.Unmarshal([]byte(resp), &tokens)
	if err != nil {
		return models.MediaResponse{}, fmt.Errorf("failed to unmarshal media response: %w", err)
	}
	return tokens, nil
}

func waitGenerateUpdate(ctx context.Context, cfg *config.Instance) (models.IndexingStatusResponse, error) {
	resp, err := client.WaitNotification(
		ctx, -1,
		cfg, models.NotificationMediaIndexing,
	)
	if err != nil {
		return models.IndexingStatusResponse{}, fmt.Errorf("failed to wait for notification: %w", err)
	}
	var status models.IndexingStatusResponse
	err = json.Unmarshal([]byte(resp), &status)
	if err != nil {
		return models.IndexingStatusResponse{}, fmt.Errorf("failed to unmarshal indexing status response: %w", err)
	}
	return status, nil
}

type ProgressBar struct {
	*tview.Box
	progress   float64
	emptyRune  rune
	filledRune rune
}

func NewProgressBar() *ProgressBar {
	return &ProgressBar{
		Box:        tview.NewBox(),
		progress:   0,
		emptyRune:  tcell.RuneBoard,
		filledRune: tcell.RuneBlock,
	}
}

func (p *ProgressBar) SetProgress(progress float64) *ProgressBar {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	p.progress = progress
	return p
}

func (p *ProgressBar) GetProgress() float64 {
	return p.progress
}

func (p *ProgressBar) Draw(screen tcell.Screen) {
	p.DrawForSubclass(screen, p)

	x, y, width, height := p.GetInnerRect()

	if height > 0 {
		barWidth := width
		filled := int(float64(barWidth) * p.progress)
		t := CurrentTheme()

		for i := range filled {
			screen.SetContent(x+i, y, p.filledRune, nil, tcell.StyleDefault.Foreground(t.ProgressFillColor))
		}

		for i := filled; i < barWidth; i++ {
			screen.SetContent(x+i, y, p.emptyRune, nil, tcell.StyleDefault.Foreground(t.ProgressEmptyColor))
		}
	}
}

// formatDBStats returns a formatted string showing database statistics.
func formatDBStats(db models.IndexingStatusResponse) string {
	if !db.Exists {
		return "No database found. Run update to scan your media folders."
	}

	mediaCount := 0
	if db.TotalMedia != nil {
		mediaCount = *db.TotalMedia
	}

	return fmt.Sprintf("Database contains %d indexed media files.", mediaCount)
}

// BuildGenerateDBPage creates the media database update page with PageFrame.
func BuildGenerateDBPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Update Media DB")

	goBack := func() {
		cancel()
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	// State components
	progressBar := NewProgressBar()
	progressBar.SetBorder(true)

	progressStatusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Starting scan...")

	dbStatsText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	completeText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	// Internal pages for different states
	statePages := tview.NewPages()

	// === INITIAL STATE ===
	initialContent := tview.NewFlex().SetDirection(tview.FlexRow)
	initialContent.AddItem(nil, 0, 1, false)
	initialContent.AddItem(dbStatsText, 2, 0, false)
	initialContent.AddItem(nil, 0, 1, false)

	// === PROGRESS STATE ===
	progressContent := tview.NewFlex().SetDirection(tview.FlexRow)
	progressTitle := tview.NewTextView().
		SetText("Scanning media files...").
		SetTextAlign(tview.AlignCenter)
	progressContent.AddItem(nil, 0, 1, false)
	progressContent.AddItem(progressTitle, 1, 0, false)
	progressContent.AddItem(progressBar, 3, 0, false)
	progressContent.AddItem(progressStatusText, 1, 0, false)
	progressContent.AddItem(nil, 0, 1, false)

	// === COMPLETE STATE ===
	completeContent := tview.NewFlex().SetDirection(tview.FlexRow)
	completeContent.AddItem(nil, 0, 1, false)
	completeContent.AddItem(completeText, 3, 0, false)
	completeContent.AddItem(nil, 0, 1, false)

	// Add state pages
	statePages.AddPage("initial", initialContent, true, true)
	statePages.AddPage("progress", progressContent, true, false)
	statePages.AddPage("complete", completeContent, true, false)

	// Button bar creation functions (declared first to allow mutual references)
	var createInitialButtonBar, createProgressButtonBar, createCompleteButtonBar func() *ButtonBar

	createProgressButtonBar = func() *ButtonBar {
		bar := NewButtonBar(app)
		bar.AddButton("Hide", goBack)
		bar.SetupNavigation(goBack)
		return bar
	}

	createCompleteButtonBar = func() *ButtonBar {
		bar := NewButtonBar(app)
		bar.AddButton("Done", func() {
			// Refresh stats before going back
			mediaCtx, mediaCancel := tuiContext()
			media, err := getMediaState(mediaCtx, cfg)
			mediaCancel()
			if err == nil {
				dbStatsText.SetText(formatDBStats(media.Database))
			}
			statePages.SwitchToPage("initial")
			frame.SetHelpText("Rescan your media folders to update the index")
			frame.SetButtonBar(createInitialButtonBar())
			frame.FocusButtonBar()
		})
		bar.SetupNavigation(goBack)
		return bar
	}

	createInitialButtonBar = func() *ButtonBar {
		bar := NewButtonBar(app)
		bar.AddButton("Update", func() {
			_, err := client.LocalClient(context.Background(), cfg, models.MethodMediaGenerate, "")
			if err != nil {
				log.Error().Err(err).Msg("error generating media db")
				return
			}
			statePages.SwitchToPage("progress")
			frame.SetHelpText("Scanning media files...")
			frame.SetButtonBar(createProgressButtonBar())
			frame.FocusButtonBar()
		})
		bar.AddButton("Back", goBack)
		bar.SetupNavigation(goBack)
		return bar
	}

	// Update functions
	updateProgress := func(current, total int, status string) {
		app.QueueUpdateDraw(func() {
			progressBar.SetProgress(float64(current) / float64(total))
			progressStatusText.SetText(status)
		})
	}

	showComplete := func(filesFound int) {
		app.QueueUpdateDraw(func() {
			completeText.SetText(fmt.Sprintf("Database update complete!\n\n%d files processed.", filesFound))
			statePages.SwitchToPage("complete")
			frame.SetHelpText("Update finished successfully")
			frame.SetButtonBar(createCompleteButtonBar())
			frame.FocusButtonBar()
		})
	}

	// Check initial state and set stats
	mediaCtx, mediaCancel := tuiContext()
	defer mediaCancel()
	media, err := getMediaState(mediaCtx, cfg)

	switch {
	case err != nil:
		dbStatsText.SetText("Unable to retrieve database status.")
		frame.SetHelpText("Rescan your media folders to update the index")
		statePages.SwitchToPage("initial")
		frame.SetButtonBar(createInitialButtonBar())
	case media.Database.Indexing:
		if media.Database.CurrentStep == nil ||
			media.Database.TotalSteps == nil ||
			media.Database.CurrentStepDisplay == nil {
			dbStatsText.SetText(formatDBStats(media.Database))
			frame.SetHelpText("Rescan your media folders to update the index")
			statePages.SwitchToPage("initial")
			frame.SetButtonBar(createInitialButtonBar())
		} else {
			progressBar.SetProgress(float64(*media.Database.CurrentStep) / float64(*media.Database.TotalSteps))
			progressStatusText.SetText(*media.Database.CurrentStepDisplay)
			statePages.SwitchToPage("progress")
			frame.SetHelpText("Scanning media files...")
			frame.SetButtonBar(createProgressButtonBar())
		}
	default:
		dbStatsText.SetText(formatDBStats(media.Database))
		frame.SetHelpText("Rescan your media folders to update the index")
		statePages.SwitchToPage("initial")
		frame.SetButtonBar(createInitialButtonBar())
	}

	// Background worker for progress updates
	go func() {
		defer cancel()

		var lastUpdate *models.IndexingStatusResponse
		for {
			select {
			case <-ctx.Done():
				return
			default:
				indexing, err := waitGenerateUpdate(ctx, cfg)
				if errors.Is(err, client.ErrRequestTimeout) {
					continue
				} else if err != nil {
					if errors.Is(err, client.ErrRequestCancelled) {
						return
					}
					log.Error().Err(err).Msg("error waiting for indexing update")
					return
				}

				if lastUpdate != nil &&
					lastUpdate.Indexing &&
					!indexing.Indexing &&
					indexing.TotalFiles != nil {
					showComplete(*indexing.TotalFiles)
					updateProgress(0, 1, "")
				} else if indexing.Indexing &&
					indexing.CurrentStep != nil &&
					indexing.TotalSteps != nil &&
					indexing.CurrentStepDisplay != nil {
					updateProgress(
						*indexing.CurrentStep,
						*indexing.TotalSteps,
						*indexing.CurrentStepDisplay,
					)
					app.QueueUpdateDraw(func() {
						statePages.SwitchToPage("progress")
						frame.SetHelpText("Scanning media files...")
						frame.SetButtonBar(createProgressButtonBar())
						frame.FocusButtonBar()
					})
				}
				lastUpdate = &indexing
			}
		}
	}()

	frame.SetContent(statePages)
	pages.AddAndSwitchToPage(PageGenerateDB, frame, true)
	frame.FocusButtonBar()
}
