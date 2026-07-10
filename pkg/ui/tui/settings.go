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
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// BuildSettingsMainMenu creates the top-level settings menu with Audio, Readers, and Advanced options.
func BuildSettingsMainMenu(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildMainPage func(),
	logDestPath string,
	logDestName string,
) {
	svc := NewSettingsService(client.NewLocalAPIClient(cfg))
	BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage, logDestPath, logDestName)
}

// BuildSettingsMainMenuWithService creates the settings menu using the given SettingsService.
func BuildSettingsMainMenuWithService(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildMainPage func(),
	logDestPath string,
	logDestName string,
) {
	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings")

	goBack := func() {
		if rebuildMainPage != nil {
			rebuildMainPage()
		} else {
			pages.SwitchToPage(PageMain)
		}
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	// Create settings list
	mainMenu := NewSettingsList(pages, PageMain)
	if rebuildMainPage != nil {
		mainMenu.SetRebuildPrevious(rebuildMainPage)
	}

	// Enable dynamic help mode
	mainMenu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	rebuildSettingsMain := func() {
		BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage, logDestPath, logDestName)
	}

	mainMenu.
		AddNavAction("Readers", "Reader connections and scanning", func() {
			buildReadersSettingsMenu(cfg, svc, pages, app, pl)
		}).
		AddNavAction("Audio", "Sound and feedback settings", func() {
			buildAudioSettingsMenu(svc, pages, app)
		}).
		AddNavAction("TUI", "Theme and display preferences", func() {
			buildTUISettingsMenu(pages, app, pl, rebuildSettingsMain)
		}).
		AddNavAction("Backup", "Back up and restore this device", func() {
			buildBackupSettingsMenu(svc, pages, app)
		})
	if showZaparooOnlineLink(svc) {
		mainMenu.AddNavAction("Zaparoo Online", "Connect this device to your account", func() {
			startAuthLinkFlow(svc, pages, app, rebuildSettingsMain)
		})
	}
	mainMenu.
		AddNavAction("Advanced", "Debug and system options", func() {
			buildAdvancedSettingsMenu(svc, pages, app)
		}).
		AddNavAction("Logs", "View and export log files", func() {
			BuildExportLogModal(pages, app, pl, logDestPath, logDestName)
		}).
		AddNavAction("About", "Version, license, and credits", func() {
			buildAboutPage(pages, app)
		})

	// Set content and trigger initial help
	frame.SetContent(mainMenu.List)
	mainMenu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsMain, frame, true)
}

// buildAudioSettingsMenu creates the audio settings submenu.
func buildAudioSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load audio settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Audio")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	audioFeedback := settings.AudioScanFeedback

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddToggle("Audio feedback on scan", "Play sound when token is scanned", &audioFeedback, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			AudioScanFeedback: &value,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating audio feedback")
			ShowErrorModal(pages, app, "Failed to save audio settings", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsAudioMenu, frame, true)
}

// exitDelayLabels extracts display labels from ExitDelayOptions.
func exitDelayLabels() []string {
	labels := make([]string, len(ExitDelayOptions))
	for i, opt := range ExitDelayOptions {
		labels[i] = opt.Label
	}
	return labels
}

// findExitDelayIndex finds the index of the given delay value in ExitDelayOptions.
func findExitDelayIndex(delay float32) int {
	for i, opt := range ExitDelayOptions {
		if opt.Value == delay {
			return i
		}
	}
	return 0
}

