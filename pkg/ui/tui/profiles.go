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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	profileListHelp          = "Select a profile to edit. Use New to create one or Switch to change active profile."
	profileSwitchModalPage   = "profile_switch_modal"
	profilePINModalPage      = "profile_pin_modal"
	profilePINEditModalPage  = "profile_pin_edit_modal"
	profileSwitchIDModalPage = "profile_switch_id_modal"
)

// profileCardZapScript builds the ZapScript written to a profile switch
// card. Profile lists keep the bearer credential hidden; the verified
// profile editor reveals it only through an intentional modal action.
func profileCardZapScript(switchID string) string {
	return "**" + zapscript.ZapScriptCmdProfile + ":" + switchID
}

func profileRoleLabel(role string) string {
	switch role {
	case "admin":
		return "Admin"
	case "member":
		return "Member"
	default:
		return role
	}
}

func formatProfileLastUsed(lastUsedAt *int64, now time.Time) string {
	if lastUsedAt == nil {
		return "Never used"
	}
	used := time.Unix(*lastUsedAt, 0)
	elapsed := now.Sub(used)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "Last used just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("Last used %dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("Last used %dh ago", int(elapsed.Hours()))
	case elapsed < 7*24*time.Hour:
		return fmt.Sprintf("Last used %dd ago", int(elapsed.Hours()/24))
	default:
		return "Last used " + used.In(now.Location()).Format("Jan 2, 2006")
	}
}

func numericPINAcceptance(text string, _ rune) bool {
	if len(text) > 8 {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validPIN(text string, required bool) bool {
	if text == "" {
		return !required
	}
	return len(text) >= 4 && numericPINAcceptance(text, 0)
}

// showProfilePINModal prompts for the PIN used when switching from a visible
// profile list. Switch cards remain bearer credentials and do not use this
// path.
func showProfilePINModal(
	pages *tview.Pages,
	app *tview.Application,
	profileName string,
	onSubmit func(string),
	onCancel func(),
) {
	pinInput := tview.NewInputField().
		SetFieldWidth(8).
		SetMaskCharacter('*').
		SetAcceptanceFunc(numericPINAcceptance)
	SetInputLabel(pinInput, "PIN")
	setupInputFieldFocus(pinInput)

	cleanup := func() {
		pages.HidePage(profilePINModalPage)
		pages.RemovePage(profilePINModalPage)
	}
	submit := func() {
		pin := pinInput.GetText()
		if !validPIN(pin, true) {
			ShowErrorModal(pages, app, "PIN must be 4 to 8 digits", func() {
				app.SetFocus(pinInput)
			})
			return
		}
		cleanup()
		onSubmit(pin)
	}
	cancel := func() {
		cleanup()
		if onCancel != nil {
			onCancel()
		}
	}

	pinInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			if config.GetTUIConfig().OnScreenKeyboard {
				ShowOSKModal(pages, app, pinInput.GetText(), func(text string) {
					pinInput.SetText(text)
					submit()
				}, func() {
					app.SetFocus(pinInput)
				})
				return nil
			}
			submit()
			return nil
		case tcell.KeyEscape:
			cancel()
			return nil
		default:
			return event
		}
	})

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("Enter PIN for "+profileName), 1, 0, false).
		AddItem(pinInput, 1, 0, true).
		AddItem(tview.NewTextView().SetText("Enter to switch · Esc to cancel"), 1, 0, false)
	content.SetBorder(true)
	SetBoxTitle(content.Box, "Profile PIN")
	pages.AddPage(profilePINModalPage, CenterWidget(38, 5, content), true, true)
	app.SetFocus(pinInput)
}

