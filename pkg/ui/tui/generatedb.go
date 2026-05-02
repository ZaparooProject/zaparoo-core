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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	mediaManageInitialPage  = "initial"
	mediaManageSetupPage    = "scrape_setup"
	mediaManageSystemsPage  = "scrape_systems"
	mediaManageProgressPage = "progress"
	mediaManageCompletePage = "complete"
	mediaManageIndex        = "index"
	mediaManageScrape       = "scrape"
	mediaManagePollInterval = 2 * time.Second
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

func getScrapeStatus(ctx context.Context, cfg *config.Instance) (models.ScrapingStatusResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodMediaScrapeStatus, "")
	if err != nil {
		return models.ScrapingStatusResponse{}, fmt.Errorf("failed to get scrape status: %w", err)
	}
	var status models.ScrapingStatusResponse
	if err := json.Unmarshal([]byte(resp), &status); err != nil {
		return models.ScrapingStatusResponse{}, fmt.Errorf("failed to unmarshal scrape status response: %w", err)
	}
	return status, nil
}

func getScrapers(ctx context.Context, cfg *config.Instance) (models.ScrapersResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodScrapers, "")
	if err != nil {
		return models.ScrapersResponse{}, fmt.Errorf("failed to get scrapers: %w", err)
	}
	var scrapers models.ScrapersResponse
	if err := json.Unmarshal([]byte(resp), &scrapers); err != nil {
		return models.ScrapersResponse{}, fmt.Errorf("failed to unmarshal scrapers response: %w", err)
	}
	return scrapers, nil
}

func getSystems(ctx context.Context, cfg *config.Instance) (models.SystemsResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodSystems, "")
	if err != nil {
		return models.SystemsResponse{}, fmt.Errorf("failed to get systems: %w", err)
	}
	var systems models.SystemsResponse
	if err := json.Unmarshal([]byte(resp), &systems); err != nil {
		return models.SystemsResponse{}, fmt.Errorf("failed to unmarshal systems response: %w", err)
	}
	return systems, nil
}

func startMediaIndex(ctx context.Context, cfg *config.Instance) error {
	_, err := client.LocalClient(ctx, cfg, models.MethodMediaGenerate, "")
	if err != nil {
		return fmt.Errorf("failed to start media index update: %w", err)
	}
	return nil
}

func cancelMediaIndex(ctx context.Context, cfg *config.Instance) error {
	_, err := client.LocalClient(ctx, cfg, models.MethodMediaGenerateCancel, "")
	if err != nil {
		return fmt.Errorf("failed to cancel media index update: %w", err)
	}
	return nil
}

func startMediaScrape(ctx context.Context, cfg *config.Instance, params models.MediaScrapeParams) error {
	b, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal scrape params: %w", err)
	}
	_, err = client.LocalClient(ctx, cfg, models.MethodMediaScrape, string(b))
	if err != nil {
		return fmt.Errorf("failed to start media scrape: %w", err)
	}
	return nil
}

func cancelMediaScrape(ctx context.Context, cfg *config.Instance) error {
	_, err := client.LocalClient(ctx, cfg, models.MethodMediaScrapeCancel, "")
	if err != nil {
		return fmt.Errorf("failed to cancel media scrape: %w", err)
	}
	return nil
}

type mediaManageUpdate struct {
	indexing models.IndexingStatusResponse
	method   string
	scraping models.ScrapingStatusResponse
}