// buildReadersSettingsMenu creates the readers settings submenu.
func buildReadersSettingsMenu(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load reader settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	autoDetect := settings.ReadersAutoDetect

	scanModeOptions := []string{"Tap", "Hold"}
	scanModeIndex := 0
	if settings.ReadersScanMode == config.ScanModeHold {
		scanModeIndex = 1
	}

	exitDelayIndex := findExitDelayIndex(settings.ReadersScanExitDelay)

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddToggle("Auto-detect readers", "Automatically find connected readers", &autoDetect, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			ReadersAutoDetect: &value,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating auto-detect")
			ShowErrorModal(pages, app, "Failed to save auto-detect setting", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	scanModeIdx := menu.GetItemCount()
	scanModeDesc := "Tap: tap to launch, Hold: exits when removed"
	menu.AddCycle("Scan mode", scanModeDesc, scanModeOptions, &scanModeIndex, func(option string, _ int) {
		mode := strings.ToLower(option)
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			ReadersScanMode: &mode,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating scan mode")
			ShowErrorModal(pages, app, "Failed to save scan mode", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	exitDelayIdx := menu.GetItemCount()
	exitDelayDesc := "Time to wait before exiting in Hold mode"
	exitLabels := exitDelayLabels()
	menu.AddCycle("Exit delay", exitDelayDesc, exitLabels, &exitDelayIndex, func(_ string, idx int) {
		delayF := ExitDelayOptions[idx].Value
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			ReadersScanExitDelay: &delayF,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating exit delay")
			ShowErrorModal(pages, app, "Failed to save exit delay", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	menu.AddNavAction("Manage readers", "Add, edit, or remove manual reader connections", func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

	cycleIndices := map[int]func(delta int){
		scanModeIdx: func(delta int) {
			scanModeIndex = (scanModeIndex + delta + len(scanModeOptions)) % len(scanModeOptions)
			mode := strings.ToLower(scanModeOptions[scanModeIndex])
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
				ReadersScanMode: &mode,
			})
			if err != nil {
				log.Warn().Err(err).Msg("error updating scan mode")
				ShowErrorModal(pages, app, "Failed to save scan mode", func() {
					app.SetFocus(menu.List)
				})
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
		exitDelayIdx: func(delta int) {
			exitDelayIndex = (exitDelayIndex + delta + len(ExitDelayOptions)) % len(ExitDelayOptions)
			delayF := ExitDelayOptions[exitDelayIndex].Value
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
				ReadersScanExitDelay: &delayF,
			})
			if err != nil {
				log.Warn().Err(err).Msg("error updating exit delay")
				ShowErrorModal(pages, app, "Failed to save exit delay", func() {
					app.SetFocus(menu.List)
				})
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
	}

	menu.SetupCycleKeys(cycleIndices)

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsReadersMenu, frame, true)
}

// buildBackupSettingsMenu creates the backup settings submenu.
func buildBackupSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	status, err := svc.GetBackupStatus(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching backup status")
		ShowErrorModal(pages, app, "Failed to load backup status", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}
	frame := NewPageFrame(app).SetTitle("Settings", "Backup")
	goBack := func() { pages.SwitchToPage(PageSettingsMain) }
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetDynamicHelpMode(true).SetHelpCallback(func(desc string) { frame.SetHelpText(desc) })

	localDesc := backupStatusDescription(&status.Local)
	menu.AddNavAction("Back up now", "Create a portable local backup ZIP", func() {
		runBackupAction(
			pages,
			app,
			menu.List,
			"Creating backup",
			"Creating local backup ZIP...",
			func(ctx context.Context) (string, error) {
				name, backupErr := svc.CreateBackup(ctx)
				if backupErr != nil {
					return "", fmt.Errorf("create local backup: %w", backupErr)
				}
				return backupLabelFromName("Local backup", name), nil
			},
			func(label string) {
				ShowInfoModal(pages, app, "Backup created", label, func() {
					buildBackupSettingsMenu(svc, pages, app)
				})
			},
		)
	})
	menu.AddNavAction("View backups", localDesc, func() {
		buildBackupListPage(svc, pages, app)
	})

	if status.Remote.Linked {
		enabled := status.Remote.Enabled
		menu.AddToggle("Remote backup", "Enable or disable scheduled remote backup", &enabled, func(value bool) {
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{BackupRemoteEnabled: &value})
			if err != nil {
				log.Warn().Err(err).Msg("error updating remote backup")
				ShowErrorModal(pages, app, "Failed to save remote backup setting", func() { app.SetFocus(menu.List) })
			}
		})
		menu.AddNavAction("Back up remotely now", "Upload current backup to remote provider", func() {
			runBackupAction(
				pages,
				app,
				menu.List,
				"Creating remote backup",
				"Uploading backup to remote provider...",
				func(ctx context.Context) (string, error) {
					id, backupErr := svc.RunRemoteBackup(ctx)
					if backupErr != nil {
						return "", fmt.Errorf("create remote backup: %w", backupErr)
					}
					return fmt.Sprintf("Remote backup %d", id), nil
				},
				func(label string) {
					ShowInfoModal(pages, app, "Remote backup created", label, func() {
						buildBackupSettingsMenu(svc, pages, app)
					})
				},
			)
		})
		menu.AddNavAction("View remote backups", "List and restore remote backup snapshots", func() {
			buildRemoteBackupListPage(svc, pages, app)
		})
		scheduleOptions := []string{"daily", "weekly", "manual"}
		scheduleIndex := 0
		for i, option := range scheduleOptions {
			if option == status.Remote.Schedule {
				scheduleIndex = i
				break
			}
		}
		menu.AddCycle(
			"Remote schedule",
			"How often remote backup should run",
			scheduleOptions,
			&scheduleIndex,
			func(value string, _ int) {
				ctx, cancel := tuiContext()
				defer cancel()
				err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{BackupRemoteSchedule: &value})
				if err != nil {
					log.Warn().Err(err).Msg("error updating remote backup schedule")
					ShowErrorModal(pages, app, "Failed to save remote backup schedule", func() {
						app.SetFocus(menu.List)
					})
				}
			},
		)
		menu.AddNavAction("Remote status", backupStatusDescription(&status.Remote), func() {
			ShowInfoModal(pages, app, "Remote backup", backupStatusText(&status.Remote), func() {
				app.SetFocus(menu.List)
			})
		})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsBackup, frame, true)
}

const backupProgressModalPage = "backup_progress_modal"

func runBackupAction(
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	title string,
	message string,
	run func(context.Context) (string, error),
	onSuccess func(string),
) {
	ctx, cancel := backupContext()
	started := time.Now()
	var dismissed atomic.Bool
	done := make(chan struct{})

	modal := tview.NewModal().
		SetText(backupProgressText(message, started)).
		AddButtons([]string{"Hide"}).
		SetDoneFunc(func(_ int, _ string) {
			dismissed.Store(true)
			pages.HidePage(backupProgressModalPage)
			pages.RemovePage(backupProgressModalPage)
			app.SetFocus(focus)
		})
	modal.SetTitle(" " + title + " ").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	pages.AddPage(backupProgressModalPage, modal, false, true)
	app.SetFocus(modal)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if dismissed.Load() {
					return
				}
				app.QueueUpdateDraw(func() {
					modal.SetText(backupProgressText(message, started))
				})
			}
		}
	}()

	go func() {
		label, err := run(ctx)
		cancel()
		close(done)
		app.QueueUpdateDraw(func() {
			pages.HidePage(backupProgressModalPage)
			pages.RemovePage(backupProgressModalPage)
			if dismissed.Load() {
				app.SetFocus(focus)
				return
			}
			if err != nil {
				log.Warn().Err(err).Msg("error running backup action")
				ShowErrorModal(pages, app, title+" failed", func() { app.SetFocus(focus) })
				return
			}
			onSuccess(label)
		})
	}()
}

func backupProgressText(message string, started time.Time) string {
	return fmt.Sprintf(
		"%s\n\nElapsed: %s\n\nTime depends on save data size. Hide keeps it running.",
		message,
		time.Since(started).Round(time.Second),
	)
}

// showZaparooOnlineLink reports whether the main settings menu should offer
// the account linking entry: only when the device is not already linked.
func showZaparooOnlineLink(svc SettingsService) bool {
	ctx, cancel := tuiContext()
	defer cancel()
	status, err := svc.GetBackupStatus(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("error fetching backup status for settings menu")
		return false
	}
	return !status.Remote.Linked
}

const authLinkModalPage = "authLinkModal"

// startAuthLinkFlow runs the reverse device link flow: display the user code
// and verification URL, then wait for the link to be approved online. onDone
// rebuilds the caller's menu when the flow ends, whatever the outcome.
func startAuthLinkFlow(svc SettingsService, pages *tview.Pages, app *tview.Application, onDone func()) {
	ctx, cancel := tuiContext()
	defer cancel()
	link, err := svc.StartAuthLink(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error starting device link")
		ShowErrorModal(pages, app, "Failed to start device linking", onDone)
		return
	}

	message := fmt.Sprintf(
		"Visit:\n%s\n\nEnter code:\n%s\n\nWaiting for approval...",
		link.VerificationURL, link.UserCode,
	)

	done := make(chan struct{})
	closeModal := func() {
		pages.HidePage(authLinkModalPage)
		pages.RemovePage(authLinkModalPage)
	}

	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(_ int, _ string) {
			close(done)
			cancelCtx, cancelCancel := tuiContext()
			defer cancelCancel()
			if cancelErr := svc.CancelAuthLink(cancelCtx); cancelErr != nil {
				log.Debug().Err(cancelErr).Msg("error cancelling device link")
			}
			closeModal()
			onDone()
		})
	modal.SetTitle(" Link with Zaparoo Online ").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	pages.AddPage(authLinkModalPage, modal, false, true)
	app.SetFocus(modal)

	go pollAuthLinkStatus(svc, pages, app, done, closeModal, onDone)
}

// pollAuthLinkStatus watches the link flow until it reaches a terminal state
// and swaps the waiting modal for the outcome.
func pollAuthLinkStatus(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	done <-chan struct{},
	closeModal func(),
	onDone func(),
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}

		ctx, cancel := tuiContext()
		status, err := svc.GetAuthLinkStatus(ctx)
		cancel()
		if err != nil {
			log.Debug().Err(err).Msg("error polling device link status")
			continue
		}
		switch status.Status {
		case models.AuthLinkStatusPending:
			continue
		case models.AuthLinkStatusApproved:
			app.QueueUpdateDraw(func() {
				closeModal()
				ShowInfoModal(pages, app, "Device linked",
					"This device is now linked to Zaparoo Online.", onDone)
			})
			return
		default:
			reason := status.Error
			if reason == "" {
				reason = "Device linking did not complete."
			}
			app.QueueUpdateDraw(func() {
				closeModal()
				ShowErrorModal(pages, app, reason, onDone)
			})
			return
		}
	}
}

func buildBackupListPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	backups, err := svc.ListBackups(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error listing backups")
		ShowErrorModal(pages, app, "Failed to list backups", func() { pages.SwitchToPage(PageSettingsBackup) })
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Backup", "Restore")
	goBack := func() { buildBackupSettingsMenu(svc, pages, app) }
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(true)
	list.SetSelectedFocusOnly(true)
	for _, backup := range backups {
		name := backupString(backup, "name")
		if name == "" {
			continue
		}
		secondary := formatBackupSize(backup, "size")
		backupName := name
		backupInfo := backup
		list.AddItem(backupDisplayLabel("Local backup", name, backupString(backup, "createdAt")), secondary, 0, func() {
			showBackupManageModal(svc, pages, app, list, backupInfo, backupName, goBack)
		})
	}
	if len(backups) == 0 {
		list.AddItem("(no backups found)", "Create a backup first", 0, nil)
	}

	frame.SetContent(list)
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsBackupList, frame, true)
	frame.FocusContent()
}

const backupManageModalPage = "backup_manage_modal"

func showBackupManageModal(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	backup map[string]any,
	backupName string,
	onRestored func(),
) {
	label := backupDisplayLabel("Local backup", backupName, backupString(backup, "createdAt"))
	showBackupModal(pages, app, " Backup details ", "Loading backup manifest...", []string{"Back"}, func(_ int) {
		app.SetFocus(focus)
	})

	go func() {
		ctx, cancel := backupContext()
		details, err := svc.InspectBackup(ctx, backupName)
		cancel()
		app.QueueUpdateDraw(func() {
			if err != nil {
				log.Warn().Err(err).Str("name", backupName).Msg("error inspecting backup")
				showBackupModal(
					pages,
					app,
					" Backup details ",
					label+"\n\nUnable to read backup manifest.\n\nRestore is disabled for this backup.",
					[]string{"Back"},
					func(_ int) { app.SetFocus(focus) },
				)
				return
			}
			showBackupDetailsActions(svc, pages, app, focus, details, backupName, onRestored)
		})
	}()
}