func showProfilePINEditModal(
	pages *tview.Pages,
	app *tview.Application,
	allowClear bool,
	onSet func(string),
	onClear func(),
	onCancel func(),
) {
	input := tview.NewInputField().
		SetFieldWidth(8).
		SetMaskCharacter('*').
		SetAcceptanceFunc(numericPINAcceptance).
		SetPlaceholder("new PIN")
	SetInputLabel(input, "New PIN")
	setupInputFieldFocus(input)

	cleanup := func() {
		pages.HidePage(profilePINEditModalPage)
		pages.RemovePage(profilePINEditModalPage)
	}
	cancel := func() {
		cleanup()
		if onCancel != nil {
			onCancel()
		}
	}
	submit := func() {
		pin := input.GetText()
		if !validPIN(pin, true) {
			ShowErrorModal(pages, app, "PIN must be 4 to 8 digits", func() {
				app.SetFocus(input)
			})
			return
		}
		cleanup()
		onSet(pin)
	}

	buttons := NewButtonBar(app)
	buttons.AddButton("Set PIN", submit)
	if allowClear {
		buttons.AddButton("Clear PIN", func() {
			ShowConfirmModal(pages, app, "Clear this profile PIN?", func() {
				cleanup()
				onClear()
			}, func() {
				app.SetFocus(buttons)
			})
		})
	}
	buttons.AddButton("Cancel", cancel)
	buttons.SetupNavigation(cancel).
		SetOnUp(func() { app.SetFocus(input) }).
		SetOnWrap(func() { app.SetFocus(input) })

	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			if config.GetTUIConfig().OnScreenKeyboard {
				ShowOSKModal(pages, app, input.GetText(), func(text string) {
					input.SetText(text)
					submit()
				}, func() {
					app.SetFocus(input)
				})
				return nil
			}
			submit()
			return nil
		case tcell.KeyTab, tcell.KeyDown:
			app.SetFocus(buttons)
			return nil
		case tcell.KeyEscape:
			cancel()
			return nil
		default:
			return event
		}
	})

	guidance := tview.NewTextView().
		SetText("Enter a new 4 to 8 digit PIN.")
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(guidance, 2, 0, false).
		AddItem(input, 1, 0, true).
		AddItem(buttons, 1, 0, false)
	content.SetBorder(true)
	SetBoxTitle(content.Box, "Profile PIN")
	pages.AddPage(profilePINEditModalPage, CenterWidget(54, 6, content), true, true)
	app.SetFocus(input)
}

func showProfileSwitchIDModal(
	pages *tview.Pages,
	app *tview.Application,
	switchID string,
	onReset func(),
	onCancel func(),
) {
	modal := tview.NewModal().
		SetText("Switch ID:\n" + switchID + "\n\nResetting invalidates existing switch cards.").
		AddButtons([]string{"Reset", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, _ string) {
			pages.HidePage(profileSwitchIDModalPage)
			pages.RemovePage(profileSwitchIDModalPage)
			if buttonIndex == 0 {
				onReset()
			} else if onCancel != nil {
				onCancel()
			}
		})
	SetBoxTitle(modal.Box, "Switch ID")
	pages.AddPage(profileSwitchIDModalPage, modal, false, true)
	app.SetFocus(modal)
}

func hasAdminProfile(profiles []models.ProfileResponse) bool {
	for i := range profiles {
		if profiles[i].Role == "admin" {
			return true
		}
	}
	return false
}

func promptProfileManagement(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	profiles []models.ProfileResponse,
	onAuthorized func(),
	onCancel func(),
) {
	admins := make([]models.ProfileResponse, 0, len(profiles))
	for i := range profiles {
		if profiles[i].Role == "admin" {
			admins = append(admins, profiles[i])
		}
	}
	if len(admins) == 0 {
		ShowErrorModal(pages, app, "No administrator profile exists. Create one to finish setup.", onCancel)
		return
	}
	if len(admins) == 1 {
		admin := &admins[0]
		showProfilePINModal(pages, app, admin.Name, func(pin string) {
			ctx, cancel := tuiContext()
			err := svc.VerifyProfileManagement(ctx, admin.ProfileID, pin)
			cancel()
			if err != nil {
				ShowErrorModal(pages, app, "Administrator PIN was not accepted", func() {
					promptProfileManagement(svc, pages, app, profiles, onAuthorized, onCancel)
				})
				return
			}
			onAuthorized()
		}, onCancel)
		return
	}

	menu := NewSettingsList(pages, PageProfilesList)
	cleanup := func() {
		pages.HidePage(profileSwitchModalPage)
		pages.RemovePage(profileSwitchModalPage)
	}
	menu.SetRebuildPrevious(func() {
		cleanup()
		if onCancel != nil {
			onCancel()
		}
	})
	menu.SetDynamicHelpMode(true)
	for i := range admins {
		admin := admins[i]
		menu.AddAction(admin.Name, "", func() {
			showProfilePINModal(pages, app, admin.Name, func(pin string) {
				ctx, cancel := tuiContext()
				err := svc.VerifyProfileManagement(ctx, admin.ProfileID, pin)
				cancel()
				if err != nil {
					ShowErrorModal(pages, app, "Administrator PIN was not accepted", func() {
						app.SetFocus(menu.List)
					})
					return
				}
				cleanup()
				onAuthorized()
			}, func() {
				app.SetFocus(menu.List)
			})
		})
	}
	menu.SetBorder(true)
	SetBoxTitle(menu.Box, "Administrator")
	pages.AddPage(profileSwitchModalPage, CenterWidget(36, min(len(admins)+2, 12), menu.List), true, true)
	app.SetFocus(menu.List)
}

