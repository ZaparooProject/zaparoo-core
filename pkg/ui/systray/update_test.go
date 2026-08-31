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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEntry records what the menu was told to do.
type fakeEntry struct {
	title    string
	enabled  bool
	disabled bool
}

func (f *fakeEntry) SetTitle(title string) { f.title = title }
func (f *fakeEntry) Enable()               { f.enabled = true }
func (f *fakeEntry) Disable()              { f.disabled = true }

// callerFor answers each method from a table and records the order asked.
func callerFor(t *testing.T, answers map[string]any) (call apiCaller, asked *[]string) {
	t.Helper()
	asked = &[]string{}
	return func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		*asked = append(*asked, method)
		answer, ok := answers[method]
		if !ok {
			return "", errors.New("unexpected method " + method)
		}
		if err, isErr := answer.(error); isErr {
			return "", err
		}
		body, err := json.Marshal(answer)
		require.NoError(t, err)
		return string(body), nil
	}, asked
}

func TestRefreshUpdateMenu_TitlesTheEntryFromStatus(t *testing.T) {
	t.Parallel()

	call, asked := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
		},
	})
	item := &fakeEntry{}

	refreshUpdateMenuWith(nil, item, call)
	assert.Equal(t, "Install 2.11.0", item.title)
	assert.True(t, item.enabled)
	assert.Equal(t, []string{models.MethodUpdateStatus}, *asked,
		"drawing the menu must not cost a check")
}

// Where updates do not apply the entry stays visible but dead, because the
// words on it are the answer someone opened the menu for.
func TestRefreshUpdateMenu_DisablesTheEntryWhereUpdatesDoNotApply(t *testing.T) {
	t.Parallel()

	call, _ := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion: "abc123-dev",
			Eligibility:    updater.EligibilityDevelopment,
		},
	})
	item := &fakeEntry{}

	refreshUpdateMenuWith(nil, item, call)
	assert.Equal(t, "Development build", item.title)
	assert.True(t, item.disabled)
	assert.False(t, item.enabled)
}

// Core may simply not be up yet. Replacing a true answer with an error would be
// worse than leaving a stale one.
func TestRefreshUpdateMenu_LeavesTheEntryAloneWhenCoreCannotBeReached(t *testing.T) {
	t.Parallel()

	call, _ := callerFor(t, map[string]any{
		models.MethodUpdateStatus: errors.New("connection refused"),
	})
	item := &fakeEntry{title: "Up to date"}

	refreshUpdateMenuWith(nil, item, call)
	assert.Equal(t, "Up to date", item.title)
	assert.False(t, item.enabled)
	assert.False(t, item.disabled)
}

func TestApplyUpdateFromMenu_InstallsAndReportsThroughTheDialog(t *testing.T) {
	t.Parallel()

	call, asked := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
		},
		models.MethodUpdateApply: models.UpdateApplyResponse{
			PreviousVersion: "2.10.0",
			NewVersion:      "2.11.0",
		},
	})

	var notes []string
	var shownTitle, shownBody string
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(title, message string) { shownTitle, shownBody = title, message })

	assert.Equal(t, []string{models.MethodUpdateStatus, models.MethodUpdateApply}, *asked)
	assert.Contains(t, notes, "Installing 2.11.0...")
	assert.Equal(t, "Update Installed", shownTitle)
	assert.Contains(t, shownBody, "2.11.0")
}

// With nothing known to install, clicking goes and looks. That is the one path
// that costs a network request, and it only happens because someone asked.
func TestApplyUpdateFromMenu_ChecksWhenNothingIsKnownAndStopsWhenStillNothing(t *testing.T) {
	t.Parallel()

	upToDate := models.UpdateCheckResponse{
		CurrentVersion: "2.11.0",
		Eligibility:    updater.EligibilityEligible,
	}
	call, asked := callerFor(t, map[string]any{
		models.MethodUpdateStatus: upToDate,
		models.MethodUpdateCheck:  upToDate,
	})

	var notes []string
	shown := false
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) { shown = true })

	assert.Equal(t, []string{
		models.MethodUpdateStatus, models.MethodUpdateCheck, models.MethodUpdateStatus,
	}, *asked, "must not call apply")
	assert.Contains(t, notes, "Zaparoo Core is up to date.")
	assert.False(t, shown)
}

// A gate that cannot be forced has already explained itself, so the menu repeats
// its reason instead of attempting an install that would fail.
func TestApplyUpdateFromMenu_DoesNotInstallThroughAGateThatCannotBeForced(t *testing.T) {
	t.Parallel()

	call, asked := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
			BlockedBy: &models.UpdateBlockedBy{
				Reason:    "indexing",
				Message:   "the media database is being indexed",
				Forceable: false,
			},
		},
	})

	var notes []string
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) {})

	assert.Equal(t, []string{models.MethodUpdateStatus}, *asked, "must not call apply")
	assert.Contains(t, notes, "Cannot update right now: the media database is being indexed")
}

// An install that fails leaves the previous version running, and saying so is
// the difference between a scare and an explanation.
func TestApplyUpdateFromMenu_SaysThePreviousVersionSurvivedAFailedInstall(t *testing.T) {
	t.Parallel()

	call, _ := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
		},
		models.MethodUpdateApply: errors.New("install failed"),
	})

	var notes []string
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) {})

	assert.Contains(t, notes, "Update failed. The previous version is still installed.")
}