func showBackupDetailsActions(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	backup map[string]any,
	backupName string,
	onRestored func(),
) {
	label := backupDisplayLabel("Local backup", backupName, backupString(backup, "createdAt"))
	showBackupModal(
		pages,
		app,
		" Backup details ",
		formatBackupDetails(label, backup),
		[]string{"Restore", "Delete", "Back"},
		func(buttonIndex int) {
			switch buttonIndex {
			case 0:
				showBackupRestoreConfirm(svc, pages, app, focus, backupName, onRestored)
			case 1:
				showBackupDeleteConfirm(svc, pages, app, focus, label, backupName, onRestored)
			default:
				app.SetFocus(focus)
			}
		},
	)
}

func showBackupModal(
	pages *tview.Pages,
	app *tview.Application,
	title string,
	message string,
	buttons []string,
	onDone func(int),
) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons(buttons).
		SetDoneFunc(func(buttonIndex int, _ string) {
			pages.HidePage(backupManageModalPage)
			pages.RemovePage(backupManageModalPage)
			onDone(buttonIndex)
		})
	modal.SetTitle(title).
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	pages.RemovePage(backupManageModalPage)
	pages.AddPage(backupManageModalPage, modal, false, true)
	app.SetFocus(modal)
}

func showBackupDeleteConfirm(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	label string,
	backupName string,
	onDeleted func(),
) {
	ShowConfirmModal(pages, app, "Delete "+label+"?", func() {
		ctx, cancel := tuiContext()
		defer cancel()
		if err := svc.DeleteBackup(ctx, backupName); err != nil {
			log.Warn().Err(err).Msg("error deleting backup")
			ShowErrorModal(pages, app, "Failed to delete backup", func() { app.SetFocus(focus) })
			return
		}
		ShowInfoModal(pages, app, "Backup deleted", label, onDeleted)
	}, func() { app.SetFocus(focus) })
}

func showBackupRestoreConfirm(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	backupName string,
	onRestored func(),
) {
	label := backupLabelFromName("Local backup", backupName)
	ShowConfirmModal(pages, app, "Restore "+label+"?", func() {
		runBackupAction(
			pages,
			app,
			focus,
			"Restoring backup",
			"Restoring local backup...",
			func(ctx context.Context) (string, error) {
				if err := svc.RestoreBackup(ctx, backupName); err != nil {
					return "", fmt.Errorf("restore local backup: %w", err)
				}
				return label, nil
			},
			func(restoredLabel string) {
				ShowInfoModal(pages, app, "Backup restored", restoredLabel, onRestored)
			},
		)
	}, func() { app.SetFocus(focus) })
}

func buildRemoteBackupListPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	backups, err := svc.ListRemoteBackups(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error listing remote backups")
		ShowErrorModal(pages, app, "Failed to list remote backups", func() { pages.SwitchToPage(PageSettingsBackup) })
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Backup", "Remote Restore")
	goBack := func() { buildBackupSettingsMenu(svc, pages, app) }
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(true)
	list.SetSelectedFocusOnly(true)
	for _, backup := range backups {
		id, ok := backupInt64(backup, "id")
		if !ok {
			continue
		}
		label := backupDisplayLabel("Remote backup", "", backupString(backup, "createdAt"))
		if label == "Remote backup" {
			label = fmt.Sprintf("Remote backup %d", id)
		}
		secondary := "ID " + strconv.FormatInt(id, 10)
		if size := formatBackupSize(backup, "sizeBytes"); size != "" {
			secondary += "  " + size
		}
		if incompatible, ok := backup["incompatible"].(bool); ok && incompatible {
			// Committed by a newer Core: listed, but refuses to restore.
			list.AddItem(label+" (requires newer Core)", secondary, 0, func() {
				ShowInfoModal(pages, app, "Incompatible backup",
					"This backup was made by a newer Core version and cannot be restored "+
						"until this device is updated.", func() { app.SetFocus(list) })
			})
			continue
		}
		backupID := id
		list.AddItem(label, secondary, 0, func() {
			showRemoteBackupRestoreConfirm(svc, pages, app, list, backupID, goBack)
		})
	}
	if len(backups) == 0 {
		list.AddItem("(no remote backups found)", "Run a remote backup first", 0, nil)
	}

	frame.SetContent(list)
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsBackupList, frame, true)
	frame.FocusContent()
}

