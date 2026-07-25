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
	"net/url"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// onlinePageData bundles the API responses the online settings page renders.
type onlinePageData struct {
	status   *models.BackupStatusResponse
	settings *models.SettingsResponse
}

// onlineServerHost returns the backup server host to display when a custom
// server is configured, or "" when using the official default.
func onlineServerHost(settings *models.SettingsResponse) string {
	if settings == nil || settings.BackupRemoteBaseURL == nil {
		return ""
	}
	base := *settings.BackupRemoteBaseURL
	if base == "" || strings.EqualFold(base, config.DefaultBackupRemoteBaseURL) {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return base
	}
	return parsed.Host
}

// buildOnlineSettingsMenu loads account status in the background, then shows
// the Zaparoo Online settings page. goBack runs when the page is dismissed.
func buildOnlineSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	loadSettingsPage(pages, app, PageSettingsOnline,
		[]string{"Settings", "Online"},
		"Loading online status...",
		"Failed to load online status",
		tuiContext, goBack,
		func(ctx context.Context) (*onlinePageData, error) {
			status, err := svc.GetBackupStatus(ctx)
			if err != nil {
				return nil, fmt.Errorf("get backup status: %w", err)
			}
			settings, err := svc.GetSettings(ctx)
			if err != nil {
				return nil, fmt.Errorf("get settings: %w", err)
			}
			return &onlinePageData{status: status, settings: settings}, nil
		},
		func(data *onlinePageData) {
			renderOnlineSettingsMenu(svc, pages, app, data, goBack)
		},
	)
}

// renderOnlineSettingsMenu shows the Zaparoo Online page: an Account section
// with link status shown directly on the menu lines, and a Features section
// pointing at the cloud features an account unlocks.
func renderOnlineSettingsMenu(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	data *onlinePageData,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle("Settings", "Online")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain).SetRebuildPrevious(goBack)
	menu.SetDynamicHelpMode(true).SetHelpCallback(func(desc string) { frame.SetHelpText(desc) })
	menu.SetOnNavigateOut(frame.FocusButtonBar)

	rebuild := func() { buildOnlineSettingsMenu(svc, pages, app, goBack) }
	status := data.status
	serverHost := onlineServerHost(data.settings)

	menu.AddHeader("Account")
	if status.Remote.Linked {
		addOnlineAccountItems(svc, pages, app, menu, status, serverHost, rebuild)
	} else {
		menu.AddValueAction("Account",
			"Link a Zaparoo Online account to unlock cloud features",
			func() string { return "Not linked" }, nil)
		linkDesc := "Connect this device to your Zaparoo Online account"
		if serverHost != "" {
			linkDesc = "Connect this device to " + serverHost
		}
		menu.AddNavAction("Link account", linkDesc, func() {
			startAuthLinkFlow(svc, pages, app, rebuild)
		})
	}

	menu.AddHeader("Features")
	playtimeSyncEnabled := false
	if data.settings != nil && data.settings.PlaytimeSyncEnabled != nil {
		playtimeSyncEnabled = *data.settings.PlaytimeSyncEnabled
	}
	playtimeSyncDesc := "Upload play history to your linked Zaparoo Online account"
	if !status.Remote.Linked {
		playtimeSyncDesc = "Upload play history when this device is linked to Zaparoo Online"
	}
	menu.AddToggle("Play history sync", playtimeSyncDesc, &playtimeSyncEnabled, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		if err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{PlaytimeSyncEnabled: &value}); err != nil {
			playtimeSyncEnabled = !value
			menu.refreshAllItems(menu.GetCurrentItem())
			log.Warn().Err(err).Msg("error updating play history sync setting")
			ShowErrorModal(pages, app, "Failed to save play history sync setting", func() {
				app.SetFocus(menu.List)
			})
		}
	})
	cloudDesc := "Create, restore, and schedule cloud backups of this device"
	if !status.Remote.Linked {
		cloudDesc = "Keep this device backed up to the cloud — included with Zaparoo Warp"
	}
	menu.AddNavAction("Cloud backup", cloudDesc, func() {
		buildBackupSettingsMenu(svc, pages, app, rebuild)
	})

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsOnline, frame, true)
}

// addOnlineAccountItems adds the Account section items available while an
// account is linked: link status, Warp subscription state, and unlink.
func addOnlineAccountItems(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	menu *SettingsList,
	status *models.BackupStatusResponse,
	serverHost string,
	rebuild func(),
) {
	deviceName := ""
	if status.Remote.DeviceName != nil {
		deviceName = *status.Remote.DeviceName
	}
	accountValue := "Linked"
	if deviceName != "" {
		accountValue = "Linked as " + deviceName
	}
	accountDesc := "This device is linked to Zaparoo Online"
	if serverHost != "" {
		accountDesc = "This device is linked to " + serverHost
	}
	accountDetail := accountDesc + "."
	if deviceName != "" {
		accountDetail += "\n\nDevice name: " + deviceName
	}
	if since := formatLinkedSince(status.Remote.LinkedAt); since != "" {
		accountDetail += "\nLinked since: " + since
	}
	menu.AddValueAction("Account", accountDesc, func() string { return accountValue }, func() {
		ShowInfoModal(pages, app, "Zaparoo Online", accountDetail, func() {
			app.SetFocus(menu.List)
		})
	})

	var warpValue, warpDetail string
	switch status.Remote.Availability {
	case "available":
		warpValue = "Active"
		warpDetail = "Your Zaparoo Warp subscription is active.\n\n" +
			"Cloud backup and other premium features are enabled."
	case "unavailable":
		warpValue = "Not active"
		warpDetail = "Cloud backup uploads require an active\nZaparoo Warp subscription.\n\n" +
			"Existing cloud backups can still be restored."
	default:
		warpValue = "Checking..."
		warpDetail = "Warp subscription status is being checked.\n\n" +
			"It refreshes automatically in the background."
	}
	menu.AddValueAction("Warp", "Premium subscription powering cloud features",
		func() string { return warpValue }, func() {
			ShowInfoModal(pages, app, "Zaparoo Warp", warpDetail, func() {
				app.SetFocus(menu.List)
			})
		})

	menu.AddNavAction("Unlink account", "Remove this device's Zaparoo Online credentials", func() {
		ShowConfirmModal(pages, app,
			"Unlink from Zaparoo Online?\n\nAutomatic cloud backups will stop\nuntil you link this device again.",
			func() {
				ctx, cancel := tuiContext()
				err := svc.Unlink(ctx)
				cancel()
				if err != nil {
					log.Warn().Err(err).Msg("error unlinking from Zaparoo Online")
					ShowErrorModal(pages, app, "Failed to unlink", func() {
						app.SetFocus(menu.List)
					})
					return
				}
				ShowInfoModal(pages, app, "Unlinked",
					"This device's Zaparoo Online\ncredentials were removed.", rebuild)
			},
			func() { app.SetFocus(menu.List) },
		)
	})
}

// formatLinkedSince renders a stored link timestamp as a short date, or ""
// when missing or unparseable.
func formatLinkedSince(linkedAt *string) string {
	if linkedAt == nil || *linkedAt == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, *linkedAt)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2 Jan 2006")
}
