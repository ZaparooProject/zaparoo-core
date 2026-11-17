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
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	PageSettingsKiosk        = "settings_kiosk"
	PageSettingsKioskContent = "settings_kiosk_content"
	PageSettingsKioskProfile = "settings_kiosk_profile"
	PageSettingsKioskSystems = "settings_kiosk_systems"
)

// kioskLocked reports whether a lock state is an enforcing lock.
func kioskLocked(state *models.KioskResponse) bool {
	return state != nil && state.State == string(kiosk.StateLocked)
}

func kioskActive(state *models.KioskResponse) bool {
	return state != nil && state.State != string(kiosk.StateOff)
}

// kioskSealText says, in plain words, how far the device's own interface is
// closed. A lock that leaves the host reachable is said to do so.
func kioskSealText(seal *models.KioskSeal) string {
	if seal.Sealed {
		return "Sealed: nothing outside Zaparoo can be reached on this device"
	}
	reasons := make([]string, 0, len(seal.Unsealed))
	for _, reason := range seal.Unsealed {
		switch reason {
		case platforms.KioskUnsealedHostMenus:
			reasons = append(reasons, "this device's own menus are not locked")
		case platforms.KioskUnsealedMainLock:
			reasons = append(reasons, "this MiSTer's menu cannot be locked")
		case platforms.KioskUnsealedMainControls:
			reasons = append(reasons, "keyboard and button shortcuts can still leave a game")
		case platforms.KioskUnsealedFrontend:
			reasons = append(reasons, "the frontend still shows its settings")
		case platforms.KioskUnsealedIdleReset:
			reasons = append(reasons, "idle reset is not supported here")
		default:
			reasons = append(reasons, reason)
		}
	}
	return "Zaparoo is locked, but " + strings.Join(reasons, "; ")
}

func kioskStateText(state *models.KioskResponse) string {
	switch state.State {
	case string(kiosk.StateLocked):
		return "Locked"
	case string(kiosk.StateUnlocked):
		if state.UnlockUntil != nil {
			if until, err := time.Parse(time.RFC3339, *state.UnlockUntil); err == nil {
				return "Unlocked until " + until.In(time.Now().Location()).Format("15:04")
			}
		}
		return "Unlocked"
	default:
		return "Off"
	}
}

// promptKioskUnlock asks for an admin profile's PIN and unlocks with it.
// Core checks the PIN; nothing here decides whether it was right.
func promptKioskUnlock(
	svc SettingsService, pages *tview.Pages, app *tview.Application, onUnlocked, onCancel func(),
) {
	ctx, cancel := tuiContext()
	profiles, err := svc.GetProfiles(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load profiles", onCancel)
		return
	}
	admins := make([]models.ProfileResponse, 0, len(profiles.Profiles))
	for i := range profiles.Profiles {
		if profiles.Profiles[i].Role == "admin" {
			admins = append(admins, profiles.Profiles[i])
		}
	}
	if len(admins) == 0 {
		ShowErrorModal(pages, app, "No administrator profile exists", onCancel)
		return
	}

	unlockAs := func(admin *models.ProfileResponse) {
		showProfilePINModal(pages, app, admin.Name, func(pin string) {
			unlockCtx, unlockCancel := tuiContext()
			_, unlockErr := svc.UnlockKiosk(unlockCtx, admin.ProfileID, pin)
			unlockCancel()
			if unlockErr != nil {
				log.Warn().Err(unlockErr).Msg("kiosk unlock refused")
				ShowErrorModal(pages, app, "Administrator PIN was not accepted", onCancel)
				return
			}
			onUnlocked()
		}, onCancel)
	}
	if len(admins) == 1 {
		unlockAs(&admins[0])
		return
	}

	const modalPage = "kiosk_unlock_admin"
	cleanup := func() {
		pages.HidePage(modalPage)
		pages.RemovePage(modalPage)
	}
	menu := NewSettingsList(pages, PageMain)
	menu.SetRebuildPrevious(func() {
		cleanup()
		onCancel()
	})
	menu.SetDynamicHelpMode(true)
	for i := range admins {
		admin := admins[i]
		menu.AddAction(admin.Name, "", func() {
			cleanup()
			unlockAs(&admin)
		})
	}
	menu.List.SetBorder(true).SetTitle(" Unlock as ")
	pages.AddPage(modalPage, CenterWidget(40, len(admins)+2, menu.List), true, true)
	app.SetFocus(menu.List)
}

