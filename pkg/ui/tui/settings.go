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
	"strings"
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
		AddNavAction("Profiles", "Profile-wide launch behavior", func() {
			buildProfilesSettingsMenu(svc, pages, app)
		}).
		AddNavAction("Clients", "Pair and revoke client devices", func() {
			BuildClientsPage(svc, pages, app)
		}).
		AddNavAction("Backup", "Back up and restore this device", func() {
			buildBackupSettingsMenu(svc, pages, app, func() { pages.SwitchToPage(PageSettingsMain) })
		}).
		AddNavAction("Online", "Zaparoo Online account and cloud features", func() {
			buildOnlineSettingsMenu(svc, pages, app, func() { pages.SwitchToPage(PageSettingsMain) })
		}).
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

// buildProfilesSettingsMenu creates profile-wide behavior settings.
func buildProfilesSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	settings, err := svc.GetSettings(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load profile settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}
	ctx, cancel = tuiContext()
	profiles, err := svc.GetProfiles(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load profiles", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Profiles")
	goBack := func() { pages.SwitchToPage(PageSettingsMain) }
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	requireForLaunch := settings.ProfilesRequireForLaunch
	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
	menu.AddToggle(
		"Require profile for launch",
		"Block launches until a profile is active; switch cards still work",
		&requireForLaunch,
		func(value bool) {
			restore := func() {
				requireForLaunch = !value
				menu.refreshAllItems(menu.GetCurrentItem())
				app.SetFocus(menu.List)
			}
			promptProfileManagement(svc, pages, app, profiles.Profiles, func() {
				ctx, cancel := tuiContext()
				err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{
					ProfilesRequireForLaunch: &value,
				})
				cancel()
				if err != nil {
					restore()
					ShowErrorModal(pages, app, "Failed to save profile settings", func() {
						app.SetFocus(menu.List)
					})
					return
				}
				app.SetFocus(menu.List)
			}, restore)
		},
	)

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsProfiles, frame, true)
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

// buildBackupSettingsMenu loads backup status in the background, then shows
// the backup settings submenu. goBack runs when the page is dismissed, so
// the page returns to wherever it was opened from.
func buildBackupSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	loadSettingsPage(pages, app, PageSettingsBackup,
		[]string{"Settings", "Backup"},
		"Loading backup status...",
		"Failed to load backup status",
		tuiContext, goBack,
		func(ctx context.Context) (*models.BackupStatusResponse, error) {
			status, err := svc.GetBackupStatus(ctx)
			if err != nil {
				return nil, fmt.Errorf("get backup status: %w", err)
			}
			return status, nil
		},
		func(status *models.BackupStatusResponse) {
			renderBackupSettingsMenu(svc, pages, app, status, goBack)
		},
	)
}

// renderBackupSettingsMenu shows the backup settings submenu: a Local
// section for portable ZIP backups and a Cloud section for Zaparoo Online
// backups (or a pointer to account linking when no account is linked).
func renderBackupSettingsMenu(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	status *models.BackupStatusResponse,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle("Settings", "Backup")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain).SetRebuildPrevious(goBack)
	menu.SetDynamicHelpMode(true).SetHelpCallback(func(desc string) { frame.SetHelpText(desc) })
	menu.SetOnNavigateOut(frame.FocusButtonBar)

	rebuild := func() { buildBackupSettingsMenu(svc, pages, app, goBack) }

	if status.ActiveOperation != "" {
		description := "Backup service is busy"
		if status.ActiveSince != nil {
			description += " since " + *status.ActiveSince
		}
		menu.AddNavAction("Active operation", description, func() {
			ShowInfoModal(
				pages, app, "Backup in progress", status.ActiveOperation+" is currently running.",
				func() { app.SetFocus(menu.List) },
			)
		})
	}

	menu.AddHeader("Local")
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
				ShowInfoModal(pages, app, "Backup created", label, rebuild)
			},
		)
	})
	menu.AddNavAction("View backups", backupStatusDescription(&status.Local), func() {
		buildBackupListPage(svc, pages, app, rebuild)
	})

	menu.AddHeader("Cloud")
	if status.Remote.Linked {
		addCloudBackupItems(svc, pages, app, menu, status, rebuild)
	} else {
		menu.AddNavAction("Link account", "Link a Zaparoo Online account to enable cloud backup", func() {
			buildOnlineSettingsMenu(svc, pages, app, rebuild)
		})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsBackup, frame, true)
}

