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
	mediaManageLoadingPage  = "loading"
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

func resumeMediaIndex(ctx context.Context, cfg *config.Instance) error {
	_, err := client.LocalClient(ctx, cfg, models.MethodMediaGenerateResume, "")
	if err != nil {
		return fmt.Errorf("failed to resume media index update: %w", err)
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

func resumeMediaScrape(ctx context.Context, cfg *config.Instance) error {
	_, err := client.LocalClient(ctx, cfg, models.MethodMediaScrapeResume, "")
	if err != nil {
		return fmt.Errorf("failed to resume media scrape: %w", err)
	}
	return nil
}

type mediaManageUpdate struct {
	indexing models.IndexingStatusResponse
	method   string
	scraping models.ScrapingStatusResponse
}

type mediaManageInitialState struct {
	media        models.MediaResponse
	mediaErr     error
	scrapeStatus models.ScrapingStatusResponse
	scrapeErr    error
}

func loadMediaManageInitialState(cfg *config.Instance) mediaManageInitialState {
	type mediaResult struct {
		media models.MediaResponse
		err   error
	}
	type scrapeResult struct {
		status models.ScrapingStatusResponse
		err    error
	}

	mediaCh := make(chan mediaResult, 1)
	scrapeCh := make(chan scrapeResult, 1)

	go func() {
		ctx, cancel := tuiContext()
		defer cancel()
		media, err := getMediaState(ctx, cfg)
		mediaCh <- mediaResult{media: media, err: err}
	}()
	go func() {
		ctx, cancel := tuiContext()
		defer cancel()
		status, err := getScrapeStatus(ctx, cfg)
		scrapeCh <- scrapeResult{status: status, err: err}
	}()

	media := <-mediaCh
	scrape := <-scrapeCh
	return mediaManageInitialState{
		media:        media.media,
		mediaErr:     media.err,
		scrapeStatus: scrape.status,
		scrapeErr:    scrape.err,
	}
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

func formatDBMenuLabel(db models.IndexingStatusResponse) string {
	if db.Indexing {
		if db.Paused {
			return "Update index: paused"
		}
		return "Update index: in progress"
	}
	if !db.Exists {
		return "Update index: not indexed"
	}

	mediaCount := 0
	if db.TotalMedia != nil {
		mediaCount = *db.TotalMedia
	}
	return fmt.Sprintf("Update index: %d media", mediaCount)
}

func mediaIndexBlockedByScrapeLabel() string {
	return "Update index: scrape running"
}

func mediaScrapeBlockedByIndexLabel() string {
	return "Scrape metadata: index running"
}

func scraperSupportsSystem(scraper models.ScraperInfo, systemID string) bool {
	if len(scraper.SupportedSystems) == 0 {
		return true
	}
	for _, supported := range scraper.SupportedSystems {
		if strings.EqualFold(supported, systemID) {
			return true
		}
	}
	return false
}

func filterScrapeSystems(allSystems []SystemItem, scraper models.ScraperInfo) []SystemItem {
	filtered := make([]SystemItem, 0, len(allSystems))
	for _, system := range allSystems {
		if scraperSupportsSystem(scraper, system.ID) {
			filtered = append(filtered, system)
		}
	}
	return filtered
}

func pruneSelectedScrapeSystems(selected []string, systems []SystemItem) []string {
	if len(selected) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(systems))
	for _, system := range systems {
		allowed[system.ID] = struct{}{}
	}
	pruned := make([]string, 0, len(selected))
	for _, systemID := range selected {
		if _, ok := allowed[systemID]; ok {
			pruned = append(pruned, systemID)
		}
	}
	return pruned
}

func formatScrapeSystemsLabel(selected []string, systems []SystemItem) string {
	if len(systems) == 0 {
		return "No supported systems"
	}
	if len(selected) > 0 {
		return strings.Join(selected, ", ")
	}
	return "All systems"
}

func formatScrapeMenuLabel(status *models.ScrapingStatusResponse) string {
	if status.Scraping {
		if status.Paused {
			return "Scrape metadata: paused"
		}
		return "Scrape metadata: in progress"
	}
	if status.ScraperID == "" && !status.Scraping && !status.Done {
		if status.TotalScraped > 0 {
			return fmt.Sprintf("Scrape metadata: %d scraped", status.TotalScraped)
		}
		return "Scrape metadata"
	}
	if status.TotalScraped > 0 {
		return fmt.Sprintf("Scrape metadata: %d scraped", status.TotalScraped)
	}
	return fmt.Sprintf("Scrape metadata: %d matched", status.Matched)
}

func formatScrapeProgress(status *models.ScrapingStatusResponse, scraperName string) string {
	parts := []string{}
	if scraperName != "" {
		parts = append(parts, scraperName)
	} else if status.ScraperID != "" {
		parts = append(parts, status.ScraperID)
	}
	if status.CurrentSystem != nil && status.CurrentSystem.SystemName != "" {
		parts = append(parts, status.CurrentSystem.SystemName)
	} else if status.CurrentSystem != nil && status.CurrentSystem.SystemID != "" {
		parts = append(parts, status.CurrentSystem.SystemID)
	} else if status.SystemID != "" {
		parts = append(parts, status.SystemID)
	}

	lines := make([]string, 0, 4)
	if prefix := strings.Join(parts, " - "); prefix != "" {
		lines = append(lines, prefix)
	}
	if status.CurrentStep != nil && status.TotalSteps != nil {
		overall := fmt.Sprintf("Overall: %d / %d", *status.CurrentStep, *status.TotalSteps)
		if status.CurrentStepDisplay != nil && *status.CurrentStepDisplay != "" {
			overall = fmt.Sprintf("Overall: %s (%d / %d)",
				*status.CurrentStepDisplay, *status.CurrentStep, *status.TotalSteps)
		}
		lines = append(lines, overall)
	}

	processed := status.Processed
	total := status.Total
	matched := status.Matched
	skipped := status.Skipped
	if status.CurrentSystem != nil {
		processed = status.CurrentSystem.Processed
		total = status.CurrentSystem.Total
		matched = status.CurrentSystem.Matched
		skipped = status.CurrentSystem.Skipped
	}
	progress := "Records"
	if status.Paused {
		progress = "Paused"
	}
	if total > 0 {
		lines = append(lines, fmt.Sprintf("%s: %d / %d", progress, processed, total))
	} else if status.Paused {
		lines = append(lines, "Paused")
	} else {
		lines = append(lines, fmt.Sprintf("Records: %d processed", processed))
	}
	lines = append(lines, fmt.Sprintf("Matched: %d  Skipped: %d", matched, skipped))
	return strings.Join(lines, "\n")
}

func mediaIndexProgress(current, total int) float64 {
	if total <= 0 || current <= 0 {
		return 0
	}
	return float64(current) / float64(total)
}

func scrapeOverallProgress(status models.ScrapingStatusResponse) float64 {
	if status.CurrentStep != nil && status.TotalSteps != nil {
		return mediaIndexProgress(*status.CurrentStep, *status.TotalSteps)
	}
	return mediaIndexProgress(status.Processed, status.Total)
}

func scrapeCurrentSystemProgress(status models.ScrapingStatusResponse) float64 {
	if status.CurrentSystem != nil {
		return mediaIndexProgress(status.CurrentSystem.Processed, status.CurrentSystem.Total)
	}
	return mediaIndexProgress(status.Processed, status.Total)
}

func handleMediaManageEscape(
	statePages *tview.Pages,
	goBack func(),
	showInitial func(),
	closeSystemsPage func(),
) {
	frontPage, _ := statePages.GetFrontPage()
	switch frontPage {
	case mediaManageSystemsPage:
		closeSystemsPage()
	case mediaManageSetupPage, mediaManageProgressPage:
		showInitial()
	case mediaManageCompletePage, mediaManageInitialPage, mediaManageLoadingPage:
		goBack()
	default:
		goBack()
	}
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

	progressBar := NewProgressBar()
	progressBar.SetBorder(true)
	systemProgressBar := NewProgressBar()
	systemProgressBar.SetBorder(true)

	progressStatusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Starting...")

	completeText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	var renderInitial func(mediaManageInitialState)
	var showInitial, refreshScrapeSetup, showScrapeSetup, startScrapeFromSetup func()
	var closeScrapeSystemsPage func()
	var showIndexProgress func(models.IndexingStatusResponse)
	var showScrapeProgress func(models.ScrapingStatusResponse)
	var createCompleteButtonBar func() *ButtonBar
	var createProgressButtonBar func(paused bool) *ButtonBar
	var setProgressButtonBar func(paused bool)
	var progressButtonBarPaused *bool

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
		allSystems   []SystemItem
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
	progressVisibleState := struct {
		visible bool
		mu      syncutil.Mutex
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
	setProgressVisible := func(visible bool) {
		progressVisibleState.mu.Lock()
		defer progressVisibleState.mu.Unlock()
		progressVisibleState.visible = visible
	}
	isProgressVisible := func() bool {
		progressVisibleState.mu.Lock()
		defer progressVisibleState.mu.Unlock()
		return progressVisibleState.visible
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
	showIndexProgressLayout := func() {
		progressContent.Clear()
		progressContent.AddItem(nil, 0, 1, false)
		progressContent.AddItem(progressTitle, 1, 0, false)
		progressContent.AddItem(progressBar, 3, 0, false)
		progressContent.AddItem(progressStatusText, 3, 0, false)
		progressContent.AddItem(nil, 0, 1, false)
	}
	showScrapeProgressLayout := func() {
		progressContent.Clear()
		progressContent.AddItem(nil, 0, 1, false)
		progressContent.AddItem(progressTitle, 1, 0, false)
		progressContent.AddItem(progressBar, 3, 0, false)
		progressContent.AddItem(systemProgressBar, 3, 0, false)
		progressContent.AddItem(progressStatusText, 4, 0, false)
		progressContent.AddItem(nil, 0, 1, false)
	}
	showIndexProgressLayout()

	completeContent := tview.NewFlex().SetDirection(tview.FlexRow)
	completeContent.AddItem(nil, 0, 1, false)
	completeContent.AddItem(completeText, 3, 0, false)
	completeContent.AddItem(nil, 0, 1, false)

	loadingText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Loading media status...")
	loadingContent := tview.NewFlex().SetDirection(tview.FlexRow)
	loadingContent.AddItem(nil, 0, 1, false)
	loadingContent.AddItem(loadingText, 1, 0, false)
	loadingContent.AddItem(nil, 0, 1, false)

	statePages.AddPage(mediaManageLoadingPage, loadingContent, true, true)
	statePages.AddPage(mediaManageInitialPage, initialList.List, true, false)
	statePages.AddPage(mediaManageSetupPage, scrapeSetupList.List, true, false)
	statePages.AddPage(mediaManageProgressPage, progressContent, true, false)
	statePages.AddPage(mediaManageCompletePage, completeContent, true, false)

	closeScrapeSystemsPage = func() {
		refreshScrapeSetup()
		statePages.RemovePage(mediaManageSystemsPage)
		statePages.SwitchToPage(mediaManageSetupPage)
		app.SetFocus(scrapeSetupList.List)
	}
	frame.SetOnEscape(func() {
		handleMediaManageEscape(statePages, goBack, showInitial, closeScrapeSystemsPage)
	})

	cancelCurrentOperation := func() {
		ShowConfirmModal(pages, app, "Cancel the current media operation?", func() {
			operation := getOperation()
			if operation == "" {
				frame.FocusButtonBar()
				return
			}

			progressStatusText.SetText("Cancelling...")
			frame.SetHelpText("Cancelling...")
			frame.FocusButtonBar()

			go func(operation string) {
				cancelCtx, cancelReq := tuiContext()
				defer cancelReq()

				var err error
				switch operation {
				case mediaManageIndex:
					err = cancelMediaIndex(cancelCtx, cfg)
				case mediaManageScrape:
					err = cancelMediaScrape(cancelCtx, cfg)
				}
				if err != nil {
					log.Warn().Err(err).Msg("error cancelling media operation")
					app.QueueUpdateDraw(func() {
						ShowErrorModal(pages, app, err.Error(), func() {
							frame.FocusButtonBar()
						})
					})
					return
				}

				app.QueueUpdateDraw(func() {
					setCancelled(operation)
					setOperation("")
					setProgressVisible(false)
					completeText.SetText("Media operation cancelled.")
					statePages.SwitchToPage(mediaManageCompletePage)
					frame.SetHelpText("Operation cancelled")
					frame.SetButtonBar(createCompleteButtonBar())
					frame.FocusButtonBar()
				})
			}(operation)
		}, func() {
			frame.FocusButtonBar()
		})
	}
	resumeCurrentOperation := func() {
		operation := getOperation()
		if operation == "" {
			frame.FocusButtonBar()
			return
		}

		go func(operation string) {
			resumeCtx, cancelReq := tuiContext()
			defer cancelReq()

			var err error
			switch operation {
			case mediaManageIndex:
				err = resumeMediaIndex(resumeCtx, cfg)
			case mediaManageScrape:
				err = resumeMediaScrape(resumeCtx, cfg)
			}
			if err != nil {
				log.Warn().Err(err).Msg("error resuming media operation")
				app.QueueUpdateDraw(func() {
					ShowErrorModal(pages, app, err.Error(), func() {
						frame.FocusButtonBar()
					})
				})
				return
			}

			app.QueueUpdateDraw(func() {
				progressStatusText.SetText("Resuming...")
				frame.SetHelpText("Stop current operation")
				setProgressButtonBar(false)
			})
		}(operation)
	}
	createProgressButtonBar = func(paused bool) *ButtonBar {
		bar := NewButtonBar(app)
		if paused {
			bar.AddButtonWithHelp("Resume", "Continue paused operation", resumeCurrentOperation)
		} else {
			bar.AddButtonWithHelp("Cancel", "Stop current operation", cancelCurrentOperation)
		}
		bar.AddButtonWithHelp("Back", "Continue in background", showInitial)
		bar.focusedIndex = 1
		bar.SetupNavigation(showInitial)
		bar.SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
		return bar
	}
	setProgressButtonBar = func(paused bool) {
		if progressButtonBarPaused != nil && *progressButtonBarPaused == paused && frame.GetButtonBar() != nil {
			frame.FocusButtonBar()
			return
		}
		progressButtonBarPaused = &paused
		frame.SetButtonBar(createProgressButtonBar(paused))
		frame.FocusButtonBar()
	}

	createCompleteButtonBar = func() *ButtonBar {
		bar := NewButtonBar(app)
		bar.AddButton("Done", func() {
			showInitial()
		})
		bar.SetupNavigation(goBack)
		return bar
	}

	renderInitial = func(state mediaManageInitialState) {
		setProgressVisible(false)
		initialList.ClearItems()

		dbLabel := "Update index: unavailable"
		if state.mediaErr == nil {
			dbLabel = formatDBMenuLabel(state.media.Database)
		}
		scrapeLabel := "Scrape metadata: unavailable"
		if state.scrapeErr == nil {
			scrapeLabel = formatScrapeMenuLabel(&state.scrapeStatus)
		}
		if (state.mediaErr != nil || !state.media.Database.Indexing) &&
			(state.scrapeErr != nil || !state.scrapeStatus.Scraping) {
			setOperation("")
		}

		switch {
		case state.mediaErr == nil && state.media.Database.Indexing:
			setOperation(mediaManageIndex)
			initialList.AddAction(dbLabel, "View index progress", func() {
				showIndexProgress(state.media.Database)
			})
		case state.scrapeErr == nil && state.scrapeStatus.Scraping:
			initialList.AddAction(mediaIndexBlockedByScrapeLabel(), "Wait for scrape to finish", func() {})
		default:
			initialList.AddAction(dbLabel, "Scan media folders", func() {
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
				showIndexProgressLayout()
				progressTitle.SetText("Updating media index...")
				progressBar.SetProgress(0)
				progressStatusText.SetText("Starting scan...")
				statePages.SwitchToPage(mediaManageProgressPage)
				setProgressVisible(true)
				frame.SetHelpText("Stop current operation")
				setProgressButtonBar(false)
			})
		}

		switch {
		case state.scrapeErr == nil && state.scrapeStatus.Scraping:
			setOperation(mediaManageScrape)
			initialList.AddAction(scrapeLabel, "View scrape progress", func() {
				showScrapeProgress(state.scrapeStatus)
			})
		case state.mediaErr == nil && state.media.Database.Indexing:
			initialList.AddAction(mediaScrapeBlockedByIndexLabel(), "Wait for index to finish", func() {})
		default:
			initialList.AddNavAction(scrapeLabel, "Scrape indexed media", showScrapeSetup)
		}
		initialList.AddAction("Back", "Return to main menu", goBack)
		initialList.TriggerInitialHelp()
	}

	showInitial = func() {
		frame.SetHelpText("")
		statePages.SwitchToPage(mediaManageLoadingPage)
		frame.SetButtonBar(nil)
		app.SetFocus(frame)

		go func() {
			state := loadMediaManageInitialState(cfg)
			select {
			case <-ctx.Done():
				return
			default:
			}
			app.QueueUpdateDraw(func() {
				renderInitial(state)
				statePages.SwitchToPage(mediaManageInitialPage)
				frame.SetHelpText("Update index or scrape metadata")
				app.SetFocus(initialList.List)
			})
		}()
	}

	refreshScrapeSetup = func() {
		scrapeSetupList.ClearItems()
		if len(scrapeState.scrapers) == 0 {
			scrapeSetupList.AddAction("No scrapers available", "No metadata scrapers are available", func() {})
			scrapeSetupList.TriggerInitialHelp()
			return
		}
		scraper := scrapeState.scrapers[scrapeState.scraperIndex]
		scrapeState.systems = filterScrapeSystems(scrapeState.allSystems, scraper)
		scrapeState.selected = pruneSelectedScrapeSystems(scrapeState.selected, scrapeState.systems)

		scraperNames := make([]string, len(scrapeState.scrapers))
		for i, scraper := range scrapeState.scrapers {
			scraperNames[i] = scraper.Name
		}
		systemsLabel := formatScrapeSystemsLabel(scrapeState.selected, scrapeState.systems)

		scrapeSetupList.AddCycle(
			"Scraper",
			"Choose metadata scraper",
			scraperNames,
			&scrapeState.scraperIndex,
			func(_ string, _ int) {
				refreshScrapeSetup()
			})
		if len(scrapeState.systems) == 0 {
			scrapeSetupList.AddAction("Systems: "+systemsLabel, "No supported systems", func() {})
		} else {
			scrapeSetupList.AddNavAction(
				"Systems: "+systemsLabel,
				"Choose systems to scrape",
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
							closeScrapeSystemsPage()
							return nil
						}
						return event
					})
					statePages.RemovePage(mediaManageSystemsPage)
					statePages.AddPage(mediaManageSystemsPage, selector, true, true)
					frame.SetHelpText("Space selects systems, Esc returns")
					app.SetFocus(selector)
				})
		}
		scrapeSetupList.AddToggle(
			"Re-scrape already scraped games",
			"Include already scraped games",
			&scrapeState.force,
			func(_ bool) {})
		if len(scrapeState.systems) == 0 {
			scrapeSetupList.AddAction("Start scrape", "No supported systems", func() {})
		} else {
			scrapeSetupList.AddAction(
				"Start scrape",
				"Start metadata scrape",
				startScrapeFromSetup,
			)
		}
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
		scrapeState.allSystems = make([]SystemItem, 0, len(systems.Systems))
		for _, system := range systems.Systems {
			if system.ID == "" {
				continue
			}
			name := system.Name
			if name == "" {
				name = system.ID
			}
			scrapeState.allSystems = append(scrapeState.allSystems, SystemItem{ID: system.ID, Name: name})
		}

		frame.SetHelpText("Configure metadata scrape")
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
		if len(scrapeState.systems) == 0 {
			showError(errors.New("selected scraper supports no indexed systems"))
			return
		}
		scraper := scrapeState.scrapers[scrapeState.scraperIndex]
		systems := scrapeState.selected
		if systems == nil {
			systems = []string{}
		}
		params := models.MediaScrapeParams{
			ScraperID: scraper.ID,
			Systems:   systems,
			Force:     scrapeState.force,
		}

		setOperation(mediaManageScrape)
		setCancelled("")
		showScrapeProgressLayout()
		progressTitle.SetText("Starting scrape...")
		progressBar.SetProgress(0)
		systemProgressBar.SetProgress(0)
		progressStatusText.SetText("")
		statePages.SwitchToPage(mediaManageProgressPage)
		setProgressVisible(true)
		frame.SetHelpText("Stop current operation")
		setProgressButtonBar(false)

		go func() {
			startCtx, startCancel := tuiContext()
			err := startMediaScrape(startCtx, cfg, params)
			startCancel()
			if err != nil {
				log.Warn().Err(err).Msg("error starting media scrape")
				app.QueueUpdateDraw(func() {
					showError(err)
				})
				return
			}

			status := models.ScrapingStatusResponse{
				ScraperID: scraper.ID,
				Scraping:  true,
			}
			app.QueueUpdateDraw(func() {
				progressTitle.SetText("Scraping metadata...")
				progressBar.SetProgress(scrapeOverallProgress(status))
				systemProgressBar.SetProgress(scrapeCurrentSystemProgress(status))
				progressStatusText.SetText(formatScrapeProgress(&status, scraper.Name))
				frame.SetHelpText("Stop current operation")
				setProgressButtonBar(false)
			})
		}()
	}

	updateIndexProgress := func(current, total int, status string) {
		app.QueueUpdateDraw(func() {
			showIndexProgressLayout()
			progressTitle.SetText("Updating media index...")
			progressBar.SetProgress(mediaIndexProgress(current, total))
			progressStatusText.SetText(status)
			setProgressButtonBar(false)
		})
	}

	updateScrapeProgress := func(status models.ScrapingStatusResponse) {
		app.QueueUpdateDraw(func() {
			showScrapeProgressLayout()
			progressTitle.SetText("Scraping metadata...")
			progressBar.SetProgress(scrapeOverallProgress(status))
			systemProgressBar.SetProgress(scrapeCurrentSystemProgress(status))
			scraperName := ""
			for _, scraper := range scrapeState.scrapers {
				if scraper.ID == status.ScraperID {
					scraperName = scraper.Name
					break
				}
			}
			progressStatusText.SetText(formatScrapeProgress(&status, scraperName))
			setProgressButtonBar(status.Paused)
		})
	}

	showIndexProgress = func(status models.IndexingStatusResponse) {
		setOperation(mediaManageIndex)
		setCancelled("")
		showIndexProgressLayout()
		progressTitle.SetText("Updating media index...")
		if status.CurrentStep != nil && status.TotalSteps != nil {
			progressBar.SetProgress(mediaIndexProgress(*status.CurrentStep, *status.TotalSteps))
		} else {
			progressBar.SetProgress(0)
		}
		if status.CurrentStepDisplay != nil {
			progressStatusText.SetText(*status.CurrentStepDisplay)
		} else {
			progressStatusText.SetText("Updating media index...")
		}
		statePages.SwitchToPage(mediaManageProgressPage)
		setProgressVisible(true)
		frame.SetHelpText("Stop current operation")
		setProgressButtonBar(status.Paused)
	}

	showScrapeProgress = func(status models.ScrapingStatusResponse) {
		setOperation(mediaManageScrape)
		setCancelled("")
		showScrapeProgressLayout()
		progressTitle.SetText("Scraping metadata...")
		progressBar.SetProgress(scrapeOverallProgress(status))
		systemProgressBar.SetProgress(scrapeCurrentSystemProgress(status))
		progressStatusText.SetText(formatScrapeProgress(&status, ""))
		statePages.SwitchToPage(mediaManageProgressPage)
		setProgressVisible(true)
		frame.SetHelpText("Stop current operation")
		setProgressButtonBar(status.Paused)
	}

	showIndexComplete := func(filesFound int) {
		setOperation("")
		app.QueueUpdateDraw(func() {
			setProgressVisible(false)
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
			setProgressVisible(false)
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
				if isProgressVisible() {
					showIndexComplete(*media.Database.TotalFiles)
				} else {
					setOperation("")
				}
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
			if isProgressVisible() {
				showScrapeComplete(status)
			} else {
				setOperation("")
			}
		}
	}

	frame.SetContent(statePages)
	pages.AddAndSwitchToPage(PageGenerateDB, frame, true)
	showInitial()

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
						if isProgressVisible() {
							showIndexComplete(*indexing.TotalFiles)
							updateIndexProgress(0, 1, "")
						} else {
							setOperation("")
						}
					} else if indexing.Indexing &&
						indexing.CurrentStep != nil &&
						indexing.TotalSteps != nil &&
						indexing.CurrentStepDisplay != nil {
						setOperation(mediaManageIndex)
						if isProgressVisible() {
							updateIndexProgress(
								*indexing.CurrentStep,
								*indexing.TotalSteps,
								*indexing.CurrentStepDisplay,
							)
						}
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
						if isProgressVisible() {
							showScrapeComplete(scraping)
						} else {
							setOperation("")
						}
					} else if scraping.Scraping {
						setOperation(mediaManageScrape)
						if isProgressVisible() {
							updateScrapeProgress(scraping)
						}
					}
					lastScrape = &scraping
				}
			}
		}
	}()
}