// buildKioskSettingsMenu is the Kiosk page under Settings. It is only
// reachable while the device is not locked, because Settings is not.
func buildKioskSettingsMenu(
	svc SettingsService, pages *tview.Pages, app *tview.Application, rebuildMainPage func(),
) {
	ctx, cancel := tuiContext()
	state, err := svc.GetKiosk(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load kiosk mode", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Kiosk mode")
	goBack := func() { pages.SwitchToPage(PageSettingsMain) }
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
	rebuild := func() { buildKioskSettingsMenu(svc, pages, app, rebuildMainPage) }
	toMain := func() {
		if rebuildMainPage != nil {
			rebuildMainPage()
			return
		}
		pages.SwitchToPage(PageMain)
	}

	enable := func(preset, summary string) {
		ShowConfirmModal(pages, app, summary, func() {
			enableCtx, enableCancel := tuiContext()
			_, enableErr := svc.EnableKiosk(enableCtx, preset, false)
			enableCancel()
			if enableErr != nil && strings.Contains(enableErr.Error(), kiosk.ErrNothingAllowed.Error()) {
				ShowConfirmModal(pages, app,
					"A profile is limited to allowed content, but nothing is allowed for it yet. "+
						"Nothing will launch on that profile. Lock anyway?",
					func() {
						retryCtx, retryCancel := tuiContext()
						_, retryErr := svc.EnableKiosk(retryCtx, preset, true)
						retryCancel()
						if retryErr != nil {
							ShowErrorModal(pages, app, kioskEnableError(retryErr), rebuild)
							return
						}
						toMain()
					}, rebuild)
				return
			}
			if enableErr != nil {
				ShowErrorModal(pages, app, kioskEnableError(enableErr), rebuild)
				return
			}
			toMain()
		}, rebuild)
	}

	menu.AddValueAction("State", "The lock's current state", func() string {
		return kioskStateText(state)
	}, func() {})

	if !kioskActive(state) {
		menu.AddAction("Turn on: Family",
			"Kids play only what you allow, with an administrator PIN to unlock on this device",
			func() {
				enable(kiosk.PresetFamily,
					"Lock this device for family use?\n\n"+
						"Settings, card writing and profile management will need an administrator PIN. "+
						"Each profile launches only what you have allowed for it. "+
						"Clients that are not paired will be disconnected.")
			})
		menu.AddAction("Turn on: Public",
			"For an unattended device: no unlock on the device, only from a paired administrator app",
			func() {
				enable(kiosk.PresetPublic,
					"Lock this device for public use?\n\n"+
						"It can only be unlocked from an app paired as administrator. "+
						"Clients that are not paired will be disconnected.")
			})
	} else {
		menu.AddValueAction("Seal", "Whether this device's own menus are closed too", func() string {
			if state.Seal.Sealed {
				return "Sealed"
			}
			return "Not sealed"
		}, func() {
			ShowInfoModal(pages, app, "Seal", kioskSealText(&state.Seal), func() {
				app.SetFocus(menu.List)
			})
		})
		menu.AddAction("Lock now", "End the temporary unlock and lock this device again", func() {
			lockCtx, lockCancel := tuiContext()
			_, lockErr := svc.LockKiosk(lockCtx)
			lockCancel()
			if lockErr != nil {
				ShowErrorModal(pages, app, "Failed to lock", rebuild)
				return
			}
			toMain()
		})
		menu.AddAction("Turn off", "Turn kiosk mode off and remove the lock", func() {
			ShowConfirmModal(pages, app, "Turn kiosk mode off?", func() {
				offCtx, offCancel := tuiContext()
				_, offErr := svc.DisableKiosk(offCtx)
				offCancel()
				if offErr != nil {
					ShowErrorModal(pages, app, "Failed to turn kiosk mode off", rebuild)
					return
				}
				rebuild()
			}, rebuild)
		})
	}
	menu.AddNavAction("Allowed content", "Choose what each profile may launch while locked", func() {
		buildKioskContentMenu(svc, pages, app, rebuild)
	})

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsKiosk, frame, true)
}