// addCloudBackupItems adds the Cloud section items available while a
// Zaparoo Online account is linked.
func addCloudBackupItems(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	menu *SettingsList,
	status *models.BackupStatusResponse,
	rebuild func(),
) {
	enabled := status.Remote.Enabled
	menu.AddToggle(
		"Automatic backup", "Back up this device to the cloud on a schedule", &enabled,
		func(value bool) {
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{BackupRemoteEnabled: &value})
			if err != nil {
				log.Warn().Err(err).Msg("error updating cloud backup setting")
				ShowErrorModal(
					pages, app, "Failed to save cloud backup setting", func() { app.SetFocus(menu.List) },
				)
			}
		},
	)
	scheduleOptions := []string{"daily", "weekly", "manual"}
	scheduleIndex := 0
	for i, option := range scheduleOptions {
		if option == status.Remote.Schedule {
			scheduleIndex = i
			break
		}
	}
	menu.AddCycle(
		"Schedule",
		"How often automatic cloud backup runs",
		scheduleOptions,
		&scheduleIndex,
		func(value string, _ int) {
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{BackupRemoteSchedule: &value})
			if err != nil {
				log.Warn().Err(err).Msg("error updating cloud backup schedule")
				ShowErrorModal(pages, app, "Failed to save cloud backup schedule", func() {
					app.SetFocus(menu.List)
				})
			}
		},
	)
	cloudUploadDescription := "Upload a backup of this device to the cloud"
	if status.Remote.Availability == "unavailable" {
		cloudUploadDescription = "Warp is required to create cloud backups"
	}
	menu.AddNavAction("Back up now", cloudUploadDescription, func() {
		if status.Remote.Availability == "unavailable" {
			ShowInfoModal(
				pages, app, "Cloud backup unavailable",
				"Cloud upload requires an active Zaparoo Warp subscription. "+
					"Existing cloud backups can still be restored.",
				func() { app.SetFocus(menu.List) },
			)
			return
		}
		runBackupAction(
			pages,
			app,
			menu.List,
			"Creating cloud backup",
			"Uploading backup to the cloud...",
			func(ctx context.Context) (string, error) {
				id, backupErr := svc.RunRemoteBackup(ctx)
				if backupErr != nil {
					return "", fmt.Errorf("create cloud backup: %w", backupErr)
				}
				return "Cloud backup " + id, nil
			},
			func(label string) {
				ShowInfoModal(pages, app, "Cloud backup created", label, rebuild)
			},
		)
	})
	menu.AddNavAction("View backups", "List and restore cloud backup snapshots", func() {
		buildRemoteBackupListPage(svc, pages, app, rebuild)
	})
	menu.AddNavAction("Status", backupStatusDescription(&status.Remote), func() {
		ShowInfoModal(pages, app, "Cloud backup", backupStatusText(&status.Remote), func() {
			app.SetFocus(menu.List)
		})
	})
}

const (
	backupProgressModalPage = "backup_progress_modal"
	// Sized to fit the five-line progress text plus border within the
	// 75-column CRT view.
	backupProgressModalWidth  = 51
	backupProgressModalHeight = 7
)

