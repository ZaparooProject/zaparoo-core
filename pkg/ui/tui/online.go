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
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// onlinePageData bundles the API responses the online settings page renders.
// activity is nil when the remote status could not be loaded; the page still
// renders and shows the status as unknown.
type onlinePageData struct {
	status   *models.BackupStatusResponse
	settings *models.SettingsResponse
	activity *models.RemoteActivityResponse
}

// customBaseURLHost returns the display host for a non-default Online base
// URL value, or "" when it is empty or the official default. Falls back to
// the raw value if it doesn't parse as a URL.
func customBaseURLHost(value string) string {
	if config.IsDefaultOnlineBaseURL(value) {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	return parsed.Host
}

// onlineServerHost returns the online server host to display when a custom
// server is configured, or "" when using the official default.
func onlineServerHost(settings *models.SettingsResponse) string {
	if settings == nil || settings.OnlineBaseURL == nil {
		return ""
	}
	return customBaseURLHost(*settings.OnlineBaseURL)
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
			activity, err := svc.GetRemoteActivity(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("error loading remote control status")
				activity = nil
			}
			return &onlinePageData{status: status, settings: settings, activity: activity}, nil
		},
		func(data *onlinePageData) {
			renderOnlineSettingsMenu(svc, pages, app, data, goBack)
		},
	)
}

// Warp availability values reported in the remote backup status.
const (
	warpAvailable   = "available"
	warpUnavailable = "unavailable"
)

// warpCheckAttempts and warpCheckInterval bound how long turning every
// feature on waits for an unknown Warp status to resolve. Core refreshes
// it in the background with a single request, so this is normally one
// round trip.
const (
	warpCheckAttempts = 5
	warpCheckInterval = time.Second
)

const onlineLinkFirstDesc = "Link your Zaparoo Online account first"

// onlineFeatures holds the per-feature consent settings grouped by the
// "All online features" row.
type onlineFeatures struct {
	remoteControl bool
	playHistory   bool
	library       bool
	cloudBackup   bool
}

// onlineFeaturesFromSettings reads the current consent settings. Cloud
// backup falls back to the backup status when settings omit it.
func onlineFeaturesFromSettings(
	settings *models.SettingsResponse, status *models.BackupStatusResponse,
) onlineFeatures {
	var features onlineFeatures
	if status != nil {
		features.cloudBackup = status.Remote.Enabled
	}
	if settings == nil {
		return features
	}
	if settings.RemoteControlEnabled != nil {
		features.remoteControl = *settings.RemoteControlEnabled
	}
	if settings.PlaytimeSyncEnabled != nil {
		features.playHistory = *settings.PlaytimeSyncEnabled
	}
	if settings.LibrarySyncEnabled != nil {
		features.library = *settings.LibrarySyncEnabled
	}
	if settings.BackupRemoteEnabled != nil {
		features.cloudBackup = *settings.BackupRemoteEnabled
	}
	return features
}

// triState summarizes the features this device can use. Cloud backup only
// counts when the account has Warp, so an account without it reads as all
// on once everything else is.
func (f onlineFeatures) triState(warp string) TriState {
	values := []bool{f.remoteControl, f.playHistory, f.library}
	if warp == warpAvailable {
		values = append(values, f.cloudBackup)
	}
	on := 0
	for _, value := range values {
		if value {
			on++
		}
	}
	switch on {
	case 0:
		return TriStateOff
	case len(values):
		return TriStateOn
	default:
		return TriStateMixed
	}
}

// allOnlineFeaturesUpdate builds the single settings update that turns every
// feature on or off. Turning off covers all four. Turning on includes cloud
// backup only when Warp is confirmed active, since scheduled backups without
// it fail; otherwise cloud backup is left as it is.
func allOnlineFeaturesUpdate(
	current onlineFeatures, on bool, warp string,
) (onlineFeatures, *models.UpdateSettingsParams) {
	value := on
	params := &models.UpdateSettingsParams{
		RemoteControlEnabled: &value,
		PlaytimeSyncEnabled:  &value,
		LibrarySyncEnabled:   &value,
	}
	next := current
	next.remoteControl, next.playHistory, next.library = on, on, on
	if !on || warp == warpAvailable {
		params.BackupRemoteEnabled = &value
		next.cloudBackup = on
	}
	return next, params
}