func switchProfileFromList(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	profile *models.ProfileResponse,
	onComplete func(error),
	onCancel func(),
) {
	switchWithPIN := func(pin *string) {
		ctx, cancel := tuiContext()
		defer cancel()
		profileID := profile.ProfileID
		onComplete(svc.SwitchProfile(ctx, &models.SwitchProfileParams{
			ProfileID: &profileID,
			PIN:       pin,
		}))
	}
	if !profile.HasPIN {
		switchWithPIN(nil)
		return
	}
	showProfilePINModal(pages, app, profile.Name, func(pin string) {
		switchWithPIN(&pin)
	}, onCancel)
}

// showProfileSwitchModal displays a quick-switch picker: the shared
// profile plus every personal profile, with the active one marked.
// Selecting an entry switches it, prompting first when it has a PIN. The
// modal owns its own selection, so it needs no pre-selected list item.
func showProfileSwitchModal(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	profiles []models.ProfileResponse,
	activeID string,
	onDone func(switched bool),
) {
	cleanup := func() {
		pages.HidePage(profileSwitchModalPage)
		pages.RemovePage(profileSwitchModalPage)
	}

	menu := NewSettingsList(pages, PageProfilesList)
	// Escape closes the modal instead of navigating pages.
	menu.SetRebuildPrevious(func() {
		cleanup()
		onDone(false)
	})
	// Names only, no inline descriptions: keeps the modal compact and, as
	// everywhere in the profiles UI, no credentials on screen.
	menu.SetDynamicHelpMode(true)

	doSwitch := func(params *models.SwitchProfileParams) {
		cleanup()
		ctx, cancel := tuiContext()
		defer cancel()
		if err := svc.SwitchProfile(ctx, params); err != nil {
			log.Warn().Err(err).Msg("error switching profile")
			ShowErrorModal(pages, app, "Failed to switch profile", func() {
				onDone(false)
			})
			return
		}
		onDone(true)
	}

	sharedLabel := "Shared profile"
	if activeID == "" {
		sharedLabel += " (active)"
	}
	menu.AddAction(sharedLabel, "", func() {
		doSwitch(nil)
	})

	for i := range profiles {
		p := profiles[i]
		label := p.Name
		if p.ProfileID == activeID {
			label += " (active)"
		}
		menu.AddAction(label, "", func() {
			switchProfileFromList(svc, pages, app, &p, func(err error) {
				if err != nil {
					log.Warn().Err(err).Msg("error switching profile")
					ShowErrorModal(pages, app, "Failed to switch profile", func() {
						app.SetFocus(menu.List)
					})
					return
				}
				cleanup()
				onDone(true)
			}, func() {
				app.SetFocus(menu.List)
			})
		})
	}

	menu.SetBorder(true)
	SetBoxTitle(menu.Box, "Switch Profile")

	height := len(profiles) + 1 + 2
	if height > 12 {
		height = 12
	}
	centered := CenterWidget(36, height, menu.List)
	pages.AddPage(profileSwitchModalPage, centered, true, true)
	app.SetFocus(menu.List)
}

