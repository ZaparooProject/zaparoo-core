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
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const clientRoleModalPage = "client_role_modal"

func formatPairingPIN(pin string) string {
	if len(pin) != 6 {
		return pin
	}
	return pin[:3] + " " + pin[3:]
}

func formatPairingRole(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func formatPairingCountdown(expiresAt, now time.Time) string {
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return "0:00"
	}
	seconds := int64((remaining + time.Second - 1) / time.Second)
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func showClientPairingModal(
	pages *tview.Pages,
	app *tview.Application,
	pairing *models.ClientsPairStartResponse,
	role string,
	onDone func(),
) {
	expiresAt := time.Unix(pairing.ExpiresAt, 0)
	message := func(now time.Time) string {
		return fmt.Sprintf("Pairing PIN: %s\nRole: %s\nExpires in: %s\n\nEnter this PIN in the client app.",
			formatPairingPIN(pairing.PIN), formatPairingRole(role), formatPairingCountdown(expiresAt, now))
	}

	done := make(chan struct{})
	modal := ShowInfoModal(pages, app, "Pair Client", message(time.Now()), func() {
		close(done)
		if onDone != nil {
			onDone()
		}
	})

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				app.QueueUpdateDraw(func() {
					modal.SetText(message(now))
				})
				if !now.Before(expiresAt) {
					return
				}
			case <-done:
				return
			}
		}
	}()
}

func startClientPairing(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	role string,
	onDone func(),
) {
	ctx, cancel := tuiContext()
	pairing, err := svc.StartClientPairing(ctx, role)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to start pairing: "+err.Error(), onDone)
		return
	}
	showClientPairingModal(pages, app, pairing, role, onDone)
}

func showClientRolePicker(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	profiles []models.ProfileResponse,
	onDone func(),
) {
	menu := NewSettingsList(pages, PageClients)
	cleanup := func() {
		pages.HidePage(clientRoleModalPage)
		pages.RemovePage(clientRoleModalPage)
	}
	menu.SetRebuildPrevious(func() {
		cleanup()
		onDone()
	})
	menu.SetDynamicHelpMode(true)
	menu.AddAction("Member", "Day-to-day access without management permissions", func() {
		cleanup()
		startClientPairing(svc, pages, app, "member", onDone)
	})
	menu.AddAction("Admin", "Full settings and profile management access", func() {
		promptProfileManagement(svc, pages, app, profiles, func() {
			cleanup()
			startClientPairing(svc, pages, app, "admin", onDone)
		}, func() {
			app.SetFocus(menu.List)
		})
	})
	menu.SetBorder(true)
	SetBoxTitle(menu.Box, "Client Role")
	pages.AddPage(clientRoleModalPage, CenterWidget(42, 4, menu.List), true, true)
	app.SetFocus(menu.List)
}

// BuildClientsPage manages paired client devices and local pairing approval.
func BuildClientsPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	clientsResp, err := svc.GetClients(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load paired clients", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}
	ctx, cancel = tuiContext()
	settings, err := svc.GetSettings(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load encryption setting", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}
	ctx, cancel = tuiContext()
	profilesResp, profilesErr := svc.GetProfiles(ctx)
	cancel()
	if profilesErr != nil {
		profilesResp = &models.ProfilesResponse{}
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Clients")
	goBack := func() { pages.SwitchToPage(PageSettingsMain) }
	frame.SetOnEscape(goBack)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetRebuildPrevious(goBack)
	menu.SetDynamicHelpMode(true).SetHelpCallback(func(text string) {
		frame.SetHelpText(text)
	})

	rebuild := func() { BuildClientsPage(svc, pages, app) }
	encryption := settings.Encryption
	menu.AddToggle("Require encryption",
		"Require paired encryption remotely; local connections remain allowed",
		&encryption, func(value bool) {
			currentIdx := menu.GetCurrentItem()
			if value {
				encryption = false
				menu.refreshAllItems(currentIdx)
				ShowConfirmModal(pages, app,
					"Require encrypted remote connections?\n\n"+
						"Unpaired remote clients will disconnect. Local connections remain available.",
					func() {
						enabled := true
						ctx, cancel := tuiContext()
						err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{Encryption: &enabled})
						cancel()
						if err != nil {
							ShowErrorModal(pages, app, "Failed to require encryption", func() {
								app.SetFocus(menu.List)
							})
							return
						}
						encryption = true
						menu.refreshAllItems(currentIdx)
						app.SetFocus(menu.List)
					}, func() {
						app.SetFocus(menu.List)
					})
				return
			}

			ctx, cancel := tuiContext()
			err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{Encryption: &value})
			cancel()
			if err != nil {
				encryption = true
				menu.refreshAllItems(currentIdx)
				ShowErrorModal(pages, app, "Failed to allow plaintext connections", func() {
					app.SetFocus(menu.List)
				})
			}
		})

	for i := range clientsResp.Clients {
		paired := clientsResp.Clients[i]
		menu.AddAction(paired.ClientName+" ("+paired.Role+")", "Revoke this paired client", func() {
			promptProfileManagement(svc, pages, app, profilesResp.Profiles, func() {
				ShowConfirmModal(pages, app, "Revoke "+paired.ClientName+"?", func() {
					ctx, cancel := tuiContext()
					err := svc.DeleteClient(ctx, paired.ClientID)
					cancel()
					if err != nil {
						ShowErrorModal(pages, app, "Failed to revoke client: "+err.Error(), nil)
						return
					}
					rebuild()
				}, nil)
			}, func() {
				app.SetFocus(menu.List)
			})
		})
	}
	if len(clientsResp.Clients) == 0 {
		menu.AddAction("(no paired clients)", "First paired client will be administrator", func() {})
	}

	buttonBar := NewButtonBar(app)
	buttonBar.AddButtonWithHelp("Pair", "Approve a new client pairing", func() {
		if len(clientsResp.Clients) == 0 {
			ShowConfirmModal(pages, app,
				"The first paired client receives administrator access. Continue?",
				func() {
					startClientPairing(svc, pages, app, "admin", rebuild)
				}, nil)
			return
		}
		showClientRolePicker(svc, pages, app, profilesResp.Profiles, rebuild)
	})
	buttonBar.AddButtonWithHelp("Cancel Pair", "Cancel any active pairing approval", func() {
		ctx, cancel := tuiContext()
		err := svc.CancelClientPairing(ctx)
		cancel()
		if err != nil {
			ShowErrorModal(pages, app, "Failed to cancel pairing", nil)
			return
		}
		ShowInfoModal(pages, app, "Pairing", "Pairing approval cancelled.", rebuild)
	})
	buttonBar.AddButtonWithHelp("Back", "Return to settings", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(text string) {
		frame.SetHelpText(text)
	})
	frame.SetButtonBar(buttonBar)
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageClients, frame, true)
	app.SetFocus(menu.List)
	log.Debug().Int("clients", len(clientsResp.Clients)).Msg("built paired clients page")
}
