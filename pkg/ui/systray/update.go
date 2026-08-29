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

func watchUpdateStatus(cfg *config.Instance, item *systray.MenuItem) {
	for {
		refreshUpdateMenu(cfg, item)
		time.Sleep(updateMenuRefresh)
	}
}

func refreshUpdateMenu(cfg *config.Instance, item *systray.MenuItem) {
	status, err := readUpdateStatus(cfg)
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
func applyUpdateFromMenu(cfg *config.Instance, item *systray.MenuItem, notify func(string)) {
	status, err := readUpdateStatus(cfg)
	if err != nil {
		log.Error().Err(err).Msg("could not read update status")
		notify("Could not read update status.")
		return
	}

	if !status.UpdateAvailable {
		// Nothing known to install, so go and look. This is the one path that
		// costs a network request, and it only happens because someone asked.
		notify("Checking for updates...")
		checked, checkErr := requestUpdateCheck(cfg)
		if checkErr != nil {
			log.Error().Err(checkErr).Msg("update check failed")
			notify("Could not check for updates.")
			return
		}
		refreshUpdateMenu(cfg, item)
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
	applied, err := requestUpdateApply(cfg)
	if err != nil {
		log.Error().Err(err).Msg("update install failed")
		notify("Update failed. The previous version is still installed.")
		return
	}
	dialog.Message(
		"Zaparoo Core %s is installed and will restart now.\n\n"+
			"If it does not start correctly the previous version is restored automatically.",
		applied.NewVersion,
	).Title("Update Installed").Info()
}

// startPairing shows the PIN a client needs. The pairing flow already exists as
// a CLI flag; on a desktop the tray is where someone will look for it, and
// there is no terminal open to read a PIN out of.
func startPairing(cfg *config.Instance, notify func(string)) {
	ctx := context.Background()
	raw, err := client.LocalClient(ctx, cfg, models.MethodClientsPairStart, "")
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

	dialog.Message(
		"Enter this PIN in the Zaparoo app to pair it with this device:\n\n%s\n\n"+
			"The PIN stops working once it is used or this dialog is closed.",
		resp.PIN,
	).Title("Pair a Device").Info()

	// Closing the dialog is the person saying they are done, whether or not a
	// client got there first. Leaving the PIN live afterwards would keep a
	// credential valid that nobody is watching any more.
	if _, err := client.LocalClient(ctx, cfg, models.MethodClientsPairCancel, ""); err != nil {
		log.Debug().Err(err).Msg("could not cancel pairing after the dialog closed")
	}
}

func readUpdateStatus(cfg *config.Instance) (*models.UpdateCheckResponse, error) {
	return updateCall(cfg, models.MethodUpdateStatus)
}

func requestUpdateCheck(cfg *config.Instance) (*models.UpdateCheckResponse, error) {
	return updateCall(cfg, models.MethodUpdateCheck)
}

func updateCall(cfg *config.Instance, method string) (*models.UpdateCheckResponse, error) {
	raw, err := client.LocalClient(context.Background(), cfg, method, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, err)
	}
	var resp models.UpdateCheckResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", method, err)
	}
	return &resp, nil
}

func requestUpdateApply(cfg *config.Instance) (*models.UpdateApplyResponse, error) {
	raw, err := client.LocalClient(context.Background(), cfg, models.MethodUpdateApply, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", models.MethodUpdateApply, err)
	}
	var resp models.UpdateApplyResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", models.MethodUpdateApply, err)
	}
	return &resp, nil
}
