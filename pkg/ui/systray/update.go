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

package systray

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"fyne.io/systray"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/nixinwang/dialog"
	"github.com/rs/zerolog/log"
)

// updateMenuRefresh is how often the menu entry re-reads the status.
//
// The read is local — update.status returns what the last check found without
// contacting anything — so this paces a file read, not a network request. The
// scheduled check behind it runs every twelve hours, so anything much tighter
// would only redraw the same words.
const updateMenuRefresh = 5 * time.Minute

// updateMenuTitle is what the entry says before it is clicked. The menu is
// where someone looks to find out whether there is anything to do, so it has to
// answer that without being opened twice.
func updateMenuTitle(status *models.UpdateCheckResponse) string {
	switch {
	case status == nil:
		return "Check for Updates"
	case status.Eligibility == updater.EligibilityDevelopment:
		return "Development build"
	case status.Eligibility == updater.EligibilityManaged:
		return "Updates managed externally"
	case status.Eligibility == updater.EligibilityUnsupported:
		return "Updates unavailable here"
	case status.LastResult != nil && status.LastResult.Outcome == updater.OutcomeRolledBack:
		return "Last update was rolled back"
	case status.UpdateAvailable && status.RolloutHeld:
		return "Install " + status.LatestVersion + " (not yet rolled out)"
	case status.UpdateAvailable:
		return "Install " + status.LatestVersion
	default:
		return "Up to date"
	}
}

// updateMenuClickable reports whether clicking the entry would do anything. An
// entry that cannot act is disabled rather than hidden, because the words on it
// are the answer someone opened the menu for.
func updateMenuClickable(status *models.UpdateCheckResponse) bool {
	if status == nil {
		return true
	}
	return updater.EligibilityCanOfferUpdates(status.Eligibility)
}

// menuEntry is the part of a tray item this file drives. *systray.MenuItem
// satisfies it, and a test can substitute something that records what was set.
type menuEntry interface {
	SetTitle(string)
	Enable()
	Disable()
}

// apiCaller is how these entries reach Core, and showDialog is how they get a
// message in front of someone. Both are injected so the menu's decisions can be
// tested without a running service or a native window.
type (
	apiCaller  func(ctx context.Context, cfg *config.Instance, method, params string) (string, error)
	showDialog func(title, message string)
)

func nativeDialog(title, message string) {
	dialog.Message("%s", message).Title(title).Info()
}

func watchUpdateStatus(cfg *config.Instance, item *systray.MenuItem) {
	for {
		refreshUpdateMenu(cfg, item)
		time.Sleep(updateMenuRefresh)
	}
}

func refreshUpdateMenu(cfg *config.Instance, item menuEntry) {
	refreshUpdateMenuWith(cfg, item, client.LocalClient)
}

func refreshUpdateMenuWith(cfg *config.Instance, item menuEntry, call apiCaller) {
	status, err := readUpdateStatus(cfg, call)
	if err != nil {
		// Leave the entry as it was. Core may simply not be up yet, and
		// replacing a true answer with an error is worse than a stale one.
		log.Debug().Err(err).Msg("could not read update status for the tray menu")
		return
	}
	item.SetTitle(updateMenuTitle(status))
	if updateMenuClickable(status) {
		item.Enable()
	} else {
		item.Disable()
	}
}

// applyUpdateFromMenu installs an update if there is one, and otherwise checks
// for one. Unlike the CLI this never blocks the caller: the menu has to stay
// responsive, so the outcome arrives as a notification.
func applyUpdateFromMenu(cfg *config.Instance, item menuEntry, notify func(string)) {
	applyUpdateFromMenuWith(cfg, item, notify, client.LocalClient, nativeDialog)
}

func applyUpdateFromMenuWith(
	cfg *config.Instance, item menuEntry, notify func(string), call apiCaller, show showDialog,
) {
	status, err := readUpdateStatus(cfg, call)
	if err != nil {
		log.Error().Err(err).Msg("could not read update status")
		notify("Could not read update status.")
		return
	}

	if !status.UpdateAvailable {
		// Nothing known to install, so go and look. This is the one path that
		// costs a network request, and it only happens because someone asked.
		notify("Checking for updates...")
		checked, checkErr := requestUpdateCheck(cfg, call)
		if checkErr != nil {
			log.Error().Err(checkErr).Msg("update check failed")
			notify("Could not check for updates.")
			return
		}
		refreshUpdateMenuWith(cfg, item, call)
		if !checked.UpdateAvailable {
			notify("Zaparoo Core is up to date.")
			return
		}
		status = checked
	}

	if status.BlockedBy != nil && !status.BlockedBy.Forceable {
		notify("Cannot update right now: " + status.BlockedBy.Message)
		return
	}

	notify("Installing " + status.LatestVersion + "...")
	applied, err := requestUpdateApply(cfg, call)
	if err != nil {
		log.Error().Err(err).Msg("update install failed")
		notify("Update failed. The previous version is still installed.")
		return
	}
	show("Update Installed", fmt.Sprintf(
		"Zaparoo Core %s is installed and will restart now.\n\n"+
			"If it does not start correctly the previous version is restored automatically.",
		applied.NewVersion))
}

// startPairing shows the PIN a client needs. The pairing flow already exists as
// a CLI flag; on a desktop the tray is where someone will look for it, and
// there is no terminal open to read a PIN out of.
func startPairing(cfg *config.Instance, notify func(string)) {
	startPairingWith(cfg, notify, client.LocalClient, nativeDialog)
}

func startPairingWith(cfg *config.Instance, notify func(string), call apiCaller, show showDialog) {
	ctx := context.Background()
	raw, err := call(ctx, cfg, models.MethodClientsPairStart, "")
	if err != nil {
		log.Error().Err(err).Msg("could not start pairing")
		notify("Could not start pairing.")
		return
	}
	var resp models.ClientsPairStartResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		log.Error().Err(err).Msg("could not read the pairing response")
		notify("Could not start pairing.")
		return
	}

	show("Pair a Device", fmt.Sprintf(
		"Enter this PIN in the Zaparoo app to pair it with this device:\n\n%s\n\n"+
			"The PIN stops working once it is used or this dialog is closed.", resp.PIN))

	// Closing the dialog is the person saying they are done, whether or not a
	// client got there first. Leaving the PIN live afterwards would keep a
	// credential valid that nobody is watching any more.
	if _, err := call(ctx, cfg, models.MethodClientsPairCancel, ""); err != nil {
		log.Debug().Err(err).Msg("could not cancel pairing after the dialog closed")
	}
}

func readUpdateStatus(cfg *config.Instance, call apiCaller) (*models.UpdateCheckResponse, error) {
	return updateCall(cfg, models.MethodUpdateStatus, call)
}

func requestUpdateCheck(cfg *config.Instance, call apiCaller) (*models.UpdateCheckResponse, error) {
	return updateCall(cfg, models.MethodUpdateCheck, call)
}

func updateCall(cfg *config.Instance, method string, call apiCaller) (*models.UpdateCheckResponse, error) {
	raw, err := call(context.Background(), cfg, method, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, err)
	}
	var resp models.UpdateCheckResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", method, err)
	}
	return &resp, nil
}

func requestUpdateApply(cfg *config.Instance, call apiCaller) (*models.UpdateApplyResponse, error) {
	raw, err := call(context.Background(), cfg, models.MethodUpdateApply, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", models.MethodUpdateApply, err)
	}
	var resp models.UpdateApplyResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", models.MethodUpdateApply, err)
	}
	return &resp, nil
}