func waitMediaManageUpdate(
	ctx context.Context,
	cfg *config.Instance,
) (mediaManageUpdate, error) {
	method, resp, err := client.WaitNotifications(
		ctx, mediaManagePollInterval,
		cfg,
		models.NotificationMediaIndexing,
		models.NotificationMediaScraping,
	)
	if err != nil {
		return mediaManageUpdate{}, fmt.Errorf("failed to wait for notification: %w", err)
	}

	switch method {
	case models.NotificationMediaIndexing:
		var status models.IndexingStatusResponse
		if err := json.Unmarshal([]byte(resp), &status); err != nil {
			return mediaManageUpdate{}, fmt.Errorf("failed to unmarshal indexing status response: %w", err)
		}
		return mediaManageUpdate{method: method, indexing: status}, nil
	case models.NotificationMediaScraping:
		var status models.ScrapingStatusResponse
		if err := json.Unmarshal([]byte(resp), &status); err != nil {
			return mediaManageUpdate{}, fmt.Errorf("failed to unmarshal scraping status response: %w", err)
		}
		return mediaManageUpdate{method: method, scraping: status}, nil
	default:
		return mediaManageUpdate{}, fmt.Errorf("unexpected notification method: %s", method)
	}
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

func formatScrapeStats(status models.ScrapingStatusResponse) string {
	if status.ScraperID == "" && !status.Scraping && !status.Done {
		return "No metadata scrape has run yet. Run a scrape after updating the media index."
	}

	state := "Last metadata scrape"
	if status.Scraping {
		state = "Metadata scrape running"
	}

	return fmt.Sprintf("%s: %d processed, %d matched, %d skipped.",
		state, status.Processed, status.Matched, status.Skipped)
}

func formatScrapeProgress(status models.ScrapingStatusResponse, scraperName string) string {
	parts := []string{}
	if scraperName != "" {
		parts = append(parts, scraperName)
	} else if status.ScraperID != "" {
		parts = append(parts, status.ScraperID)
	}
	if status.SystemID != "" {
		parts = append(parts, status.SystemID)
	}

	prefix := strings.Join(parts, " - ")
	if prefix != "" {
		prefix += "\n"
	}
	if status.Total > 0 {
		return fmt.Sprintf("%s%d / %d processed | %d matched | %d skipped",
			prefix, status.Processed, status.Total, status.Matched, status.Skipped)
	}
	return fmt.Sprintf("%s%d processed | %d matched | %d skipped",
		prefix, status.Processed, status.Matched, status.Skipped)
}

// BuildGenerateDBPage creates the media management page with PageFrame.
func BuildGenerateDBPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel called in goBack
	frame := NewPageFrame(app).
		SetTitle("Manage Media")

	goBack := func() {
		cancel()
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	progressBar := NewProgressBar()
	progressBar.SetBorder(true)

	progressStatusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Starting...")

	completeText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	var refreshInitial, showInitial, refreshScrapeSetup, showScrapeSetup, startScrapeFromSetup func()
	var createCompleteButtonBar, getProgressButtonBar func() *ButtonBar

	statePages := tview.NewPages()
	initialList := NewSettingsList(pages, PageMain).
		SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	scrapeSetupList := NewSettingsList(pages, mediaManageInitialPage).
		SetRebuildPrevious(func() {
			showInitial()
		}).
		SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	scrapeState := struct {
		scrapers     []models.ScraperInfo
		systems      []SystemItem
		selected     []string
		scraperIndex int
		force        bool
	}{}

	operationState := struct {
		operation string
		mu        syncutil.Mutex
	}{}
	cancelledState := struct {
		operation string
		mu        syncutil.Mutex
	}{}
	setOperation := func(operation string) {
		operationState.mu.Lock()
		defer operationState.mu.Unlock()
		operationState.operation = operation
	}
	getOperation := func() string {
		operationState.mu.Lock()
		defer operationState.mu.Unlock()
		return operationState.operation
	}
	setCancelled := func(operation string) {
		cancelledState.mu.Lock()
		defer cancelledState.mu.Unlock()
		cancelledState.operation = operation
	}
	consumeCancelled := func(operation string) bool {
		cancelledState.mu.Lock()
		defer cancelledState.mu.Unlock()
		if cancelledState.operation != operation {
			return false
		}
		cancelledState.operation = ""
		return true
	}

	showError := func(err error) {
		ShowErrorModal(pages, app, err.Error(), func() {
			app.SetFocus(frame)
		})
	}

	progressContent := tview.NewFlex().SetDirection(tview.FlexRow)
	progressTitle := tview.NewTextView().
		SetText("Working...").
		SetTextAlign(tview.AlignCenter)
	progressContent.AddItem(nil, 0, 1, false)
	progressContent.AddItem(progressTitle, 1, 0, false)
	progressContent.AddItem(progressBar, 3, 0, false)
	progressContent.AddItem(progressStatusText, 3, 0, false)
	progressContent.AddItem(nil, 0, 1, false)

	completeContent := tview.NewFlex().SetDirection(tview.FlexRow)
	completeContent.AddItem(nil, 0, 1, false)
	completeContent.AddItem(completeText, 3, 0, false)
	completeContent.AddItem(nil, 0, 1, false)

	statePages.AddPage(mediaManageInitialPage, initialList.List, true, true)
	statePages.AddPage(mediaManageSetupPage, scrapeSetupList.List, true, false)
	statePages.AddPage(mediaManageProgressPage, progressContent, true, false)
	statePages.AddPage(mediaManageCompletePage, completeContent, true, false)

	var progressButtonBar *ButtonBar
	getProgressButtonBar = func() *ButtonBar {
		if progressButtonBar != nil {
			return progressButtonBar
		}
		bar := NewButtonBar(app)
		bar.AddButton("Cancel", func() {
			ShowConfirmModal(pages, app, "Cancel the current media operation?", func() {
				cancelCtx, cancelReq := tuiContext()
				defer cancelReq()

				var err error
				operation := getOperation()
				switch operation {
				case mediaManageIndex:
					err = cancelMediaIndex(cancelCtx, cfg)
				case mediaManageScrape:
					err = cancelMediaScrape(cancelCtx, cfg)
				}
				if err != nil {
					log.Warn().Err(err).Msg("error cancelling media operation")
					showError(err)
					return
				}
				setCancelled(operation)
				setOperation("")
				completeText.SetText("Media operation cancelled.")
				statePages.SwitchToPage(mediaManageCompletePage)
				frame.SetHelpText("Operation cancelled")
				frame.SetButtonBar(createCompleteButtonBar())
				frame.FocusButtonBar()
			}, func() {
				frame.FocusButtonBar()
			})
		})
		bar.AddButton("Hide", goBack)
		bar.SetupNavigation(goBack)
		progressButtonBar = bar
		return progressButtonBar
	}

	createCompleteButtonBar = func() *ButtonBar {
		bar := NewButtonBar(app)
		bar.AddButton("Done", func() {
			showInitial()
		})
		bar.SetupNavigation(goBack)
		return bar
	}

	refreshInitial = func() {
		initialList.ClearItems()

		mediaCtx, mediaCancel := tuiContext()
		media, mediaErr := getMediaState(mediaCtx, cfg)
		mediaCancel()

		scrapeCtx, scrapeCancel := tuiContext()
		scrapeStatus, scrapeErr := getScrapeStatus(scrapeCtx, cfg)
		scrapeCancel()

		dbStats := "Unable to retrieve database status."
		if mediaErr == nil {
			dbStats = formatDBStats(media.Database)
		}
		scrapeStats := "Unable to retrieve metadata scrape status."
		if scrapeErr == nil {
			scrapeStats = formatScrapeStats(scrapeStatus)
		}

		initialList.AddAction("Update index", dbStats, func() {
			startCtx, startCancel := tuiContext()
			err := startMediaIndex(startCtx, cfg)
			startCancel()
			if err != nil {
				log.Warn().Err(err).Msg("error generating media db")
				showError(err)
				return
			}
			setOperation(mediaManageIndex)
			setCancelled("")
			progressTitle.SetText("Updating media index...")
			progressBar.SetProgress(0)
			progressStatusText.SetText("Starting scan...")
			statePages.SwitchToPage(mediaManageProgressPage)
			frame.SetHelpText("Scanning media files. Cancel stops the active index update.")
			frame.SetButtonBar(getProgressButtonBar())
			frame.FocusButtonBar()
		})

		initialList.AddNavAction("Scrape metadata", scrapeStats, showScrapeSetup)
		initialList.AddAction("Back", "Return to the main menu", goBack)
		initialList.TriggerInitialHelp()
	}

	showInitial = func() {
		setOperation("")
		frame.SetHelpText("Update the media index or scrape metadata for indexed games")
		refreshInitial()
		statePages.SwitchToPage(mediaManageInitialPage)
		frame.SetButtonBar(nil)
		app.SetFocus(initialList.List)
	}

	refreshScrapeSetup = func() {
		scrapeSetupList.ClearItems()
		if len(scrapeState.scrapers) == 0 {
			scrapeSetupList.AddAction("No scrapers available", "No metadata scrapers are available", func() {})
			scrapeSetupList.TriggerInitialHelp()
			return
		}

		scraperNames := make([]string, len(scrapeState.scrapers))
		for i, scraper := range scrapeState.scrapers {
			scraperNames[i] = scraper.Name
		}
		systemsLabel := "All systems"
		if len(scrapeState.selected) > 0 {
			systemsLabel = strings.Join(scrapeState.selected, ", ")
		}

		scrapeSetupList.AddCycle(
			"Scraper",
			"Choose the metadata scraper to run",
			scraperNames,
			&scrapeState.scraperIndex,
			func(_ string, _ int) {})
		scrapeSetupList.AddNavAction(
			"Systems: "+systemsLabel,
			"Select target systems. Leave empty to scrape all supported systems",
			func() {
				selector := NewSystemSelector(&SystemSelectorConfig{
					Systems:  scrapeState.systems,
					Selected: scrapeState.selected,
					Mode:     SystemSelectorMulti,
					OnMulti: func(selected []string) {
						scrapeState.selected = selected
					},
				})
				selector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyEscape {
						refreshScrapeSetup()
						statePages.RemovePage(mediaManageSystemsPage)
						statePages.SwitchToPage(mediaManageSetupPage)
						app.SetFocus(scrapeSetupList.List)
						return nil
					}
					return event
				})
				statePages.RemovePage(mediaManageSystemsPage)
				statePages.AddPage(mediaManageSystemsPage, selector, true, true)
				frame.SetHelpText("Select systems to scrape. Leave all unchecked to scrape all supported systems.")
				app.SetFocus(selector)
			})
		scrapeSetupList.AddToggle(
			"Re-scrape already scraped games",
			"Scrape games even if metadata already exists",
			&scrapeState.force,
			func(_ bool) {})
		scrapeSetupList.AddAction(
			"Start scrape",
			"Start scraping metadata with the selected options",
			startScrapeFromSetup,
		)
		scrapeSetupList.AddBackWithDesc("Return to Manage Media")
		scrapeSetupList.TriggerInitialHelp()
	}

	showScrapeSetup = func() {
		loadCtx, loadCancel := tuiContext()
		scrapers, err := getScrapers(loadCtx, cfg)
		loadCancel()
		if err != nil {
			showError(err)
			return
		}
		if len(scrapers.Scrapers) == 0 {
			showError(errors.New("no metadata scrapers are available"))
			return
		}

		loadCtx, loadCancel = tuiContext()
		systems, err := getSystems(loadCtx, cfg)
		loadCancel()
		if err != nil {
			showError(err)
			return
		}

		scrapeState.scrapers = scrapers.Scrapers
		scrapeState.scraperIndex = 0
		scrapeState.force = false
		scrapeState.selected = nil
		scrapeState.systems = make([]SystemItem, 0, len(systems.Systems))
		for _, system := range systems.Systems {
			if system.ID == "" {
				continue
			}
			name := system.Name
			if name == "" {
				name = system.ID
			}
			scrapeState.systems = append(scrapeState.systems, SystemItem{ID: system.ID, Name: name})
		}

		frame.SetHelpText("Configure and start a metadata scrape")
		refreshScrapeSetup()
		statePages.SwitchToPage(mediaManageSetupPage)
		frame.SetButtonBar(nil)
		app.SetFocus(scrapeSetupList.List)
	}

	startScrapeFromSetup = func() {
		if len(scrapeState.scrapers) == 0 {
			showError(errors.New("no metadata scrapers are available"))
			return
		}
		scraper := scrapeState.scrapers[scrapeState.scraperIndex]
		params := models.MediaScrapeParams{
			ScraperID: scraper.ID,
			Systems:   scrapeState.selected,
			Force:     scrapeState.force,
		}

		startCtx, startCancel := tuiContext()
		err := startMediaScrape(startCtx, cfg, params)
		startCancel()
		if err != nil {
			log.Warn().Err(err).Msg("error starting media scrape")
			showError(err)
			return
		}

		setOperation(mediaManageScrape)
		setCancelled("")
		progressTitle.SetText("Scraping metadata...")
		progressBar.SetProgress(0)
		progressStatusText.SetText(formatScrapeProgress(models.ScrapingStatusResponse{
			ScraperID: scraper.ID,
			Scraping:  true,
		}, scraper.Name))
		statePages.SwitchToPage(mediaManageProgressPage)
		frame.SetHelpText("Scraping metadata. Cancel stops the active scrape.")
		frame.SetButtonBar(getProgressButtonBar())
		frame.FocusButtonBar()
	}

	updateIndexProgress := func(current, total int, status string) {
		app.QueueUpdateDraw(func() {
			progressTitle.SetText("Updating media index...")
			if total > 0 {
				progressBar.SetProgress(float64(current) / float64(total))
			} else {
				progressBar.SetProgress(0)
			}
			progressStatusText.SetText(status)
		})
	}

	updateScrapeProgress := func(status models.ScrapingStatusResponse) {
		app.QueueUpdateDraw(func() {
			progressTitle.SetText("Scraping metadata...")
			if status.Total > 0 {
				progressBar.SetProgress(float64(status.Processed) / float64(status.Total))
			} else {
				progressBar.SetProgress(0)
			}
			scraperName := ""
			for _, scraper := range scrapeState.scrapers {
				if scraper.ID == status.ScraperID {
					scraperName = scraper.Name
					break
				}
			}
			progressStatusText.SetText(formatScrapeProgress(status, scraperName))
		})
	}

	showIndexComplete := func(filesFound int) {
		setOperation("")
		app.QueueUpdateDraw(func() {
			completeText.SetText(fmt.Sprintf("Database update complete!\n\n%d files processed.", filesFound))
			statePages.SwitchToPage(mediaManageCompletePage)
			frame.SetHelpText("Update finished successfully")
			frame.SetButtonBar(createCompleteButtonBar())
			frame.FocusButtonBar()
		})
	}

	showScrapeComplete := func(status models.ScrapingStatusResponse) {
		setOperation("")
		app.QueueUpdateDraw(func() {
			completeText.SetText(fmt.Sprintf(
				"Metadata scrape complete!\n\n%d processed, %d matched, %d skipped.",
				status.Processed,
				status.Matched,
				status.Skipped,
			))
			statePages.SwitchToPage(mediaManageCompletePage)
			frame.SetHelpText("Scrape finished successfully")
			frame.SetButtonBar(createCompleteButtonBar())
			frame.FocusButtonBar()
		})
	}

	pollOperationStatus := func() {
		switch getOperation() {
		case mediaManageIndex:
			mediaCtx, mediaCancel := tuiContext()
			media, err := getMediaState(mediaCtx, cfg)
			mediaCancel()
			if err != nil || media.Database.Indexing {
				return
			}
			if consumeCancelled(mediaManageIndex) {
				return
			}
			if media.Database.TotalFiles != nil {
				showIndexComplete(*media.Database.TotalFiles)
			}
		case mediaManageScrape:
			scrapeCtx, scrapeCancel := tuiContext()
			status, err := getScrapeStatus(scrapeCtx, cfg)
			scrapeCancel()
			if err != nil || status.Scraping || !status.Done {
				return
			}
			if consumeCancelled(mediaManageScrape) {
				return
			}
			showScrapeComplete(status)
		}
	}

	mediaCtx, mediaCancel := tuiContext()
	defer mediaCancel()
	media, err := getMediaState(mediaCtx, cfg)
	scrapeCtx, scrapeCancel := tuiContext()
	scrapeStatus, scrapeErr := getScrapeStatus(scrapeCtx, cfg)
	scrapeCancel()

	switch {
	case err != nil:
		showInitial()
	case media.Database.Indexing:
		if media.Database.CurrentStep == nil ||
			media.Database.TotalSteps == nil ||
			media.Database.CurrentStepDisplay == nil {
			showInitial()
		} else {
			setOperation(mediaManageIndex)
			setCancelled("")
			progressTitle.SetText("Updating media index...")
			progressBar.SetProgress(float64(*media.Database.CurrentStep) / float64(*media.Database.TotalSteps))
			progressStatusText.SetText(*media.Database.CurrentStepDisplay)
			statePages.SwitchToPage(mediaManageProgressPage)
			frame.SetHelpText("Scanning media files. Cancel stops the active index update.")
			frame.SetButtonBar(getProgressButtonBar())
			frame.FocusButtonBar()
		}
	case scrapeErr == nil && scrapeStatus.Scraping:
		setOperation(mediaManageScrape)
		setCancelled("")
		progressTitle.SetText("Scraping metadata...")
		if scrapeStatus.Total > 0 {
			progressBar.SetProgress(float64(scrapeStatus.Processed) / float64(scrapeStatus.Total))
		}
		progressStatusText.SetText(formatScrapeProgress(scrapeStatus, ""))
		statePages.SwitchToPage(mediaManageProgressPage)
		frame.SetHelpText("Scraping metadata. Cancel stops the active scrape.")
		frame.SetButtonBar(getProgressButtonBar())
		frame.FocusButtonBar()
	default:
		showInitial()
	}

	go func() {
		defer cancel()

		var lastUpdate *models.IndexingStatusResponse
		var lastScrape *models.ScrapingStatusResponse
		for {
			select {
			case <-ctx.Done():
				return
			default:
				update, err := waitMediaManageUpdate(ctx, cfg)
				if errors.Is(err, client.ErrRequestTimeout) {
					pollOperationStatus()
					continue
				} else if err != nil {
					if errors.Is(err, client.ErrRequestCancelled) {
						return
					}
					log.Warn().Err(err).Msg("error waiting for indexing update")
					return
				}

				switch update.method {
				case models.NotificationMediaIndexing:
					indexing := update.indexing
					if ((lastUpdate != nil && lastUpdate.Indexing) || getOperation() == mediaManageIndex) &&
						!indexing.Indexing && indexing.TotalFiles != nil {
						if consumeCancelled(mediaManageIndex) {
							lastUpdate = &indexing
							continue
						}
						showIndexComplete(*indexing.TotalFiles)
						updateIndexProgress(0, 1, "")
					} else if indexing.Indexing &&
						indexing.CurrentStep != nil &&
						indexing.TotalSteps != nil &&
						indexing.CurrentStepDisplay != nil {
						setOperation(mediaManageIndex)
						updateIndexProgress(
							*indexing.CurrentStep,
							*indexing.TotalSteps,
							*indexing.CurrentStepDisplay,
						)
						app.QueueUpdateDraw(func() {
							statePages.SwitchToPage(mediaManageProgressPage)
							frame.SetHelpText("Scanning media files. Cancel stops the active index update.")
							frame.SetButtonBar(getProgressButtonBar())
						})
					}
					lastUpdate = &indexing
				case models.NotificationMediaScraping:
					scraping := update.scraping
					if ((lastScrape != nil && lastScrape.Scraping) || getOperation() == mediaManageScrape) &&
						!scraping.Scraping && scraping.Done {
						if consumeCancelled(mediaManageScrape) {
							lastScrape = &scraping
							continue
						}
						showScrapeComplete(scraping)
					} else if scraping.Scraping {
						setOperation(mediaManageScrape)
						updateScrapeProgress(scraping)
						app.QueueUpdateDraw(func() {
							statePages.SwitchToPage(mediaManageProgressPage)
							frame.SetHelpText("Scraping metadata. Cancel stops the active scrape.")
							frame.SetButtonBar(getProgressButtonBar())
						})
					}
					lastScrape = &scraping
				}
			}
		}
	}()

	frame.SetContent(statePages)
	pages.AddAndSwitchToPage(PageGenerateDB, frame, true)
	if getOperation() != "" {
		frame.FocusButtonBar()
	} else {
		app.SetFocus(initialList.List)
	}
}