// resolveWarpAvailability re-reads the backup status while Warp
// availability is unknown, giving Core's background check time to finish.
// It returns the last availability seen, which is still unknown if the
// check did not complete in time.
func resolveWarpAvailability(
	ctx context.Context, svc SettingsService, current string, attempts int, interval time.Duration,
) string {
	for range attempts {
		if current == warpAvailable || current == warpUnavailable {
			return current
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return current
		case <-timer.C:
		}
		status, err := svc.GetBackupStatus(ctx)
		if err != nil {
			log.Debug().Err(err).Msg("error re-reading backup status for Warp availability")
			continue
		}
		current = status.Remote.Availability
	}
	return current
}

// setAllOnlineFeatures turns every online feature on or off in one settings
// update and calls onDone on the UI thread with the resulting features. When
// turning on with Warp availability still unknown, it first waits for the
// check behind a cancellable "checking" modal; cancelling makes no change.
func setAllOnlineFeatures(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	current onlineFeatures,
	on bool,
	warp string,
	onDone func(onlineFeatures, error),
) {
	apply := func(warp string) {
		next, params := allOnlineFeaturesUpdate(current, on, warp)
		ctx, cancel := tuiContext()
		defer cancel()
		if err := svc.UpdateSettings(ctx, params); err != nil {
			log.Warn().Err(err).Bool("on", on).Msg("error updating all online features")
			onDone(current, err)
			return
		}
		onDone(next, nil)
	}
	if !on || warp == warpAvailable || warp == warpUnavailable {
		apply(warp)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), warpCheckAttempts*warpCheckInterval+TUIRequestTimeout)
	cancelled := false
	closeWaiting := ShowWaitingModal(pages, app, "Checking Zaparoo Warp...", func() {
		cancelled = true
		cancel()
		onDone(current, nil)
	})
	go func() {
		defer cancel()
		resolved := resolveWarpAvailability(ctx, svc, warp, warpCheckAttempts, warpCheckInterval)
		app.QueueUpdateDraw(func() {
			if cancelled {
				return
			}
			closeWaiting()
			apply(resolved)
		})
	}()
}