// Closing the dialog is the person saying they are done. Leaving the PIN live
// would keep a credential valid that nobody is watching.
func TestStartPairing_ShowsThePINAndCancelsWhenTheDialogCloses(t *testing.T) {
	t.Parallel()

	call, asked := callerFor(t, map[string]any{
		models.MethodClientsPairStart:  models.ClientsPairStartResponse{PIN: "482913"},
		models.MethodClientsPairCancel: struct{}{},
	})

	var shownBody string
	order := []string{}
	startPairingWith(nil, func(string) {}, call, func(_, message string) {
		shownBody = message
		order = append(order, "dialog")
	})

	assert.Contains(t, shownBody, "482913")
	assert.Equal(t,
		[]string{models.MethodClientsPairStart, models.MethodClientsPairCancel}, *asked)
	assert.Equal(t, []string{"dialog"}, order, "the PIN is cancelled after the dialog closes")
}

func TestStartPairing_ReportsAFailureToStart(t *testing.T) {
	t.Parallel()

	call, _ := callerFor(t, map[string]any{
		models.MethodClientsPairStart: errors.New("refused"),
	})

	var notes []string
	shown := false
	startPairingWith(nil, func(m string) { notes = append(notes, m) }, call,
		func(string, string) { shown = true })

	assert.Equal(t, []string{"Could not start pairing."}, notes)
	assert.False(t, shown, "there is no PIN to show")
}

func TestApplyUpdateFromMenu_ReportsWhenStatusCannotBeRead(t *testing.T) {
	t.Parallel()

	call, asked := callerFor(t, map[string]any{
		models.MethodUpdateStatus: errors.New("connection refused"),
	})

	var notes []string
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) {})

	assert.Equal(t, []string{models.MethodUpdateStatus}, *asked)
	assert.Equal(t, []string{"Could not read update status."}, notes)
}

// A check that turns one up carries straight on into the install, and refreshes
// the entry so the menu behind the notification is no longer stale.
func TestApplyUpdateFromMenu_InstallsWhatTheCheckTurnedUp(t *testing.T) {
	t.Parallel()

	available := models.UpdateCheckResponse{
		CurrentVersion:  "2.10.0",
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
		Eligibility:     updater.EligibilityEligible,
	}
	answers := map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion: "2.10.0",
			Eligibility:    updater.EligibilityEligible,
		},
		models.MethodUpdateCheck: available,
		models.MethodUpdateApply: models.UpdateApplyResponse{
			PreviousVersion: "2.10.0", NewVersion: "2.11.0",
		},
	}
	call, asked := callerFor(t, answers)

	var notes []string
	shown := false
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) { shown = true })

	assert.Equal(t, []string{
		models.MethodUpdateStatus, models.MethodUpdateCheck,
		models.MethodUpdateStatus, models.MethodUpdateApply,
	}, *asked)
	assert.Contains(t, notes, "Installing 2.11.0...")
	assert.True(t, shown)
}

func TestApplyUpdateFromMenu_ReportsAFailedCheck(t *testing.T) {
	t.Parallel()

	call, _ := callerFor(t, map[string]any{
		models.MethodUpdateStatus: models.UpdateCheckResponse{
			CurrentVersion: "2.10.0",
			Eligibility:    updater.EligibilityEligible,
		},
		models.MethodUpdateCheck: errors.New("no route to host"),
	})

	var notes []string
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) {})

	assert.Contains(t, notes, "Could not check for updates.")
}

// A response that arrives but cannot be read is a failure, not an empty answer:
// treating it as "nothing available" would quietly show the wrong thing.
func TestUpdateCall_RejectsAResponseItCannotRead(t *testing.T) {
	t.Parallel()

	call := func(context.Context, *config.Instance, string, string) (string, error) {
		return "not json", nil
	}
	_, err := updateCall(nil, models.MethodUpdateStatus, call)
	require.Error(t, err)
	assert.Contains(t, err.Error(), models.MethodUpdateStatus)
}

func TestStartPairing_ReportsAPairingResponseItCannotRead(t *testing.T) {
	t.Parallel()

	call := func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		if method == models.MethodClientsPairStart {
			return "not json", nil
		}
		return "{}", nil
	}

	var notes []string
	shown := false
	startPairingWith(nil, func(m string) { notes = append(notes, m) }, call,
		func(string, string) { shown = true })

	assert.Equal(t, []string{"Could not start pairing."}, notes)
	assert.False(t, shown)
}

// An apply response that cannot be read is a failed install as far as the menu
// is concerned, because it has no version to report and no proof it worked.
func TestApplyUpdateFromMenu_TreatsAnUnreadableApplyResponseAsAFailure(t *testing.T) {
	t.Parallel()

	call := func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		if method == models.MethodUpdateApply {
			return "not json", nil
		}
		body, err := json.Marshal(models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
		})
		require.NoError(t, err)
		return string(body), nil
	}

	var notes []string
	shown := false
	applyUpdateFromMenuWith(nil, &fakeEntry{}, func(m string) { notes = append(notes, m) }, call,
		func(string, string) { shown = true })

	assert.Contains(t, notes, "Update failed. The previous version is still installed.")
	assert.False(t, shown)
}

// The PIN was already shown, so a cancel that fails is logged and nothing else:
// there is no second thing to tell the person to do about it.
func TestStartPairing_SurvivesACancelThatFails(t *testing.T) {
	t.Parallel()

	call := func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		if method == models.MethodClientsPairCancel {
			return "", errors.New("connection closed")
		}
		body, err := json.Marshal(models.ClientsPairStartResponse{PIN: "482913"})
		require.NoError(t, err)
		return string(body), nil
	}

	var notes []string
	var shownBody string
	startPairingWith(nil, func(m string) { notes = append(notes, m) }, call,
		func(_, message string) { shownBody = message })

	assert.Contains(t, shownBody, "482913")
	assert.Empty(t, notes, "the PIN was shown; a failed cancel is not the person's problem")
}
