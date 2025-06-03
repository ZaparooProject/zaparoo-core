package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	PageMain              = "main"
	PageSettingsMain      = "settings_main"
	PageSettingsTags      = "settings_tags"
	PageSettingsTagsRead  = "settings_tags_read"
	PageSettingsTagsWrite = "settings_tags_write"
	PageSettingsAudio     = "settings_audio"
	PageSettingsReaders   = "settings_readers"
	PageSettingsScanMode  = "settings_readers_scanMode"
	PageSearchMedia       = "search_media"
	PageExportLog         = "export_log"
	PageGenerateDB        = "generate_db"
)

func getTokens(ctx context.Context, cfg *config.Instance) (models.TokensResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodTokens, "")
	if err != nil {
		return models.TokensResponse{}, err
	}
	var tokens models.TokensResponse
	err = json.Unmarshal([]byte(resp), &tokens)
	if err != nil {
		return models.TokensResponse{}, err
	}
	return tokens, nil
}

func setupButtonNavigation(app *tview.Application, buttons ...*tview.Button) {
	for i, button := range buttons {
		prevIndex := (i - 1 + len(buttons)) % len(buttons)
		nextIndex := (i + 1) % len(buttons)

		button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			if k == tcell.KeyUp || k == tcell.KeyLeft {
				app.SetFocus(buttons[prevIndex])
				return event
			} else if k == tcell.KeyDown || k == tcell.KeyRight {
				app.SetFocus(buttons[nextIndex])
				return event
			}
			return event
		})
	}
}

