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

// The secure-device prompt drives a transactional first-run setup: require
// encrypted remote connections, then immediately pair an admin client with
// the PIN shown on screen. If no client pairs before the PIN expires (or
// the user cancels), encryption is switched back off so a half-finished
// setup can never lock remote clients out.

import (
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	encryptionPairingModalPage   = "encryption_pairing_modal"
	encryptionPairPollInterval   = 2 * time.Second
	encryptionPairAdminRole      = "admin"
	encryptionSetupModalTitle    = "Secure Zaparoo"
	encryptionSetupNotAppliedMsg = "No changes were made. You can set this up\nanytime from Settings > Clients."
)

// shouldPromptEncryption reports whether the secure-device prompt applies:
// the device has no paired clients. This covers both the fresh state
// (encryption off) and the recovery state where a previous setup required
// encryption but the TUI exited before a client paired.
func shouldPromptEncryption(clients *models.ClientsResponse) bool {
	return clients != nil && len(clients.Clients) == 0
}

// maybeShowEncryptionPrompt shows the secure-device prompt when the device
// has no paired clients. markPrompted is called when the prompt should not
// be shown again (setup succeeded or the user declined permanently).
func maybeShowEncryptionPrompt(
	svc SettingsService, pages *tview.Pages, app *tview.Application, markPrompted func(),
) {
	ctx, cancel := tuiContext()
	clients, err := svc.GetClients(ctx)
	cancel()
	if err != nil {
		log.Warn().Err(err).Msg("failed to check paired clients for encryption prompt")
		return
	}
	if !shouldPromptEncryption(clients) {
		return
	}
	ShowEncryptionPrompt(pages, app,
		func() { startEncryptionSetup(svc, pages, app, markPrompted) },
		nil,
		markPrompted,
	)
}

// startEncryptionSetup enables required encryption and starts an admin
// pairing approval, showing the PIN until a client pairs or the PIN expires.
func startEncryptionSetup(
	svc SettingsService, pages *tview.Pages, app *tview.Application, markPrompted func(),
) {
	enabled := true
	ctx, cancel := tuiContext()
	err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{Encryption: &enabled})
	cancel()
	if err != nil {
		log.Warn().Err(err).Msg("failed to require encryption during secure-device setup")
		ShowErrorModal(pages, app, "Failed to require encryption", nil)
		return
	}
	ctx, cancel = tuiContext()
	pairing, err := svc.StartClientPairing(ctx, encryptionPairAdminRole)
	cancel()
	if err != nil {
		failOpenEncryptionSetup(svc, pages, app, "Failed to start pairing: "+err.Error())
		return
	}
	showEncryptionPairingModal(svc, pages, app, pairing, encryptionPairPollInterval, markPrompted)
}

// failOpenEncryptionSetup reverts to the open (plaintext-allowed) state after
// an incomplete setup so remote clients are not locked out, then reports why.
func failOpenEncryptionSetup(
	svc SettingsService, pages *tview.Pages, app *tview.Application, reason string,
) {
	disabled := false
	ctx, cancel := tuiContext()
	err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{Encryption: &disabled})
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("failed to revert encryption after incomplete secure-device setup")
		ShowErrorModal(pages, app,
			reason+"\n\nEncryption may still be required.\nCheck Settings > Clients.", nil)
		return
	}
	ShowInfoModal(pages, app, encryptionSetupModalTitle,
		reason+"\n\n"+encryptionSetupNotAppliedMsg, nil)
}

// showEncryptionPairingModal displays the admin pairing PIN with a countdown,
// polling for a successful pairing. On success encryption stays required and
// markPrompted fires; on expiry or cancel the setup fails open.
func showEncryptionPairingModal(
	svc SettingsService, pages *tview.Pages, app *tview.Application,
	pairing *models.ClientsPairStartResponse, pollInterval time.Duration, markPrompted func(),
) {
	expiresAt := time.Unix(pairing.ExpiresAt, 0)
	address := ""
	if ip := helpers.GetLocalIP(); ip != "" {
		address = "\nDevice address: " + ip
	}
	message := func(now time.Time) string {
		return fmt.Sprintf(
			"Pairing PIN: %s\nExpires in: %s%s\n\n"+
				"Open the Zaparoo App and enter this PIN\nwhen it asks to pair with this device.",
			formatPairingPIN(pairing.PIN), formatPairingCountdown(expiresAt, now), address)
	}

	resolved := false
	done := make(chan struct{})
	dialog := NewDialog().
		SetText(message(time.Now())).
		SetTitle("Pair Admin Client").
		AddButtons([]string{"Cancel"})

	// resolve funcs run only on the UI thread (modal DoneFunc or
	// QueueUpdateDraw), so the resolved guard needs no locking.
	beginResolve := func() bool {
		if resolved {
			return false
		}
		resolved = true
		close(done)
		pages.HidePage(encryptionPairingModalPage)
		pages.RemovePage(encryptionPairingModalPage)
		return true
	}
	cancelPairing := func() {
		ctx, cancel := tuiContext()
		if err := svc.CancelClientPairing(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to cancel pairing approval")
		}
		cancel()
	}
	resolvePaired := func() {
		if !beginResolve() {
			return
		}
		cancelPairing()
		markPrompted()
		ShowInfoModal(pages, app, encryptionSetupModalTitle,
			"Zaparoo secured.\n\nOnly approved devices can now connect to\nZaparoo. Your phone has admin access.", nil)
	}
	resolveExpired := func() {
		if !beginResolve() {
			return
		}
		failOpenEncryptionSetup(svc, pages, app, "No phone was approved before the PIN expired.")
	}
	resolveCancelled := func() {
		if !beginResolve() {
			return
		}
		cancelPairing()
		failOpenEncryptionSetup(svc, pages, app, "Pairing cancelled.")
	}

	dialog.SetDoneFunc(func(_ int) {
		resolveCancelled()
	})
	pages.AddPage(encryptionPairingModalPage, dialog, true, true)
	app.SetFocus(dialog)

	go func() {
		countdown := time.NewTicker(time.Second)
		defer countdown.Stop()
		poll := time.NewTicker(pollInterval)
		defer poll.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-countdown.C:
				if !now.Before(expiresAt) {
					app.QueueUpdateDraw(resolveExpired)
					return
				}
				app.QueueUpdateDraw(func() {
					dialog.SetText(message(now))
				})
			case <-poll.C:
				if !time.Now().Before(expiresAt) {
					app.QueueUpdateDraw(resolveExpired)
					return
				}
				ctx, cancel := tuiContext()
				clients, err := svc.GetClients(ctx)
				cancel()
				if err == nil && len(clients.Clients) > 0 {
					app.QueueUpdateDraw(resolvePaired)
					return
				}
			}
		}
	}()
}