// BuildProfilesPage creates the profiles list page. Selecting a profile
// verifies an administrator PIN before opening its settings editor.
func BuildProfilesPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	profilesResp, err := svc.GetProfiles(ctx)
	cancel()
	if err != nil {
		log.Warn().Err(err).Msg("error fetching profiles")
		ShowErrorModal(pages, app, "Failed to load profiles", func() {
			pages.SwitchToPage(PageMain)
		})
		return
	}
	profiles := profilesResp.Profiles

	ctx, cancel = tuiContext()
	active, err := svc.GetActiveProfile(ctx)
	cancel()
	if err != nil {
		log.Warn().Err(err).Msg("error fetching active profile")
	}
	activeID := ""
	if active != nil {
		activeID = active.ProfileID
	}

	frame := NewPageFrame(app).
		SetTitle("Profiles")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	menu := NewSettingsList(pages, PageMain)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	buttonBar := NewButtonBar(app)
	hasAdmin := hasAdminProfile(profiles)
	buttonBar.AddButtonWithHelp("New", "Create a new profile", func() {
		if !hasAdmin {
			buildProfileEditPage(svc, pages, app, nil)
			return
		}
		promptProfileManagement(svc, pages, app, profiles, func() {
			buildProfileEditPage(svc, pages, app, nil)
		}, func() {
			app.SetFocus(buttonBar)
		})
	})
	buttonBar.AddButtonWithHelp("Switch", "Quickly switch the active profile", func() {
		showProfileSwitchModal(svc, pages, app, profiles, activeID, func(switched bool) {
			if switched {
				BuildProfilesPage(svc, pages, app)
				return
			}
			app.SetFocus(buttonBar)
		})
	})
	buttonBar.AddButtonWithHelp("Back", "Return to main menu", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})
	frame.SetButtonBar(buttonBar)

	for i := range profiles {
		p := profiles[i]
		label := p.Name
		if p.ProfileID == activeID {
			label += " (active)"
		}
		label += " · " + profileRoleLabel(p.Role) + " · " + formatProfileLastUsed(p.LastUsedAt, time.Now())
		menu.AddNavAction(label, profileListHelp, func() {
			if !hasAdmin {
				buildProfileEditPage(svc, pages, app, &p)
				return
			}
			promptProfileManagement(svc, pages, app, profiles, func() {
				buildProfileEditPage(svc, pages, app, &p)
			}, func() {
				app.SetFocus(menu.List)
			})
		})
	}
	if len(profiles) == 0 {
		menu.AddAction("(no profiles)", profileListHelp, func() {})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageProfilesList, frame, true)
}

// limitsEnabledStates are the tri-state options for the limits override:
// inherit the global setting, force on, or force off.
var (
	limitsEnabledStates = []string{"Inherit", "On", "Off"}
	profileRoleStates   = []string{"Admin", "Member"}
)

// validDuration reports whether the text is empty (inherit) or a valid
// duration string ("0" means unlimited).
func validDuration(text string) bool {
	if text == "" {
		return true
	}
	_, err := time.ParseDuration(text)
	return err == nil
}