func backupActionErrorText(title string, err error) string {
	message := strings.ToLower(err.Error())
	var guidance string
	switch {
	case strings.Contains(message, "backup operation ") && strings.Contains(message, " has been running since "):
		guidance = "Another backup or restore is already running. Wait for it to finish, then try again."
	case strings.Contains(message, "cannot restore backup while media is active"):
		guidance = "Stop active media before restoring this backup."
	case strings.Contains(message, "cannot restore backup while media is launching") ||
		strings.Contains(message, "media launch is in progress"):
		guidance = "Wait for media launch to finish, then try restoring again."
	case strings.Contains(message, "full-device backup is not supported on this platform"):
		guidance = "Full-device backup is not available on this platform."
	case strings.Contains(message, "remote backup is not available for this account"):
		guidance = "Cloud backup requires an active Zaparoo Warp subscription."
	case strings.Contains(message, "backup restore rollback requires recovery") ||
		strings.Contains(message, "pending backup restore transaction exists"):
		guidance = "Restart Zaparoo Core to complete restore recovery, then try again."
	case strings.Contains(message, "backup restore restart is pending"):
		guidance = "Restart Zaparoo Core before starting another backup or restore operation."
	case strings.Contains(message, "remote backup rate limited"):
		guidance = "A backup was uploaded recently. Wait a few minutes, then try again."
	case strings.Contains(message, "remote backup is unlinked") || strings.Contains(message, "device not linked"):
		guidance = "Relink this device to Zaparoo Online, then try again."
	case strings.Contains(message, "insufficient disk space"):
		guidance = "Free storage space on this device, then try again."
	default:
		guidance = "Check Core logs for details, then try again."
	}
	return title + " failed.\n\n" + guidance
}

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
	done := make(chan struct{})

	// Deliberately blocking: no buttons, and all input is swallowed.
	// Backups and restores are foreground operations whose outcome must
	// always be reported, and a restore ends in a Core restart. A plain
	// bordered TextView is used instead of tview.Modal so no empty button
	// row is reserved.
	modal := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(backupProgressText(message, started))
	modal.SetInputCapture(func(_ *tcell.EventKey) *tcell.EventKey { return nil })
	modal.SetBorder(true)
	SetBoxTitle(modal.Box, title)
	modal.SetTitleAlign(tview.AlignCenter)
	pages.AddPage(backupProgressModalPage,
		CenterWidget(backupProgressModalWidth, backupProgressModalHeight, modal), true, true)
	app.SetFocus(modal)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
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
			if err != nil {
				log.Warn().Err(err).Msg("error running backup action")
				ShowErrorModal(pages, app, backupActionErrorText(title, err), func() { app.SetFocus(focus) })
				return
			}
			onSuccess(label)
		})
	}()
}

func backupProgressText(message string, started time.Time) string {
	return fmt.Sprintf(
		"%s\n\nElapsed: %s\n\nTime depends on save data size.",
		message,
		time.Since(started).Round(time.Second),
	)
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

	done := make(chan struct{})
	closeModal := func() {
		pages.HidePage(authLinkModalPage)
		pages.RemovePage(authLinkModalPage)
	}

	dialog := NewDialog().
		SetText(authLinkMessage(link.VerificationURL, link.UserCode)).
		SetTextAlign(tview.AlignLeft).
		SetTitle("Link with Zaparoo Online").
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(_ int) {
			close(done)
			cancelCtx, cancelCancel := tuiContext()
			defer cancelCancel()
			if cancelErr := svc.CancelAuthLink(cancelCtx); cancelErr != nil {
				log.Debug().Err(cancelErr).Msg("error cancelling device link")
			}
			closeModal()
			onDone()
		})
	pages.AddPage(authLinkModalPage, dialog, true, true)
	app.SetFocus(dialog)

	go pollAuthLinkStatus(svc, pages, app, done, closeModal, onDone)
}

