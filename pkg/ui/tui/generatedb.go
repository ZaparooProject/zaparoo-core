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

		for i := range filled {
			screen.SetContent(x+i, y, p.filledRune, nil, tcell.StyleDefault.Foreground(tcell.ColorGreen))
		}

		for i := filled; i < barWidth; i++ {
			screen.SetContent(x+i, y, p.emptyRune, nil, tcell.StyleDefault.Foreground(tcell.ColorGray))
		}
	}
}

func initStatePage(
	cfg *config.Instance,
	app *tview.Application,
	appPages *tview.Pages,
	parentPages *tview.Pages,
	cancel context.CancelFunc,
) tview.Primitive {
	initialState := tview.NewFlex().SetDirection(tview.FlexRow)
	explanationText := tview.NewTextView().
		SetText("Update Core's internal database of media files.").
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	buttonFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn)

	startButton := tview.NewButton("Update")

	backButton := tview.NewButton("Go back").
		SetSelectedFunc(func() {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
		})

	backButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyRight || k == tcell.KeyLeft || k == tcell.KeyTab || k == tcell.KeyBacktab {
			app.SetFocus(startButton)
			return nil
		}
		return event
	})
	startButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyRight || k == tcell.KeyLeft || k == tcell.KeyTab || k == tcell.KeyBacktab {
			app.SetFocus(backButton)
			return nil
		}
		return event
	})

	startButton.SetSelectedFunc(func() {
		_, err := client.LocalClient(context.Background(), cfg, models.MethodMediaGenerate, "")
		if err != nil {
			log.Error().Err(err).Msg("error generating media db")
			return
		}
		parentPages.SwitchToPage("progress")
	})

	buttonFlex.AddItem(nil, 0, 1, false)
	buttonFlex.AddItem(startButton, 0, 1, true)
	buttonFlex.AddItem(nil, 1, 0, false)
	buttonFlex.AddItem(backButton, 0, 1, false)
	buttonFlex.AddItem(nil, 0, 1, false)

	initialState.AddItem(nil, 0, 1, false)
	initialState.AddItem(explanationText, 0, 2, false)
	initialState.AddItem(buttonFlex, 1, 1, true)
	initialState.AddItem(nil, 0, 1, false)

	initialState.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	return initialState
}

func progressStatePage(
	_ *tview.Application,
	appPages *tview.Pages,
	_ *tview.Pages,
	cancel context.CancelFunc,
) (tview.Primitive, *ProgressBar, *tview.TextView) {
	progressState := tview.NewFlex().SetDirection(tview.FlexRow)
	progressText := tview.NewTextView().
		SetText("Scanning media files...").
		SetTextAlign(tview.AlignCenter)

	progress := NewProgressBar()
	progress.SetBorder(true)

	statusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Starting scan...")

	hideButton := tview.NewButton("Hide").
		SetSelectedFunc(func() {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
		})

	progressState.AddItem(nil, 0, 1, false)
	progressState.AddItem(progressText, 2, 0, false)
	progressState.AddItem(nil, 1, 0, false)
	progressState.AddItem(progress, 3, 0, false)
	progressState.AddItem(nil, 1, 0, false)
	progressState.AddItem(statusText, 2, 0, false)
	progressState.AddItem(nil, 1, 0, false)

	buttonFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(hideButton, 0, 1, true).
		AddItem(nil, 0, 1, false)
	progressState.AddItem(buttonFlex, 1, 0, true)

	progressState.AddItem(nil, 0, 1, false)

	layout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 5, 0, false).
		AddItem(progressState, 0, 1, true).
		AddItem(nil, 5, 0, false)

	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	return layout, progress, statusText
}

func completeStatePage(
	_ *tview.Application,
	appPages *tview.Pages,
	parentPages *tview.Pages,
	cancel context.CancelFunc,
) (tview.Primitive, *tview.TextView) {
	completeState := tview.NewFlex().SetDirection(tview.FlexRow)
	completeText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	doneButton := tview.NewButton("Done").
		SetSelectedFunc(func() {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
			parentPages.SwitchToPage("initial")
		})

	completeState.AddItem(nil, 0, 1, false)
	completeState.AddItem(completeText, 0, 2, false)
	completeState.AddItem(nil, 1, 0, false)

	buttonFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(doneButton, 0, 1, true).
		AddItem(nil, 0, 1, false)
	completeState.AddItem(buttonFlex, 1, 0, true)

	completeState.AddItem(nil, 0, 1, false)

	layout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 5, 0, false).
		AddItem(completeState, 0, 1, true).
		AddItem(nil, 5, 0, false)

	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			cancel() // Cancel the goroutine
			appPages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	return layout, completeText
}