// renderOnlineSettingsMenu shows the Zaparoo Online page in two columns.
// The left column holds what most people need: the account, the switch for
// every online feature, and cloud backup scheduling. The right column holds
// the individual features and remote control status. Features stay disabled
// until an account is linked, because linking resets every one of them.
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

	newColumn := func() *SettingsList {
		column := NewSettingsList(pages, PageSettingsMain).SetRebuildPrevious(goBack)
		column.SetDynamicHelpMode(true).SetHelpCallback(func(desc string) { frame.SetHelpText(desc) })
		return column
	}
	left := newColumn()
	right := newColumn()
	var columns *SettingsColumns
	refocus := func() { app.SetFocus(columns) }

	rebuild := func() { buildOnlineSettingsMenu(svc, pages, app, goBack) }
	status := data.status
	linked := status.Remote.Linked
	serverHost := onlineServerHost(data.settings)
	if serverHost != "" {
		frame.SetInfoText(fmt.Sprintf(
			"[%s]Custom server: %s. All online features use it.[-]",
			CurrentTheme().WarningColorName, serverHost,
		))
	}
	features := onlineFeaturesFromSettings(data.settings, status)
	warp := status.Remote.Availability

	left.AddHeader("Account")
	if linked {
		addOnlineAccountItems(pages, app, left, status, serverHost, refocus)
	} else {
		left.AddValueAction("Status",
			"Link a Zaparoo Online account to use online features",
			func() string { return "Not linked" }, nil)
		linkDesc := "Connect this device to your Zaparoo Online account"
		if serverHost != "" {
			linkDesc = "Connect this device to " + serverHost
		}
		left.AddNavAction("Link account", linkDesc, func() {
			startAuthLinkFlow(svc, pages, app, rebuild)
		})
	}
	allDesc := "Turn every online feature on or off. Each can still be changed on its own"
	if !linked {
		allDesc = onlineLinkFirstDesc
	}
	left.AddTriState("All online features", allDesc,
		func() TriState { return features.triState(warp) },
		func(on bool) {
			setAllOnlineFeatures(svc, pages, app, features, on, warp, func(next onlineFeatures, err error) {
				features = next
				left.Redraw()
				right.Redraw()
				if err != nil {
					ShowErrorModal(pages, app, "Failed to save online features", refocus)
					return
				}
				refocus()
			})
		}).SetLastItemDisabled(!linked)
	if linked {
		addOnlineUnlinkItem(svc, pages, app, left, rebuild, refocus)
	}

	left.AddHeader("Backup")
	scheduleOptions := []string{"daily", "weekly", "manual"}
	scheduleIndex := max(slices.Index(scheduleOptions, status.Remote.Schedule), 0)
	scheduleDesc := "How often automatic cloud backup runs"
	if !linked {
		scheduleDesc = onlineLinkFirstDesc
	}
	left.AddCycle("Schedule", scheduleDesc, scheduleOptions, &scheduleIndex, func(value string, _ int) {
		ctx, cancel := tuiContext()
		defer cancel()
		if err := svc.UpdateSettings(ctx, &models.UpdateSettingsParams{BackupRemoteSchedule: &value}); err != nil {
			log.Warn().Err(err).Msg("error updating cloud backup schedule")
			ShowErrorModal(pages, app, "Failed to save cloud backup schedule", refocus)
		}
	}).SetLastItemDisabled(!linked)
	left.AddNavAction("Manage backups", "Back up now, view and restore local and cloud backups", func() {
		buildBackupSettingsMenu(svc, pages, app, rebuild)
	})

	featureToggle := func(
		label, description, logName string,
		value *bool,
		params func(*bool) *models.UpdateSettingsParams,
	) {
		if !linked {
			description = onlineLinkFirstDesc
		}
		right.AddToggle(label, description, value, func(next bool) {
			ctx, cancel := tuiContext()
			defer cancel()
			if err := svc.UpdateSettings(ctx, params(&next)); err != nil {
				*value = !next
				right.Redraw()
				log.Warn().Err(err).Msgf("error updating %s setting", logName)
				ShowErrorModal(pages, app, "Failed to save "+logName+" setting", refocus)
			}
			left.Redraw()
		}).SetLastItemDisabled(!linked)
	}

	right.AddHeader("Features")
	featureToggle("Remote control",
		"Allow your linked Zaparoo Online account to send approved commands to this device",
		"remote control", &features.remoteControl,
		func(v *bool) *models.UpdateSettingsParams {
			return &models.UpdateSettingsParams{RemoteControlEnabled: v}
		})
	featureToggle("Play history sync",
		"Upload play history to your linked Zaparoo Online account",
		"play history sync", &features.playHistory,
		func(v *bool) *models.UpdateSettingsParams {
			return &models.UpdateSettingsParams{PlaytimeSyncEnabled: v}
		})
	featureToggle("Library sync",
		"Sync your game list, favorites, likes and decks with your linked Zaparoo Online account",
		"library sync", &features.library,
		func(v *bool) *models.UpdateSettingsParams { return &models.UpdateSettingsParams{LibrarySyncEnabled: v} })
	cloudBackupDesc := "Back up this device to the cloud on a schedule"
	if warp == warpUnavailable {
		cloudBackupDesc = "Back up this device to the cloud on a schedule. Requires Zaparoo Warp"
	}
	featureToggle("Cloud backup", cloudBackupDesc,
		"cloud backup", &features.cloudBackup,
		func(v *bool) *models.UpdateSettingsParams {
			return &models.UpdateSettingsParams{BackupRemoteEnabled: v}
		})

	right.AddHeader("Remote")
	right.AddValueAction("Status",
		"Whether Zaparoo Online can currently send commands to this device",
		func() string { return remoteStatusValue(data.activity) },
		func() {
			ShowInfoModal(pages, app, "Remote control", remoteStatusDetail(data.activity), refocus)
		})
	right.AddNavAction("Activity",
		"See what a linked account's remote commands have done on this device", func() {
			buildRemoteActivityPage(svc, pages, app, rebuild)
		})

	columns = NewSettingsColumns(app, left, right)
	frame.SetContent(columns)
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsOnline, frame, true)
}