// authLinkMessage lays out the link instructions so the two things the user
// must act on — the address and the code — stand out from the fixed text.
func authLinkMessage(verificationURL, userCode string) string {
	t := CurrentTheme()
	accent := colorToHex(t.LabelColor)
	dim := colorToHex(t.SecondaryTextColor)
	displayURL := strings.TrimPrefix(strings.TrimPrefix(verificationURL, "https://"), "http://")
	return fmt.Sprintf(
		"On your phone or computer, open:\n  [%s::b]%s[-::-]\n\n"+
			"and enter this code:\n  [%s::b]%s[-::-]\n\n"+
			"[%s]Waiting for approval...[-::-]",
		accent, displayURL, accent, userCode, dim,
	)
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

func buildBackupListPage(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	loadSettingsPage(pages, app, PageSettingsBackupList,
		[]string{"Settings", "Backup", "Local"},
		"Loading backups...",
		"Failed to list backups",
		tuiContext, goBack,
		func(ctx context.Context) ([]map[string]any, error) {
			backups, err := svc.ListBackups(ctx)
			if err != nil {
				return nil, fmt.Errorf("list backups: %w", err)
			}
			return backups, nil
		},
		func(backups []map[string]any) {
			renderBackupListPage(svc, pages, app, backups, goBack)
		},
	)
}

func renderBackupListPage(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	backups []map[string]any,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle("Settings", "Backup", "Local")
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
	showBackupModal(pages, app, "Backup details", "Loading backup manifest...", []string{"Back"}, func(_ int) {
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
					"Backup details",
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
		"Backup details",
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
	dialog := NewDialog().
		SetText(message).
		SetTitle(title).
		AddButtons(buttons).
		SetDoneFunc(func(buttonIndex int) {
			pages.HidePage(backupManageModalPage)
			pages.RemovePage(backupManageModalPage)
			onDone(buttonIndex)
		})
	pages.RemovePage(backupManageModalPage)
	pages.AddPage(backupManageModalPage, dialog, true, true)
	app.SetFocus(dialog)
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

const (
	coreRestartModalPage    = "core_restart_modal"
	coreRestartPollInterval = time.Second
	// coreRestartNoDownGrace ends the wait when the API never drops: the
	// restart finished before polling started, or is not coming.
	coreRestartNoDownGrace = 15 * time.Second
	coreRestartTimeout     = 90 * time.Second
)

// waitForCoreRestart blocks the UI while Core restarts after a restore.
// The service restarts a few seconds after the restore response, so wait
// for the API to drop and come back before running onDone; if it never
// drops, give up waiting after a grace period. onDone always runs.
func waitForCoreRestart(svc SettingsService, pages *tview.Pages, app *tview.Application, onDone func()) {
	waitForCoreRestartWith(
		svc, pages, app, coreRestartPollInterval, coreRestartNoDownGrace, coreRestartTimeout, onDone,
	)
}

func waitForCoreRestartWith(
	svc SettingsService, pages *tview.Pages, app *tview.Application,
	pollInterval, noDownGrace, timeout time.Duration, onDone func(),
) {
	modal := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Core is restarting.\n\nWaiting for it to come back...")
	modal.SetInputCapture(func(_ *tcell.EventKey) *tcell.EventKey { return nil })
	modal.SetBorder(true)
	SetBoxTitle(modal.Box, "Restore")
	modal.SetTitleAlign(tview.AlignCenter)
	pages.AddPage(coreRestartModalPage, CenterWidget(45, 5, modal), true, true)
	app.SetFocus(modal)

	go func() {
		started := time.Now()
		sawDown := false
		cameBack := false
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := tuiContext()
			_, err := svc.GetSettings(ctx)
			cancel()
			elapsed := time.Since(started)
			if err != nil {
				sawDown = true
				if elapsed < timeout {
					continue
				}
			} else {
				if !sawDown && elapsed < noDownGrace {
					continue
				}
				cameBack = true
			}
			break
		}
		app.QueueUpdateDraw(func() {
			pages.HidePage(coreRestartModalPage)
			pages.RemovePage(coreRestartModalPage)
			// End with a modal that requires an explicit OK: an
			// auto-dismissing wait hands focus straight to the rebuilt
			// menu, where a stray Enter lands on its first action.
			message := "Core restarted successfully."
			if !cameBack {
				message = "Core has not come back yet.\nCheck the service status before continuing."
			}
			ShowInfoModal(pages, app, "Restore", message, onDone)
		})
	}()
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
	ShowConfirmModal(
		pages, app,
		"Restore "+label+"?\n\nCore will restart after restore.",
		func() {
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
					ShowInfoModal(pages, app, "Backup restored", restoredLabel, func() {
						waitForCoreRestart(svc, pages, app, onRestored)
					})
				},
			)
		}, func() { app.SetFocus(focus) })
}

func buildRemoteBackupListPage(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	loadSettingsPage(pages, app, PageSettingsBackupList,
		[]string{"Settings", "Backup", "Cloud"},
		"Loading cloud backups...",
		"Failed to list cloud backups",
		backupContext, goBack,
		func(ctx context.Context) ([]map[string]any, error) {
			backups, err := svc.ListRemoteBackups(ctx)
			if err != nil {
				return nil, fmt.Errorf("list cloud backups: %w", err)
			}
			return backups, nil
		},
		func(backups []map[string]any) {
			renderRemoteBackupListPage(svc, pages, app, backups, goBack)
		},
	)
}

func renderRemoteBackupListPage(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	backups []map[string]any,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle("Settings", "Backup", "Cloud")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(true)
	list.SetSelectedFocusOnly(true)
	for _, backup := range backups {
		id := backupString(backup, "id")
		if id == "" {
			continue
		}
		label := backupDisplayLabel("Cloud backup", "", backupString(backup, "createdAt"))
		if label == "Cloud backup" {
			label = "Cloud backup " + id
		}
		secondary := "ID " + id
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
		list.AddItem("(no cloud backups found)", "Create a cloud backup first", 0, nil)
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
	backupID string,
	onRestored func(),
) {
	ShowConfirmModal(
		pages, app,
		"Restore cloud backup "+backupID+"?\n\nCore will restart after restore.",
		func() {
			runBackupAction(
				pages,
				app,
				focus,
				"Restoring cloud backup",
				"Downloading and restoring cloud backup...",
				func(ctx context.Context) (string, error) {
					if err := svc.RestoreRemoteBackup(ctx, backupID); err != nil {
						return "", fmt.Errorf("restore cloud backup: %w", err)
					}
					return backupID, nil
				},
				func(restoredLabel string) {
					ShowInfoModal(pages, app, "Cloud backup restored", restoredLabel, func() {
						waitForCoreRestart(svc, pages, app, onRestored)
					})
				},
			)
		}, func() { app.SetFocus(focus) })
}

func backupStatusDescription(status *models.BackupStatusEntry) string {
	if status.Availability == "unavailable" {
		return "Warp unavailable; restore remains available"
	}
	if status.LastStatus == "partial" {
		return fmt.Sprintf("Completed with %d warning(s)", status.SkippedFiles)
	}
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
	if status.Availability != "" {
		lines = append(lines, "Warp availability: "+status.Availability)
	}
	if status.AvailabilityCheckedAt != nil {
		lines = append(lines, "Availability checked: "+*status.AvailabilityCheckedAt)
	}
	if status.LastSuccessAt != nil {
		lines = append(lines, "Last success: "+*status.LastSuccessAt)
	}
	if status.LastError != "" {
		lines = append(lines, "Last error: "+status.LastError)
	}
	if status.SkippedFiles > 0 {
		lines = append(lines, fmt.Sprintf("Skipped paths: %d", status.SkippedFiles))
	}
	for _, warning := range status.Warnings {
		lines = append(lines, "Warning: "+warning.Path+" ("+warning.Reason+")")
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
	if errText := backupString(backup, "error"); errText != "" {
		lines = append(lines, "Error: "+errText)
	}
	if categoryLines := formatBackupCategories(backup); len(categoryLines) > 0 {
		lines = append(lines, "", "Manifest:")
		lines = append(lines, categoryLines...)
	}
	if warningLines := formatBackupWarnings(backup); len(warningLines) > 0 {
		lines = append(lines, "", "Warnings:")
		lines = append(lines, warningLines...)
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

func formatBackupWarnings(backup map[string]any) []string {
	raw, ok := backup["warnings"].([]any)
	if !ok {
		return nil
	}
	lines := make([]string, 0, len(raw))
	for _, item := range raw {
		warning, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := backupString(warning, "path")
		reason := backupString(warning, "reason")
		if path == "" || reason == "" {
			continue
		}
		lines = append(lines, path+" ("+reason+")")
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