// buildProfileEditPage creates the profile settings editor. A nil profile
// creates a new one; saving or cancelling returns to the profile list.
//
//nolint:gocyclo,funlen // profile state, validation, and privileged actions stay together
func buildProfileEditPage(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	profile *models.ProfileResponse,
) {
	isNew := profile == nil
	ctx, cancel := tuiContext()
	profilesResp, profilesErr := svc.GetProfiles(ctx)
	cancel()
	if profilesErr != nil {
		ShowErrorModal(pages, app, "Failed to load profiles", func() {
			BuildProfilesPage(svc, pages, app)
		})
		return
	}
	adminSetup := !hasAdminProfile(profilesResp.Profiles)

	goBack := func() {
		BuildProfilesPage(svc, pages, app)
	}

	titleParts := []string{"Profiles", "New"}
	if !isNew {
		titleParts = []string{"Profiles", profile.Name, "Edit"}
	}
	frame := NewPageFrame(app).
		SetTitle(titleParts...).
		SetHelpText("Tab between fields, Save when done")
	frame.SetOnEscape(goBack)

	name := ""
	pin := ""
	limitsState := 0
	roleState := 1
	if adminSetup {
		roleState = 0
	}
	daily := ""
	session := ""
	hasPIN := false
	if !isNew {
		name = profile.Name
		if profile.Role == "admin" {
			roleState = 0
		}
		hasPIN = profile.HasPIN
		if profile.LimitsEnabled != nil {
			if *profile.LimitsEnabled {
				limitsState = 1
			} else {
				limitsState = 2
			}
		}
		if profile.DailyLimit != nil {
			daily = *profile.DailyLimit
		}
		if profile.SessionLimit != nil {
			session = *profile.SessionLimit
		}
	}
	clearPIN := false

	buttonBar := NewButtonBar(app)
	requestAdmin := func(onAuthorized func()) {
		if adminSetup {
			onAuthorized()
			return
		}
		promptProfileManagement(svc, pages, app, profilesResp.Profiles, onAuthorized, func() {
			app.SetFocus(buttonBar)
		})
	}

	menu := NewSettingsList(pages, PageProfilesList)
	menu.SetRebuildPrevious(goBack).
		SetDynamicHelpMode(true).
		SetHelpCallback(func(help string) {
			frame.SetHelpText(help)
		})
	menu.AddTextEdit("Name", "Profile display name", &name, &SettingsTextEditOptions{
		App:        app,
		FieldWidth: 30,
		HelpText:   "Enter the name shown in profile menus.",
		Validate: func(text string) error {
			if strings.TrimSpace(text) == "" {
				return errors.New("name is required")
			}
			return nil
		},
	}, nil)
	menu.AddValueAction("PIN", "Set, replace, or clear the profile PIN", func() string {
		switch {
		case clearPIN:
			return "Will be cleared"
		case pin != "" || hasPIN:
			return "******"
		default:
			return "Not set"
		}
	}, func() {
		allowClear := roleState != 0 && (pin != "" || (!isNew && hasPIN))
		showProfilePINEditModal(pages, app, allowClear, func(newPIN string) {
			pin = newPIN
			clearPIN = false
			menu.refreshAllItems(menu.GetCurrentItem())
			app.SetFocus(menu.List)
		}, func() {
			pin = ""
			clearPIN = !isNew && hasPIN
			menu.refreshAllItems(menu.GetCurrentItem())
			app.SetFocus(menu.List)
		}, func() {
			app.SetFocus(menu.List)
		})
	})
	if !isNew {
		menu.AddValueAction("Switch ID", "Reveal or reset the switch-card identifier", func() string {
			return "******"
		}, func() {
			showProfileSwitchIDModal(pages, app, profile.SwitchID, func() {
				reset := func() {
					ctx, cancel := tuiContext()
					updated, err := svc.UpdateProfile(ctx, &models.UpdateProfileParams{
						ProfileID:          profile.ProfileID,
						RegenerateSwitchID: true,
					})
					cancel()
					if err != nil {
						ShowErrorModal(pages, app, "Failed to reset switch ID", func() {
							app.SetFocus(menu.List)
						})
						return
					}
					profile.SwitchID = updated.SwitchID
					ShowInfoModal(pages, app, "Switch ID", "New switch ID issued.", func() {
						app.SetFocus(menu.List)
					})
				}
				if adminSetup {
					reset()
					return
				}
				promptProfileManagement(svc, pages, app, profilesResp.Profiles, reset, func() {
					app.SetFocus(menu.List)
				})
			}, func() {
				app.SetFocus(menu.List)
			})
		})
	}
	if !adminSetup {
		menu.AddCycle("Role", "Administrator or member profile", profileRoleStates, &roleState, nil)
	}
	menu.AddCycle("Limits", "Override global playtime limits", limitsEnabledStates, &limitsState, nil)
	durationValidator := func(text string) error {
		if !validDuration(strings.TrimSpace(text)) {
			return errors.New("duration must look like 2h30m; use 0 for unlimited")
		}
		return nil
	}
	limitEditOptions := func() *SettingsTextEditOptions {
		return &SettingsTextEditOptions{
			App:          app,
			FieldWidth:   12,
			EmptyDisplay: "Inherit",
			HelpText:     "Examples: 30m, 2h, 2h30m. Empty inherits the global limit; 0 means unlimited.",
			Validate:     durationValidator,
		}
	}
	menu.AddTextEdit("Daily limit", "Empty inherits global; 0 means unlimited", &daily, limitEditOptions(), nil)
	menu.AddTextEdit("Session limit", "Empty inherits global; 0 means unlimited", &session, limitEditOptions(), nil)

	doSave := func() {
		nameVal := strings.TrimSpace(name)
		if nameVal == "" {
			ShowErrorModal(pages, app, "Name is required", func() {
				app.SetFocus(menu.List)
			})
			return
		}
		pinVal := strings.TrimSpace(pin)
		if roleState == 0 && pinVal == "" && (isNew || !hasPIN || clearPIN) {
			ShowErrorModal(pages, app, "Administrator profiles require a PIN", func() {
				app.SetFocus(menu.List)
			})
			return
		}
		if !validPIN(pinVal, false) {
			ShowErrorModal(pages, app, "PIN must be 4 to 8 digits", func() {
				app.SetFocus(menu.List)
			})
			return
		}
		dailyVal := strings.TrimSpace(daily)
		sessionVal := strings.TrimSpace(session)
		if !validDuration(dailyVal) || !validDuration(sessionVal) {
			ShowErrorModal(pages, app, "Limits must be durations like 2h30m (0 = unlimited)", func() {
				app.SetFocus(menu.List)
			})
			return
		}

		var limitsEnabled *bool
		switch limitsState {
		case 1:
			v := true
			limitsEnabled = &v
		case 2:
			v := false
			limitsEnabled = &v
		}
		var dailyPtr, sessionPtr *string
		if dailyVal != "" {
			dailyPtr = &dailyVal
		}
		if sessionVal != "" {
			sessionPtr = &sessionVal
		}

		role := strings.ToLower(profileRoleStates[roleState])
		if isNew {
			params := &models.NewProfileParams{
				Name:          nameVal,
				Role:          role,
				LimitsEnabled: limitsEnabled,
				DailyLimit:    dailyPtr,
				SessionLimit:  sessionPtr,
			}
			if pinVal != "" {
				params.PIN = &pinVal
			}
			ctx, cancel := tuiContext()
			created, err := svc.NewProfile(ctx, params)
			cancel()
			if err != nil {
				log.Warn().Err(err).Msg("error creating profile")
				ShowErrorModal(pages, app, "Failed to create profile: "+err.Error(), func() {
					app.SetFocus(menu.List)
				})
				return
			}
			_ = created
			BuildProfilesPage(svc, pages, app)
			return
		}

		// Clear-then-set: the form's state is the profile's full desired
		// limit configuration, so empty fields become inherited again.
		params := &models.UpdateProfileParams{
			ProfileID:     profile.ProfileID,
			Name:          &nameVal,
			Role:          &role,
			ClearLimits:   true,
			LimitsEnabled: limitsEnabled,
			DailyLimit:    dailyPtr,
			SessionLimit:  sessionPtr,
			ClearPIN:      clearPIN && pinVal == "",
		}
		if pinVal != "" {
			params.PIN = &pinVal
		}
		ctx, cancel := tuiContext()
		updated, err := svc.UpdateProfile(ctx, params)
		cancel()
		if err != nil {
			log.Warn().Err(err).Msg("error updating profile")
			ShowErrorModal(pages, app, "Failed to update profile: "+err.Error(), func() {
				app.SetFocus(menu.List)
			})
			return
		}
		_ = updated
		BuildProfilesPage(svc, pages, app)
	}

	buttonBar.AddButtonWithHelp("Save", "Save the profile", func() {
		requestAdmin(func() {
			doSave()
		})
	})
	if !isNew {
		buttonBar.AddButtonWithHelp("Write Card", "Write a switch card for this profile", func() {
			requestAdmin(func() {
				WriteTagWithModal(pages, app, svc, profileCardZapScript(profile.SwitchID), func(_ bool) {
					app.SetFocus(buttonBar)
				})
			})
		})
		buttonBar.AddButtonWithHelp("Delete", "Delete profile; saved data remains on disk", func() {
			requestAdmin(func() {
				ShowConfirmModal(pages, app, "Delete profile "+profile.Name+"?", func() {
					ctx, cancel := tuiContext()
					err := svc.DeleteProfile(ctx, profile.ProfileID)
					cancel()
					if err != nil {
						ShowErrorModal(pages, app, "Failed to delete profile: "+err.Error(), nil)
						return
					}
					BuildProfilesPage(svc, pages, app)
				}, nil)
			})
		})
	}
	buttonBar.AddButtonWithHelp("Back", "Discard changes and return to profiles", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	frame.SetContent(menu.List)
	frame.SetButtonBar(buttonBar)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageProfilesEdit, frame, true)
}