func kioskEnableError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, kiosk.ErrNoAdminProfile.Error()):
		return "Create an administrator profile first. Its PIN is what unlocks this device."
	case strings.Contains(msg, kiosk.ErrNoAdminClient.Error()):
		return "Pair an app as administrator first. Without one, nothing could unlock this device."
	default:
		return "Failed to turn kiosk mode on"
	}
}

// buildKioskContentMenu lists the profiles a locked device can be left on.
func buildKioskContentMenu(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	ctx, cancel := tuiContext()
	profiles, err := svc.GetProfiles(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load profiles", goBack)
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Kiosk mode", "Allowed content")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsKiosk)
	menu.SetRebuildPrevious(goBack)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
	rebuild := func() { buildKioskContentMenu(svc, pages, app, goBack) }

	menu.AddNavAction("Shared profile", "What launches when no profile is active", func() {
		buildKioskProfileContent(svc, pages, app, "", "Shared profile", rebuild)
	})
	for i := range profiles.Profiles {
		profile := profiles.Profiles[i]
		if profile.Role == "admin" {
			// A locked device is never left on an administrator profile.
			continue
		}
		menu.AddNavAction(profile.Name, "What "+profile.Name+" may launch", func() {
			buildKioskProfileContent(svc, pages, app, profile.ProfileID, profile.Name, rebuild)
		})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsKioskContent, frame, true)
}

var kioskModeOptions = []string{"Device default", "Everything except denied", "Only what is allowed"}

func kioskModeIndex(mode string) int {
	switch mode {
	case database.ContentModeAll:
		return 1
	case database.ContentModeAllowed:
		return 2
	default:
		return 0
	}
}

func kioskModeValue(index int) string {
	switch index {
	case 1:
		return database.ContentModeAll
	case 2:
		return database.ContentModeAllowed
	default:
		return ""
	}
}

// allowedSystemIDs returns the systems a rule's allow entries name.
func allowedSystemIDs(entries []models.ContentRuleEntry) []string {
	ids := []string{}
	for i := range entries {
		if entries[i].Effect == database.ContentEffectAllow && entries[i].Kind == database.ContentKindSystem {
			ids = append(ids, entries[i].SystemID)
		}
	}
	return ids
}

// withAllowedSystems replaces a rule's system allow entries and keeps every
// other entry, so entries this page does not edit survive a save.
func withAllowedSystems(entries []models.ContentRuleEntry, systemIDs []string) []models.ContentRuleEntry {
	out := make([]models.ContentRuleEntry, 0, len(entries)+len(systemIDs))
	for i := range entries {
		if entries[i].Effect == database.ContentEffectAllow && entries[i].Kind == database.ContentKindSystem {
			continue
		}
		out = append(out, entries[i])
	}
	for _, id := range systemIDs {
		out = append(out, models.ContentRuleEntry{
			Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: id,
		})
	}
	return out
}

// buildKioskProfileContent edits one profile's content mode and the systems
// it allows. Deck, single-game and tag entries are kept as they are; they
// are set through the API.
func buildKioskProfileContent(
	svc SettingsService, pages *tview.Pages, app *tview.Application,
	profileID, profileName string, goBack func(),
) {
	ctx, cancel := tuiContext()
	content, err := svc.GetProfileContent(ctx, profileID)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load allowed content", goBack)
		return
	}

	frame := NewPageFrame(app).SetTitle("Kiosk mode", "Allowed content", profileName)
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsKioskContent)
	menu.SetRebuildPrevious(goBack)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
	rebuild := func() { buildKioskProfileContent(svc, pages, app, profileID, profileName, goBack) }

	modeIndex := kioskModeIndex(content.Mode)
	menu.AddCycle("Mode", "Whether this profile launches everything or only what is allowed",
		kioskModeOptions, &modeIndex, func(_ string, index int) {
			saveCtx, saveCancel := tuiContext()
			saveErr := svc.UpdateProfileContent(saveCtx, &models.ProfilesContentUpdateParams{
				ProfileID: profileID, Mode: kioskModeValue(index), Entries: content.Entries,
			})
			saveCancel()
			if saveErr != nil {
				ShowErrorModal(pages, app, "Failed to save mode", rebuild)
				return
			}
			content.Mode = kioskModeValue(index)
		})
	menu.AddValueAction("Allowed systems", "Systems this profile may launch from", func() string {
		count := len(allowedSystemIDs(content.Entries))
		if count == 0 {
			return "None"
		}
		return fmt.Sprintf("%d selected", count)
	}, func() {
		buildKioskSystemsPage(svc, pages, app, profileID, profileName, content, rebuild)
	})
	if other := len(content.Entries) - len(allowedSystemIDs(content.Entries)); other > 0 {
		menu.AddValueAction("Other entries", "Deck, single-game and deny entries set through the API",
			func() string { return strconv.Itoa(other) }, func() {})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsKioskProfile, frame, true)
}