func errorStatePage(
	_ *tview.Application,
	appPages *tview.Pages,
	_ *tview.Pages,
	cancel context.CancelFunc,
	message string,
) tview.Primitive {
	errorState := tview.NewFlex().SetDirection(tview.FlexRow)
	errorText := tview.NewTextView().
		SetText(message).
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	backButton := tview.NewButton("Go back").
		SetSelectedFunc(func() {
			cancel()
			appPages.SwitchToPage(PageMain)
		})

	buttonFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	buttonFlex.AddItem(nil, 0, 1, false)
	buttonFlex.AddItem(backButton, 0, 1, true)
	buttonFlex.AddItem(nil, 0, 1, false)

	errorState.AddItem(nil, 0, 1, false)
	errorState.AddItem(errorText, 0, 2, false)
	errorState.AddItem(buttonFlex, 1, 1, true)
	errorState.AddItem(nil, 0, 1, false)

	errorState.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			cancel()
			appPages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	return errorState
}

func BuildGenerateDBPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
) tview.Primitive {
	// Create a cancellable context for the goroutine
	ctx, cancel := context.WithCancel(context.Background())

	generateDB := tview.NewPages()
	generateDB.SetTitle("Update Media DB")
	generateDB.SetBorder(true)

	progressState, progressBar, statusText := progressStatePage(app, pages, generateDB, cancel)
	generateDB.AddPage("progress", progressState, true, false)

	initialState := initStatePage(cfg, app, pages, generateDB, cancel)
	generateDB.AddPage("initial", initialState, true, false)

	completeState, completeText := completeStatePage(app, pages, generateDB, cancel)
	generateDB.AddPage("complete", completeState, true, false)

	errorMsg := "Could not connect to Core service.\nPlease ensure it is running."
	errorPage := errorStatePage(app, pages, generateDB, cancel, errorMsg)
	generateDB.AddPage("error", errorPage, true, false)

	updateProgress := func(current, total int, status string) {
		app.QueueUpdateDraw(func() {
			progressBar.SetProgress(float64(current) / float64(total))
			statusText.SetText(status)
		})
	}

	showComplete := func(filesFound int) {
		app.QueueUpdateDraw(func() {
			completeText.SetText(fmt.Sprintf("Database update complete!\n%d files processed.", filesFound))
			generateDB.SwitchToPage("complete")
		})
	}

	mediaCtx, mediaCancel := tuiContext()
	defer mediaCancel()
	media, err := getMediaState(mediaCtx, cfg)
	switch {
	case err != nil:
		log.Debug().Err(err).Msg("failed to get media state")
		generateDB.SwitchToPage("error")
	case media.Database.Indexing:
		// Check for nil pointers before dereferencing
		if media.Database.CurrentStep == nil ||
			media.Database.TotalSteps == nil ||
			media.Database.CurrentStepDisplay == nil {
			generateDB.SwitchToPage("initial")
		} else {
			// Set progress directly to avoid nested QueueUpdateDraw calls
			progressBar.SetProgress(float64(*media.Database.CurrentStep) / float64(*media.Database.TotalSteps))
			statusText.SetText(*media.Database.CurrentStepDisplay)
			generateDB.SwitchToPage("progress")
		}
	default:
		generateDB.SwitchToPage("initial")
	}

	go func() {
		defer cancel() // Ensure cleanup on goroutine exit

		// Continue with normal notification loop
		var lastUpdate *models.IndexingStatusResponse
		for {
			select {
			case <-ctx.Done():
				// Context cancelled, clean exit
				return
			default:
				indexing, err := waitGenerateUpdate(ctx, cfg)
				if errors.Is(err, client.ErrRequestTimeout) {
					continue
				} else if err != nil {
					if errors.Is(err, client.ErrRequestCancelled) {
						// Context cancelled during request, clean exit
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
					// Switch to progress page if we're not already there
					app.QueueUpdateDraw(func() {
						generateDB.SwitchToPage("progress")
					})
				}
				lastUpdate = &indexing
			}
		}
	}()

	pageDefaults(PageGenerateDB, pages, generateDB)
	return generateDB
}
