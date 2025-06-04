package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func getMediaState(ctx context.Context, cfg *config.Instance) (models.MediaResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodMedia, "")
	if err != nil {
		return models.MediaResponse{}, err
	}
	var tokens models.MediaResponse
	err = json.Unmarshal([]byte(resp), &tokens)
	if err != nil {
		return models.MediaResponse{}, err
	}
	return tokens, nil
}

func waitGenerateUpdate(ctx context.Context, cfg *config.Instance) (models.IndexingStatusResponse, error) {
	resp, err := client.WaitNotification(
		ctx, -1,
		cfg, models.NotificationMediaIndexing,
	)
	if err != nil {
		return models.IndexingStatusResponse{}, nil
	}
	var status models.IndexingStatusResponse
	err = json.Unmarshal([]byte(resp), &status)
	if err != nil {
		return models.IndexingStatusResponse{}, err
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
	p.Box.DrawForSubclass(screen, p)

	x, y, width, height := p.GetInnerRect()

	if height > 0 {
		barWidth := width
		filled := int(float64(barWidth) * p.progress)

		for i := 0; i < filled; i++ {
			screen.SetContent(x+i, y, p.filledRune, nil, tcell.StyleDefault.Foreground(tcell.ColorGreen))
		}

		for i := filled; i < barWidth; i++ {
			screen.SetContent(x+i, y, p.emptyRune, nil, tcell.StyleDefault.Foreground(tcell.ColorGray))
		}
	}
}

func initStatePage(
	cfg *config.Instance,
	_ *tview.Application,
	appPages *tview.Pages,
	parentPages *tview.Pages,
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
			appPages.SwitchToPage(PageMain)
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
	buttonFlex.AddItem(startButton, 0, 1, false)
	buttonFlex.AddItem(nil, 1, 0, false)
	buttonFlex.AddItem(backButton, 0, 1, false)
	buttonFlex.AddItem(nil, 0, 1, false)

	initialState.AddItem(nil, 0, 1, false)
	initialState.AddItem(explanationText, 0, 2, false)
	initialState.AddItem(buttonFlex, 3, 1, false)
	initialState.AddItem(nil, 0, 1, false)

	return initialState
}

func progressStatePage(
	_ *tview.Application,
	appPages *tview.Pages,
	_ *tview.Pages,
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
	progressState.AddItem(buttonFlex, 1, 0, false)

	progressState.AddItem(nil, 0, 1, false)

	layout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 5, 0, false).
		AddItem(progressState, 0, 1, true).
		AddItem(nil, 5, 0, false)

	return layout, progress, statusText
}

func completeStatePage(
	_ *tview.Application,
	appPages *tview.Pages,
	parentPages *tview.Pages,
) (tview.Primitive, *tview.TextView) {
	completeState := tview.NewFlex().SetDirection(tview.FlexRow)
	completeText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	doneButton := tview.NewButton("Done").
		SetSelectedFunc(func() {
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
	completeState.AddItem(buttonFlex, 1, 0, false)

	completeState.AddItem(nil, 0, 1, false)

	layout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 5, 0, false).
		AddItem(completeState, 0, 1, true).
		AddItem(nil, 5, 0, false)

	return layout, completeText
}

func BuildGenerateDBPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
) tview.Primitive {
	generateDB := tview.NewPages()
	generateDB.SetTitle("Update Media DB")
	generateDB.SetBorder(true)

	progressState, progressBar, statusText := progressStatePage(app, pages, generateDB)
	generateDB.AddPage("progress", progressState, true, false)

	initialState := initStatePage(cfg, app, pages, generateDB)
	generateDB.AddPage("initial", initialState, true, false)

	completeState, completeText := completeStatePage(app, pages, generateDB)
	generateDB.AddPage("complete", completeState, true, false)

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

	media, err := getMediaState(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error getting media state")
	} else if media.Database.Indexing {
		updateProgress(
			*media.Database.CurrentStep,
			*media.Database.TotalSteps,
			*media.Database.CurrentStepDisplay,
		)
		generateDB.SwitchToPage("progress")
	} else {
		generateDB.SwitchToPage("initial")
	}

	go func() {
		var lastUpdate *models.IndexingStatusResponse
		for {
			indexing, err := waitGenerateUpdate(context.Background(), cfg)
			if errors.Is(client.ErrRequestTimeout, err) {
				continue
			} else if err != nil {
				log.Error().Err(err).Msg("error waiting for indexing update")
				return
			}
			log.Debug().Msgf("indexing update: %+v", indexing)

			if lastUpdate != nil &&
				lastUpdate.Indexing == true &&
				indexing.Indexing == false &&
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
			}
			lastUpdate = &indexing
		}
	}()

	return generateDB
}