func showRemoteBackupRestoreConfirm(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	focus tview.Primitive,
	backupID int64,
	onRestored func(),
) {
	ShowConfirmModal(pages, app, fmt.Sprintf("Restore remote backup %d?", backupID), func() {
		runBackupAction(
			pages,
			app,
			focus,
			"Restoring remote backup",
			"Downloading and restoring remote backup...",
			func(ctx context.Context) (string, error) {
				if err := svc.RestoreRemoteBackup(ctx, backupID); err != nil {
					return "", fmt.Errorf("restore remote backup: %w", err)
				}
				return strconv.FormatInt(backupID, 10), nil
			},
			func(restoredLabel string) {
				ShowInfoModal(pages, app, "Remote backup restored", restoredLabel, onRestored)
			},
		)
	}, func() { app.SetFocus(focus) })
}

func backupStatusDescription(status *models.BackupStatusEntry) string {
	if status.LastStatus == "" || status.LastStatus == "never" {
		return "No successful backup yet"
	}
	if status.LastSuccessAt != nil && *status.LastSuccessAt != "" {
		return "Last success: " + *status.LastSuccessAt
	}
	return "Last status: " + status.LastStatus
}

func backupStatusText(status *models.BackupStatusEntry) string {
	lines := []string{"Status: " + status.LastStatus}
	if status.Schedule != "" {
		lines = append(lines, "Schedule: "+status.Schedule)
	}
	if status.LastSuccessAt != nil {
		lines = append(lines, "Last success: "+*status.LastSuccessAt)
	}
	if status.LastError != "" {
		lines = append(lines, "Last error: "+status.LastError)
	}
	for _, category := range []string{"zaparoo", "settings", "inputs", "saves", "savestates"} {
		entry, ok := status.Categories[category]
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %d files, %d bytes", category, entry.Files, entry.Bytes))
	}
	return strings.Join(lines, "\n")
}

func formatBackupDetails(label string, backup map[string]any) string {
	lines := []string{label}
	if createdAt, ok := formatBackupTimestamp(backupString(backup, "createdAt")); ok {
		lines = append(lines, "Created: "+createdAt)
	}
	if size := formatBackupSize(backup, "size"); size != "" {
		lines = append(lines, "Size: "+size)
	}
	if status := backupString(backup, "status"); status != "" {
		lines = append(lines, "Status: "+status)
	}
	if valid, ok := backup["valid"].(bool); ok && !valid {
		lines = append(lines, "Valid: no")
	} else if ok {
		lines = append(lines, "Valid: yes")
	}
	if errText := backupString(backup, "error"); errText != "" {
		lines = append(lines, "Error: "+errText)
	}
	if categoryLines := formatBackupCategories(backup); len(categoryLines) > 0 {
		lines = append(lines, "", "Manifest:")
		lines = append(lines, categoryLines...)
	}
	return strings.Join(lines, "\n")
}

func formatBackupCategories(backup map[string]any) []string {
	raw, ok := backup["categories"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	labels := map[string]string{
		"zaparoo":    "Zaparoo",
		"settings":   "Settings",
		"inputs":     "Inputs",
		"saves":      "Saves",
		"savestates": "Save states",
	}
	ordered := []string{"zaparoo", "settings", "inputs", "saves", "savestates"}
	lines := make([]string, 0, len(raw))
	for _, category := range ordered {
		entry, ok := raw[category].(map[string]any)
		if !ok {
			continue
		}
		name := labels[category]
		files, _ := backupAnyInt64(entry["files"])
		bytes, _ := backupAnyInt64(entry["bytes"])
		lines = append(lines, fmt.Sprintf("%s: %d files, %s", name, files, formatHumanBytes(bytes)))
	}
	return lines
}

func backupDisplayLabel(prefix, name, createdAt string) string {
	if label, ok := formatBackupTimestamp(createdAt); ok {
		return prefix + " " + label
	}
	return backupLabelFromName(prefix, name)
}

func backupLabelFromName(prefix, name string) string {
	if label, ok := timestampFromBackupName(name); ok {
		return prefix + " " + label
	}
	if name == "" {
		return prefix
	}
	return name
}

func timestampFromBackupName(name string) (string, bool) {
	trimmed := strings.TrimPrefix(name, "backup-")
	parts := strings.SplitN(trimmed, "-", 3)
	if len(parts) < 2 {
		return "", false
	}
	parsed, err := time.ParseInLocation("20060102150405", parts[0]+parts[1], time.UTC)
	if err != nil {
		return "", false
	}
	return formatBackupTime(parsed), true
}

func formatBackupTimestamp(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", false
	}
	return formatBackupTime(parsed), true
}

func formatBackupTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func formatBackupSize(backup map[string]any, key string) string {
	value, ok := backup[key]
	if !ok || value == nil {
		return ""
	}
	bytes, ok := backupAnyInt64(value)
	if !ok {
		return ""
	}
	return formatHumanBytes(bytes)
}

func formatHumanBytes(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unitIndex := -1
	for value >= 1024 && unitIndex < len(units)-1 {
		value /= 1024
		unitIndex++
	}
	value = math.Ceil(value*10) / 10
	if value == math.Trunc(value) {
		return fmt.Sprintf("%.0f %s", value, units[unitIndex])
	}
	return fmt.Sprintf("%.1f %s", value, units[unitIndex])
}