func buildKioskSystemsPage(
	svc SettingsService, pages *tview.Pages, app *tview.Application,
	profileID, profileName string, content *models.ProfilesContentResponse, goBack func(),
) {
	ctx, cancel := tuiContext()
	systems, err := svc.GetSystems(ctx)
	cancel()
	if err != nil {
		ShowErrorModal(pages, app, "Failed to load systems list", goBack)
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

	frame := NewPageFrame(app).
		SetTitle("Allowed content", profileName, "Systems").
		SetHelpText("Select the systems this profile may launch from")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app)

	selector := NewSystemSelector(&SystemSelectorConfig{
		Mode:     SystemSelectorMulti,
		Systems:  items,
		Selected: allowedSystemIDs(content.Entries),
	})
	save := func() {
		saveCtx, saveCancel := tuiContext()
		saveErr := svc.UpdateProfileContent(saveCtx, &models.ProfilesContentUpdateParams{
			ProfileID: profileID,
			Mode:      content.Mode,
			Entries:   withAllowedSystems(content.Entries, selector.GetSelected()),
		})
		saveCancel()
		if saveErr != nil {
			log.Warn().Err(saveErr).Msg("error saving allowed systems")
			ShowErrorModal(pages, app, "Failed to save allowed systems", func() {
				app.SetFocus(selector)
			})
			return
		}
		goBack()
	}
	buttonBar.AddButtonWithHelp("Done", "Save allowed systems and return", save).
		AddButtonWithHelp("Back", "Discard changes and return", goBack).
		SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})
	frame.SetButtonBar(buttonBar)

	selector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyDown:
			if selector.GetCurrentItem() == selector.GetItemCount()-1 {
				frame.FocusButtonBar()
				return nil
			}
		case tcell.KeyUp:
			if selector.GetCurrentItem() == 0 {
				frame.FocusButtonBar()
				return nil
			}
		default:
		}
		return event
	})
	buttonBar.SetOnUp(func() {
		selector.SetCurrentItem(selector.GetItemCount() - 1)
		app.SetFocus(selector)
	})
	buttonBar.SetOnDown(func() {
		selector.SetCurrentItem(0)
		app.SetFocus(selector)
	})

	frame.SetContent(selector)
	pages.AddAndSwitchToPage(PageSettingsKioskSystems, frame, true)
}