func BuildMain(
	cfg *config.Instance,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) (*tview.Application, error) {
	app := tview.NewApplication()
	SetTheme(&tview.Styles)

	main := tview.NewFlex()

	introText := tview.NewTextView().SetText(
		"Visit [::bu:https://zaparoo.org]zaparoo.org[::-:-] for guides and support.",
	).SetDynamicColors(true)
	statusText := tview.NewTextView().SetDynamicColors(true)

	svcRunning := isRunning()
	var svcStatus string
	if svcRunning {
		svcStatus = "RUNNING"
	} else {
		svcStatus = "NOT RUNNING"
	}

	ip := utils.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	webUI := fmt.Sprintf("http://%s:%d/app/", ip, cfg.ApiPort())

	statusText.SetText(
		fmt.Sprintf(
			"[::b]Status:[::-]  %s\n[::b]Address:[::-] %s\n[::b]Web UI:[::-]  [:::%s]%s[:::-]",
			svcStatus,
			ipDisplay,
			webUI, webUI,
		),
	)

	helpText := tview.NewTextView()
	lastScanned := tview.NewTextView()
	lastScanned.SetDynamicColors(true)

	if svcRunning {
		lastScanned.SetBorder(true).SetTitle("Last Scanned")
		tokens, err := getTokens(context.Background(), cfg)
		if err != nil {
			lastScanned.SetText("Error checking last scanned:\n" + err.Error())
		} else {
			if tokens.Last != nil {
				lastScanned.SetText(fmt.Sprintf(
					"[::b]Time:[::-]  %s\n[::b]ID:[::-]    %s\n[::b]Value:[::-] %s",
					tokens.Last.ScanTime.Format("2006-01-02 15:04:05"),
					tokens.Last.UID,
					tokens.Last.Text,
				))
			} else {
				lastScanned.SetText("[::b]Time:[::-]  -\n[::b]ID:[::-]    -\n[::b]Value:[::-] -")
			}

			go func() {
				for {
					resp, err := client.WaitNotification(
						context.Background(), -1,
						cfg, models.NotificationTokensAdded,
					)
					if errors.Is(client.ErrRequestTimeout, err) {
						continue
					} else if err != nil {
						app.QueueUpdateDraw(func() {
							lastScanned.SetText("Error checking last scanned:\n" + err.Error())
						})
						return
					}

					var token models.TokenResponse
					err = json.Unmarshal([]byte(resp), &token)
					if err != nil {
						app.QueueUpdateDraw(func() {
							lastScanned.SetText("Error checking last scanned:\n" + err.Error())
						})
						return
					}

					app.QueueUpdateDraw(func() {
						lastScanned.SetText(fmt.Sprintf(
							"[::b]Time:[::-]  %s\n[::b]ID:[::-]    %s\n[::b]Value:[::-] %s",
							token.ScanTime.Format("2006-01-02 15:04:05"),
							token.UID,
							token.Text,
						))
					})
				}
			}()
		}
	}

	displayCol := tview.NewFlex().SetDirection(tview.FlexRow)
	displayCol.AddItem(introText, 1, 1, false)
	displayCol.AddItem(tview.NewTextView(), 1, 1, false)
	displayCol.AddItem(statusText, 3, 1, false)
	displayCol.AddItem(tview.NewTextView(), 0, 1, false)
	displayCol.AddItem(lastScanned, 9, 1, false)
	displayCol.AddItem(helpText, 1, 1, false)

	pages := tview.NewPages().
		AddPage(PageMain, main, true, true)

	// create the main modal
	main.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	main.AddItem(displayCol, 0, 1, false)

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSearchMedia)
	})
	searchButton.SetBorder(true)
	searchButton.SetFocusFunc(func() {
		searchButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Search for media and write to an NFC tag.")
	})
	searchButton.SetBlurFunc(func() {
		searchButton.SetBorderColor(tcell.ColorWhite)
	})

	writeButton := tview.NewButton("Custom write").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSettingsTagsWrite)
	})
	writeButton.SetBorder(true)
	writeButton.SetFocusFunc(func() {
		writeButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Write custom ZapScript to an NFC tag.")
	})
	writeButton.SetBlurFunc(func() {
		writeButton.SetBorderColor(tcell.ColorWhite)
	})

	updateDBButton := tview.NewButton("Update media DB").SetSelectedFunc(func() {
		pages.AddAndSwitchToPage(PageGenerateDB, BuildGenerateDBPage(cfg, pages, app), true)
	})
	updateDBButton.SetBorder(true)
	updateDBButton.SetFocusFunc(func() {
		updateDBButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Scan disk to create index of games.")
	})
	updateDBButton.SetBlurFunc(func() {
		updateDBButton.SetBorderColor(tcell.ColorWhite)
	})

	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		pages.SwitchToPage(PageSettingsMain)
	})
	settingsButton.SetBorder(true)
	settingsButton.SetFocusFunc(func() {
		settingsButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Manage settings for Core service.")
	})
	settingsButton.SetBlurFunc(func() {
		settingsButton.SetBorderColor(tcell.ColorWhite)
	})

	exportButton := tview.NewButton("Export log").SetSelectedFunc(func() {
		pages.SwitchToPage(PageExportLog)
	})
	exportButton.SetBorder(true)
	exportButton.SetFocusFunc(func() {
		exportButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Export Core log file for support.")
	})
	exportButton.SetBlurFunc(func() {
		exportButton.SetBorderColor(tcell.ColorWhite)
	})

	exitButton := tview.NewButton("Exit").SetSelectedFunc(func() {
		app.Stop()
	})
	exitButton.SetBorder(true)
	exitButton.SetFocusFunc(func() {
		exitButton.SetBorderColor(tcell.ColorDarkBlue)
		helpText.SetText("Exit app. (service will continue running)")
	})
	exitButton.SetBlurFunc(func() {
		exitButton.SetBorderColor(tcell.ColorWhite)
	})

	setupButtonNavigation(
		app,
		searchButton,
		writeButton,
		updateDBButton,
		settingsButton,
		exportButton,
		exitButton,
	)

	main.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape {
			app.Stop()
		}
		return event
	})

	main.AddItem(tview.NewTextView(), 1, 1, false)

	buttonNav := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(searchButton, 0, 1, true).
		AddItem(writeButton, 0, 1, false).
		AddItem(updateDBButton, 0, 1, false).
		AddItem(settingsButton, 0, 1, false).
		AddItem(exportButton, 0, 1, false).
		AddItem(exitButton, 0, 1, false)
	main.AddItem(buttonNav, 20, 1, true)

	BuildExportLogModal(pl, app, pages, logDestPath, logDestName)
	BuildSettingsMainMenu(cfg, pages, app)
	BuildTagsMenu(cfg, pages, app)
	BuildTagsReadMenu(cfg, pages, app)
	BuildSearchMedia(cfg, pages, app)
	BuildTagsWriteMenu(cfg, pages, app)
	BuildAudioMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)

	pages.SwitchToPage(PageMain)

	centeredPages := centerWidget(70, 20, pages)
	return app.SetRoot(centeredPages, true).
		EnableMouse(true), nil
}