func backupString(backup map[string]any, key string) string {
	value, ok := backup[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func backupInt64(backup map[string]any, key string) (int64, bool) {
	value, ok := backup[key]
	if !ok || value == nil {
		return 0, false
	}
	return backupAnyInt64(value)
}

func backupAnyInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}

// buildAdvancedSettingsMenu creates the advanced settings menu.
func buildAdvancedSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load advanced settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Advanced")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	debugLogging := settings.DebugLogging
	errorReporting := settings.ErrorReporting

	// Build ignore systems label with count indicator
	ignoreLabel := "Ignore systems"
	ignoreCount := len(settings.ReadersScanIgnoreSystem)
	if ignoreCount > 0 {
		ignoreLabel = fmt.Sprintf("Ignore systems (%d selected)", ignoreCount)
	}

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddNavAction(ignoreLabel, "Systems to ignore exiting in Hold mode", func() {
		buildIgnoreSystemsPage(svc, pages, app)
	})

	menu.AddToggle("Debug logging", "Enable verbose debug output", &debugLogging, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			DebugLogging: &value,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating debug logging")
			ShowErrorModal(pages, app, "Failed to save debug logging setting", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	errorReportingDesc := "Send anonymous crash reports to help improve Zaparoo"
	menu.AddToggle("Error reporting", errorReportingDesc, &errorReporting, func(value bool) {
		// Capture current item index before modal steals focus
		currentIdx := menu.GetCurrentItem()

		if value {
			// Immediately revert the toggle - AddToggle already flipped it
			errorReporting = false
			menu.refreshAllItems(currentIdx)

			// Show confirmation before enabling
			ShowConfirmModal(pages, app,
				"Enable anonymous error reporting?\n\n"+
					"Crash reports help us fix bugs faster.\n"+
					"Reports are anonymized and sent via Sentry. "+
					"No personal data is collected.",
				func() {
					// User confirmed - now enable it
					enabled := true
					ctx, cancel := tuiContext()
					defer cancel()
					err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
						ErrorReporting: &enabled,
					})
					if err != nil {
						log.Warn().Err(err).Msg("error enabling error reporting")
						ShowErrorModal(pages, app, "Failed to save error reporting setting", func() {
							app.SetFocus(menu.List)
						})
						return
					}
					errorReporting = true
					menu.refreshAllItems(currentIdx)
					app.SetFocus(menu.List)
				},
				func() {
					// User cancelled - already reverted, just restore focus
					app.SetFocus(menu.List)
				},
			)
		} else {
			// Disable without confirmation
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
				ErrorReporting: &value,
			})
			if err != nil {
				log.Warn().Err(err).Msg("error disabling error reporting")
				errorReporting = true
				menu.refreshAllItems(currentIdx)
				ShowErrorModal(pages, app, "Failed to save error reporting setting", func() {
					app.SetFocus(menu.List)
				})
			}
		}
	})

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsAdvanced, frame, true)
}

// buildReaderListPage creates the reader list management page.
func buildReaderListPage(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load reader list", func() {
			pages.SwitchToPage(PageSettingsReadersMenu)
		})
		return
	}

	readers := settings.ReadersConnect

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers", "Manage").
		SetHelpText("Select a reader to edit, or use Add/Delete")

	goBack := func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	}
	frame.SetOnEscape(goBack)

	readerList := tview.NewList()
	readerList.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	readerList.ShowSecondaryText(true)
	readerList.SetSelectedFocusOnly(true)
	readerList.SetFocusFunc(func() {
		frame.SetHelpText("Select a reader to edit, or use Add/Delete")
	})
	readerList.SetBlurFunc(func() {
		// Keep help text visible when moving to buttons
	})

	refreshList := func() {
		readerList.Clear()
		for i, reader := range readers {
			idx := i
			display := reader.Driver
			if reader.Path != "" {
				display += ":" + reader.Path
			}
			if !reader.IsEnabled() {
				display += " (disabled)"
			}
			secondary := ""
			if reader.IDSource != "" {
				t := CurrentTheme()
				secondary = fmt.Sprintf("[%s::b]ID Source:[-::-] %s", t.LabelColorName, reader.IDSource)
			}
			readerList.AddItem(display, secondary, 0, func() {
				buildReaderEditPage(cfg, svc, pages, app, pl, &readers, idx)
			})
		}
		if len(readers) == 0 {
			readerList.AddItem("(no readers configured)", "Press Add to create one", 0, nil)
		}
	}
	refreshList()

	buttonBar := NewButtonBar(app)

	buttonBar.AddButtonWithHelp("Add", "Add a new reader connection", func() {
		buildReaderEditPage(cfg, svc, pages, app, pl, &readers, len(readers))
	})

	buttonBar.AddButtonWithHelp("Delete", "Remove the selected reader", func() {
		if len(readers) == 0 {
			return
		}
		idx := readerList.GetCurrentItem()
		if idx >= 0 && idx < len(readers) {
			readerName := readers[idx].Driver
			if readers[idx].Path != "" {
				readerName += ":" + readers[idx].Path
			}
			ShowConfirmModal(pages, app, "Delete reader "+readerName+"?", func() {
				// Create updated slice for API call without modifying original yet
				updatedReaders := slices.Delete(slices.Clone(readers), idx, idx+1)
				ctx, cancel := tuiContext()
				defer cancel()
				err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
					ReadersConnect: &updatedReaders,
				})
				if err != nil {
					log.Warn().Err(err).Msg("error deleting reader")
					ShowErrorModal(pages, app, "Failed to delete reader", func() {
						app.SetFocus(readerList)
					})
					return
				}
				// Only update local slice after successful API call
				readers = updatedReaders
				refreshList()
			}, nil)
		}
	})

	buttonBar.AddButtonWithHelp("Back", "Return to reader settings", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	frame.SetContent(readerList)
	frame.SetButtonBar(buttonBar)
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsReaderList, frame, true)
}