// addOnlineAccountItems adds the Account section status items shown while an
// account is linked: link status and Warp subscription state.
func addOnlineAccountItems(
	pages *tview.Pages,
	app *tview.Application,
	menu *SettingsList,
	status *models.BackupStatusResponse,
	serverHost string,
	refocus func(),
) {
	deviceName := ""
	if status.Remote.DeviceName != nil {
		deviceName = *status.Remote.DeviceName
	}
	accountLabel, accountValue := "Status", "Linked"
	if deviceName != "" {
		accountLabel, accountValue = "Linked as", deviceName
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
	menu.AddValueAction(accountLabel, accountDesc, func() string { return accountValue }, func() {
		ShowInfoModal(pages, app, "Zaparoo Online", accountDetail, refocus)
	})

	var warpValue, warpDetail string
	switch status.Remote.Availability {
	case warpAvailable:
		warpValue = "Active"
		warpDetail = "Your Zaparoo Warp subscription is active.\n\n" +
			"Cloud backup and other premium features are enabled."
	case warpUnavailable:
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
			ShowInfoModal(pages, app, "Zaparoo Warp", warpDetail, refocus)
		})
}

// addOnlineUnlinkItem adds the Unlink account action and its confirmation.
func addOnlineUnlinkItem(
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	menu *SettingsList,
	rebuild func(),
	refocus func(),
) {
	menu.AddNavAction("Unlink account", "Remove this device's Zaparoo Online credentials", func() {
		ShowConfirmModal(pages, app,
			"Unlink from Zaparoo Online?\n\nThis turns off remote control, play history\n"+
				"sync, library sync and cloud backup on this device.\nTurn them back on after linking again.",
			func() {
				ctx, cancel := tuiContext()
				err := svc.Unlink(ctx)
				cancel()
				if err != nil {
					log.Warn().Err(err).Msg("error unlinking from Zaparoo Online")
					ShowErrorModal(pages, app, "Failed to unlink", refocus)
					return
				}
				ShowInfoModal(pages, app, "Unlinked",
					"This device's Zaparoo Online\ncredentials were removed.", rebuild)
			},
			refocus,
		)
	})
}

// remoteStatusValue renders the poller's last observation as the short
// value shown on the Remote status menu line.
func remoteStatusValue(activity *models.RemoteActivityResponse) string {
	if activity == nil {
		return "Unknown"
	}
	switch activity.Status.State {
	case state.RemoteStateDisabled:
		return "Off"
	case state.RemoteStateUnlinked:
		return "Not linked"
	case state.RemoteStateConnecting:
		return "Connecting"
	case state.RemoteStateWaiting:
		return "Waiting for commands"
	case state.RemoteStateNotRemoteDevice:
		return "Not the remote device"
	case state.RemoteStateUnavailable:
		return "Not available"
	case state.RemoteStateCredentialRejected:
		return "Link rejected"
	case state.RemoteStateError:
		return "Server unreachable"
	default:
		return "Unknown"
	}
}

// remoteStatusDetail renders the explanation shown when the Remote status
// line is selected: what the state means and what, if anything, to do.
func remoteStatusDetail(activity *models.RemoteActivityResponse) string {
	if activity == nil {
		return "The remote control status could not be loaded."
	}
	var detail string
	switch activity.Status.State {
	case state.RemoteStateDisabled:
		detail = "Remote control is switched off on this device."
	case state.RemoteStateUnlinked:
		detail = "Link this device to a Zaparoo Online account\nto use remote control."
	case state.RemoteStateConnecting:
		detail = "Connecting to Zaparoo Online."
	case state.RemoteStateWaiting:
		detail = "Zaparoo Online can send commands to this device."
	case state.RemoteStateNotRemoteDevice:
		detail = "Only one device per account can use remote control\nwithout Zaparoo Warp.\n\n" +
			"Choose this device for remote access on Zaparoo Online.\nIt can take a few minutes to connect afterwards."
	case state.RemoteStateUnavailable:
		detail = "Zaparoo Online reports remote control as unavailable.\nTry again later."
	case state.RemoteStateCredentialRejected:
		detail = "Zaparoo Online rejected this device's credentials.\nUnlink the account and link it again."
	case state.RemoteStateError:
		detail = "Zaparoo Online could not be reached.\nCheck the network connection; the device retries on its own."
	default:
		detail = "Remote control has not reported a status yet."
	}
	if activity.Status.LastErrorCode != "" {
		detail += "\n\nLast error: " + activity.Status.LastErrorCode
	}
	if activity.Status.LastContactAt != "" {
		detail += "\nLast contact: " + formatRemoteActivityTime(activity.Status.LastContactAt)
	}
	return detail
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
