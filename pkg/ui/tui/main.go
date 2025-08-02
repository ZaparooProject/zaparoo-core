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

func setupButtonNavigation(
	app *tview.Application,
	svcRunning bool,
	buttons ...*tview.Button,
) {
	for i, button := range buttons {
		if !svcRunning {
			button.SetDisabled(true)
			continue
		}

		prevIndex := (i - 1 + len(buttons)) % len(buttons)
		nextIndex := (i + 1) % len(buttons)

		button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			switch k { //nolint:exhaustive
			case tcell.KeyUp, tcell.KeyLeft:
				app.SetFocus(buttons[prevIndex])
				return event
			case tcell.KeyDown, tcell.KeyRight:
				app.SetFocus(buttons[nextIndex])
				return event
			case tcell.KeyEscape:
				app.Stop()
				return nil
			}
			return event
		})
	}
}

func BuildMainPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) tview.Primitive {
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
		svcStatus = "NOT RUNNING\nThe Zaparoo Core service may not have started. Check logs for more information."
	}

	ip := utils.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	webUI := fmt.Sprintf("http://%s:%d/app/", ip, cfg.APIPort())

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
	lastScanned.SetBorder(true).SetTitle("Last Scanned")

	if svcRunning {
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
					if errors.Is(err, client.ErrRequestTimeout) {
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
	} else {
		lastScanned.SetText("[::b]Time:[::-]  -\n[::b]ID:[::-]    -\n[::b]Value:[::-] -")
	}

	displayCol := tview.NewFlex().SetDirection(tview.FlexRow)
	displayCol.AddItem(introText, 1, 1, false)
	displayCol.AddItem(statusText, 0, 1, false)
	displayCol.AddItem(lastScanned, 6, 1, false)
	displayCol.AddItem(helpText, 1, 1, false)

	// create the main modal
	main.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	main.AddItem(displayCol, 0, 1, false)

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		BuildSearchMedia(cfg, pages, app)
	})
	searchButton.SetFocusFunc(func() {
		helpText.SetText("Search for media and write to an NFC tag.")
	})

	writeButton := tview.NewButton("Custom write").SetSelectedFunc(func() {
		BuildTagsWriteMenu(cfg, pages, app)
	})
	writeButton.SetFocusFunc(func() {
		helpText.SetText("Write custom ZapScript to an NFC tag.")
	})

	updateDBButton := tview.NewButton("Update media DB").SetSelectedFunc(func() {
		BuildGenerateDBPage(cfg, pages, app)
	})
	updateDBButton.SetFocusFunc(func() {
		helpText.SetText("Scan disk to create index of games.")
	})

	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		BuildSettingsMainMenu(cfg, pages, app)
	})
	settingsButton.SetFocusFunc(func() {
		helpText.SetText("Manage settings for Core service.")
	})

	exportButton := tview.NewButton("Export log").SetSelectedFunc(func() {
		BuildExportLogModal(pl, app, pages, logDestPath, logDestName)
	})
	exportButton.SetFocusFunc(func() {
		helpText.SetText("Export Core log file for support.")
	})

	exitButton := tview.NewButton("Exit").SetSelectedFunc(func() {
		app.Stop()
	})
	exitButton.SetFocusFunc(func() {
		if svcRunning {
			helpText.SetText("Exit TUI app. (service will continue running)")
		} else {
			helpText.SetText("Exit TUI app.")
		}
	})

	setupButtonNavigation(
		app,
		svcRunning,
		searchButton,
		writeButton,
		updateDBButton,
		settingsButton,
		exportButton,
		exitButton,
	)
	if !svcRunning {
		exitButton.SetDisabled(false)
	}

	main.AddItem(tview.NewTextView(), 1, 1, false)

	buttonNav := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView(), 0, 1, false).
		AddItem(searchButton, 1, 1, svcRunning).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(writeButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(updateDBButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(settingsButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(exportButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(exitButton, 1, 1, !svcRunning).
		AddItem(tview.NewTextView(), 0, 1, false)
	main.AddItem(buttonNav, 20, 1, true)

	pageDefaults(PageMain, pages, main)
	return main
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

	pages := tview.NewPages()
	BuildMainPage(cfg, pages, app, pl, isRunning, logDestPath, logDestName)

	centeredPages := CenterWidget(75, 15, pages)
	return app.SetRoot(centeredPages, true), nil
}