// buildReaderEditPage creates the reader edit form.
func buildReaderEditPage(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	readers *[]models.ReaderConnection,
	index int,
) {
	isNew := index >= len(*readers)
	var reader models.ReaderConnection
	if !isNew {
		reader = (*readers)[index]
	} else {
		reader = models.ReaderConnection{Driver: "pn532"}
	}

	// Get available drivers from platform
	supportedReaders := pl.SupportedReaders(cfg)
	availableDrivers := make([]string, 0, len(supportedReaders))
	for _, r := range supportedReaders {
		availableDrivers = append(availableDrivers, r.Metadata().ID)
	}

	if len(availableDrivers) == 0 {
		ShowErrorModal(pages, app, "No reader drivers available for this platform", func() {
			buildReaderListPage(cfg, svc, pages, app, pl)
		})
		return
	}

	goBack := func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	}

	// Create page frame
	var titlePart string
	if isNew {
		titlePart = "Add"
	} else {
		titlePart = "Edit"
	}
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers", titlePart).
		SetHelpText("Use ←→ to change driver, Tab to move between fields")

	frame.SetOnEscape(goBack)

	driverIndex := 0
	for i, d := range availableDrivers {
		if d == reader.Driver {
			driverIndex = i
			break
		}
	}

	driverDisplay := tview.NewTextView().SetDynamicColors(true)
	updateDriverDisplay := func() {
		t := CurrentTheme()
		driverDisplay.SetText(fmt.Sprintf(
			"[%s::b]Driver:[-::-] < %s >",
			t.LabelColorName, availableDrivers[driverIndex],
		))
	}
	updateDriverDisplay()

	pathInput := tview.NewInputField().
		SetText(reader.Path).
		SetFieldWidth(30)
	SetInputLabel(pathInput, "Path")
	setupInputFieldFocus(pathInput)

	idSourceInput := tview.NewInputField().
		SetText(reader.IDSource).
		SetFieldWidth(20)
	SetInputLabel(idSourceInput, "ID Source")
	setupInputFieldFocus(idSourceInput)

	enabledVal := reader.Enabled == nil || *reader.Enabled
	enabledDisplay := tview.NewTextView().SetDynamicColors(true)
	updateEnabledDisplay := func() {
		t := CurrentTheme()
		label := "Yes"
		if !enabledVal {
			label = "No"
		}
		enabledDisplay.SetText(fmt.Sprintf(
			"[%s::b]Enabled:[-::-] < %s >",
			t.LabelColorName, label,
		))
	}
	updateEnabledDisplay()

	buttonBar := NewButtonBar(app)

	buttonBar.AddButtonWithHelp("Save", "Save reader configuration", func() {
		reader.Driver = availableDrivers[driverIndex]
		reader.Path = pathInput.GetText()
		reader.IDSource = idSourceInput.GetText()
		if !enabledVal {
			f := false
			reader.Enabled = &f
		} else if reader.Enabled != nil && !*reader.Enabled {
			reader.Enabled = nil
		}

		if isNew || index >= len(*readers) {
			*readers = append(*readers, reader)
		} else {
			(*readers)[index] = reader
		}

		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			ReadersConnect: readers,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error saving reader")
			ShowErrorModal(pages, app, "Failed to save reader", func() {
				app.SetFocus(driverDisplay)
			})
			return
		}
		goBack()
	})

	buttonBar.AddButtonWithHelp("Cancel", "Discard changes and go back", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	// Create form content wrapper
	formContent := tview.NewFlex().SetDirection(tview.FlexRow)
	formContent.AddItem(driverDisplay, 1, 0, true)
	formContent.AddItem(pathInput, 1, 0, false)
	formContent.AddItem(idSourceInput, 1, 0, false)
	formContent.AddItem(enabledDisplay, 1, 0, false)

	focusOrder := []tview.Primitive{driverDisplay, pathInput, idSourceInput, enabledDisplay, buttonBar}

	setFocus := func(idx int) {
		if idx < 0 {
			idx = len(focusOrder) - 1
		} else if idx >= len(focusOrder) {
			idx = 0
		}
		app.SetFocus(focusOrder[idx])
	}

	driverDisplay.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyLeft {
			driverIndex = (driverIndex - 1 + len(availableDrivers)) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		}
		if key == tcell.KeyRight {
			driverIndex = (driverIndex + 1) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyEnter || key == tcell.KeyTab {
			setFocus(1)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			frame.FocusButtonBar()
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	pathInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyEnter && config.GetTUIConfig().OnScreenKeyboard {
			ShowOSKModal(
				pages,
				app,
				pathInput.GetText(),
				func(text string) {
					pathInput.SetText(text)
					app.SetFocus(pathInput)
				},
				func() {
					app.SetFocus(pathInput)
				},
			)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			setFocus(0)
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyTab {
			setFocus(2)
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	idSourceInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyEnter && config.GetTUIConfig().OnScreenKeyboard {
			ShowOSKModal(
				pages,
				app,
				idSourceInput.GetText(),
				func(text string) {
					idSourceInput.SetText(text)
					app.SetFocus(idSourceInput)
				},
				func() {
					app.SetFocus(idSourceInput)
				},
			)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			setFocus(1)
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyTab {
			setFocus(3)
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	enabledDisplay.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyLeft || key == tcell.KeyRight {
			enabledVal = !enabledVal
			updateEnabledDisplay()
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyEnter || key == tcell.KeyTab {
			setFocus(4)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			setFocus(2)
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	buttonBar.SetOnUp(func() {
		setFocus(3) // enabledDisplay
	})
	buttonBar.SetOnDown(func() {
		setFocus(0) // driverDisplay (wrap)
	})

	frame.SetContent(formContent)
	frame.SetButtonBar(buttonBar)

	pages.AddAndSwitchToPage(PageSettingsReaderEdit, frame, true)
	app.SetFocus(driverDisplay)
}

// buildIgnoreSystemsPage creates the ignore systems multi-select page.
func buildIgnoreSystemsPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load settings", func() {
			pages.SwitchToPage(PageSettingsAdvanced)
		})
		return
	}

	systems, err := svc.GetSystems(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("error fetching systems")
		ShowErrorModal(pages, app, "Failed to load systems list", func() {
			pages.SwitchToPage(PageSettingsAdvanced)
		})
		return
	}

	items := make([]SystemItem, len(systems))
	for i, sys := range systems {
		label := sys.Name
		if label == "" {
			label = sys.ID
		}
		items[i] = SystemItem{ID: sys.ID, Name: label}
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Advanced", "Ignore Systems").
		SetHelpText("Select systems to ignore during media scanning")

	goBack := func() {
		buildAdvancedSettingsMenu(svc, pages, app)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app)

	var systemSelector *SystemSelector
	systemSelector = NewSystemSelector(&SystemSelectorConfig{
		Mode:     SystemSelectorMulti,
		Systems:  items,
		Selected: settings.ReadersScanIgnoreSystem,
		OnMulti: func(_ []string) {
			// Update button label when selection changes
			count := systemSelector.GetSelectedCount()
			if count > 0 {
				buttonBar.UpdateButtonLabel(0, fmt.Sprintf("Done (%d)", count))
			} else {
				buttonBar.UpdateButtonLabel(0, "Done")
			}
		},
	})

	saveAndExit := func() {
		selected := systemSelector.GetSelected()
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
			ReadersScanIgnoreSystem: &selected,
		})
		if err != nil {
			log.Warn().Err(err).Msg("error updating ignored systems")
			ShowErrorModal(pages, app, "Failed to save ignored systems", func() {
				app.SetFocus(systemSelector)
			})
			return
		}
		buildAdvancedSettingsMenu(svc, pages, app)
	}

	// Update initial button label if items are selected
	count := systemSelector.GetSelectedCount()
	initialLabel := "Done"
	if count > 0 {
		initialLabel = fmt.Sprintf("Done (%d)", count)
	}

	buttonBar.AddButtonWithHelp(initialLabel, "Save ignored systems and return", saveAndExit).
		AddButtonWithHelp("Back", "Discard changes and return", goBack).
		SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})
	frame.SetButtonBar(buttonBar)

	// Setup navigation from list to button bar (with wrap)
	systemSelector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab {
			frame.FocusButtonBar()
			return nil
		}
		if key == tcell.KeyDown {
			if systemSelector.GetCurrentItem() == systemSelector.GetItemCount()-1 {
				frame.FocusButtonBar()
				return nil
			}
		}
		if key == tcell.KeyUp {
			if systemSelector.GetCurrentItem() == 0 {
				frame.FocusButtonBar()
				return nil
			}
		}
		return event
	})

	// Setup navigation from button bar back to list (with wrap and correct position)
	buttonBar.SetOnUp(func() {
		systemSelector.SetCurrentItem(systemSelector.GetItemCount() - 1) // Last item
		app.SetFocus(systemSelector)
	})
	buttonBar.SetOnDown(func() {
		systemSelector.SetCurrentItem(0) // First item (wrap)
		app.SetFocus(systemSelector)
	})

	frame.SetContent(systemSelector)
	pages.AddAndSwitchToPage(PageSettingsIgnoreSystems, frame, true)
}

// buildAboutPage creates the About page with version, license, and credits.
func buildAboutPage(pages *tview.Pages, app *tview.Application) {
	frame := NewPageFrame(app).
		SetTitle("About")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	t := CurrentTheme()

	content := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false)

	aboutText := fmt.Sprintf(`[%s::b]Zaparoo Core[-::-]
Version %s

[%s::b]Copyright[-::-]
© %d The Zaparoo Project Contributors

[%s::b]License[-::-]
GNU General Public License v3.0 or later (GPL-3.0-or-later)

This is free software: you are free to change and redistribute it.
There is NO WARRANTY, to the extent permitted by law.`,
		t.AccentColorName,
		config.AppVersion,
		t.AccentColorName,
		time.Now().Year(),
		t.AccentColorName,
	)

	content.SetText(aboutText)

	frame.SetContent(content)
	pages.AddAndSwitchToPage(PageSettingsAbout, frame, true)
	app.SetFocus(buttonBar)
}
