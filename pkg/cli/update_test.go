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

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingCaller answers the update methods and remembers what was asked, so a
// test can tell "showed the status" from "went ahead and installed".
//
// update.check answers the same as update.status unless a test overrides it,
// which stands for a check that found nothing new.
func recordingCaller(
	t *testing.T, status *models.UpdateCheckResponse,
) (call reloadAPICaller, called *[]string) {
	t.Helper()
	return recordingCallerWithCheck(t, status, status)
}

func recordingCallerWithCheck(
	t *testing.T, status, checked *models.UpdateCheckResponse,
) (call reloadAPICaller, called *[]string) {
	t.Helper()
	called = &[]string{}
	return func(_ context.Context, _ *config.Instance, method, _ string) (string, error) {
		*called = append(*called, method)
		switch method {
		case models.MethodUpdateStatus:
			body, err := json.Marshal(status)
			require.NoError(t, err)
			return string(body), nil
		case models.MethodUpdateCheck:
			body, err := json.Marshal(checked)
			require.NoError(t, err)
			return string(body), nil
		case models.MethodUpdateApply:
			body, err := json.Marshal(models.UpdateApplyResponse{
				PreviousVersion: status.CurrentVersion,
				NewVersion:      status.LatestVersion,
			})
			require.NoError(t, err)
			return string(body), nil
		default:
			return "", errors.New("unexpected method " + method)
		}
	}, called
}

func TestRunUpdate_ShowsStatusAndInstallsWhenOneIsAvailable(t *testing.T) {
	t.Parallel()

	call, called := recordingCaller(t, &models.UpdateCheckResponse{
		CurrentVersion:  "2.10.0",
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
		Eligibility:     updater.EligibilityEligible,
	})
	var out, errOut bytes.Buffer

	code := runUpdateTo(t.Context(), &out, &errOut, nil, call)
	assert.Equal(t, 0, code)
	assert.Equal(t,
		[]string{models.MethodUpdateStatus, models.MethodUpdateCheck, models.MethodUpdateApply},
		*called)
	assert.Contains(t, out.String(), "2.11.0 is available")
	assert.Contains(t, out.String(), "Installed 2.11.0")
	assert.Empty(t, errOut.String())
}

// Nothing to install must not turn into an install attempt, so the command is
// safe to run just to see where the device stands.
func TestRunUpdate_ReportsAndStopsWhenNothingIsAvailable(t *testing.T) {
	t.Parallel()

	checkedAt := time.Now().Add(-2 * time.Hour)
	call, called := recordingCaller(t, &models.UpdateCheckResponse{
		CurrentVersion: "2.11.0",
		Eligibility:    updater.EligibilityEligible,
		CheckedAt:      &checkedAt,
	})
	var out, errOut bytes.Buffer

	code := runUpdateTo(t.Context(), &out, &errOut, nil, call)
	assert.Equal(t, 0, code)
	assert.Equal(t, []string{models.MethodUpdateStatus, models.MethodUpdateCheck}, *called,
		"must not call apply")
	assert.Contains(t, out.String(), "Up to date")
}

// The stored status is whatever the last scheduled check found, and that runs
// every twelve hours. Someone typing the command is asking now, so a release
// published since then has to be found rather than reported as up to date.
func TestRunUpdate_LooksAgainWhenTheStoredAnswerIsNothing(t *testing.T) {
	t.Parallel()

	checkedAt := time.Now().Add(-6 * time.Hour)
	call, called := recordingCallerWithCheck(t,
		&models.UpdateCheckResponse{
			CurrentVersion: "2.10.0",
			Eligibility:    updater.EligibilityEligible,
			CheckedAt:      &checkedAt,
		},
		&models.UpdateCheckResponse{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.11.0",
			UpdateAvailable: true,
			Eligibility:     updater.EligibilityEligible,
		})
	var out, errOut bytes.Buffer

	code := runUpdateTo(t.Context(), &out, &errOut, nil, call)
	assert.Equal(t, 0, code)
	assert.Equal(t,
		[]string{models.MethodUpdateStatus, models.MethodUpdateCheck, models.MethodUpdateApply},
		*called)
	assert.Contains(t, out.String(), "2.11.0 is available")
	assert.NotContains(t, out.String(), "Up to date")
}

// Checking costs a network request and a write. The states whose answer comes
// from what the install is, rather than from what has been released, cannot be
// changed by looking, so they must not pay for it.
func TestRunUpdate_DoesNotLookAgainWhenNoReleaseCouldApply(t *testing.T) {
	t.Parallel()

	for name, eligibility := range map[string]string{
		"development build": updater.EligibilityDevelopment,
		"managed install":   updater.EligibilityManaged,
		"unsupported":       updater.EligibilityUnsupported,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			call, called := recordingCaller(t, &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    eligibility,
			})
			var out, errOut bytes.Buffer

			code := runUpdateTo(t.Context(), &out, &errOut, nil, call)
			assert.Equal(t, 0, code)
			assert.Equal(t, []string{models.MethodUpdateStatus}, *called)
		})
	}
}

// The gate already explained itself. Trying anyway would only produce the same
// sentence as an error.
func TestRunUpdate_DoesNotInstallThroughAGateThatCannotBeForced(t *testing.T) {
	t.Parallel()

	call, called := recordingCaller(t, &models.UpdateCheckResponse{
		CurrentVersion:  "2.10.0",
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
		Eligibility:     updater.EligibilityEligible,
		BlockedBy: &models.UpdateBlockedBy{
			Reason:    "indexing",
			Message:   "the media database is being indexed",
			Forceable: false,
		},
	})
	var out, errOut bytes.Buffer

	code := runUpdateTo(t.Context(), &out, &errOut, nil, call)
	assert.Equal(t, 1, code)
	assert.Equal(t, []string{models.MethodUpdateStatus, models.MethodUpdateCheck}, *called,
		"must not call apply")
	assert.Contains(t, out.String(), "the media database is being indexed")
}

func TestDescribeUpdateStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status *models.UpdateCheckResponse
		want   string
	}{
		"nothing known": {status: nil, want: "unavailable"},
		"development build": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "abc123-dev",
				Eligibility:    updater.EligibilityDevelopment,
			},
			want: "development build",
		},
		"managed install": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    updater.EligibilityManaged,
			},
			want: "managed by a package manager",
		},
		// A rollback outranks an available update: something already happened to
		// this device and the person needs to know before anything else.
		"rollback outranks availability": {
			status: &models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				Eligibility:     updater.EligibilityEligible,
				LastResult: &models.UpdateLastResult{
					Outcome:     updater.OutcomeRolledBack,
					FromVersion: "2.10.0",
					ToVersion:   "2.11.0",
				},
			},
			want: "rolled back",
		},
		"a successful update is not worth announcing": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    updater.EligibilityEligible,
				LastResult: &models.UpdateLastResult{
					Outcome:     updater.OutcomeSucceeded,
					FromVersion: "2.10.0",
					ToVersion:   "2.11.0",
				},
			},
			want: "Running 2.11.0",
		},
		"held by rollout": {
			status: &models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				RolloutHeld:     true,
				Eligibility:     updater.EligibilityEligible,
			},
			want: "has not reached this device yet",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, describeUpdateStatus(tt.status), tt.want)
		})
	}
}

// Status reads what the last check found rather than looking again, so the line
// has to admit how old that is instead of implying it just looked.
func TestDescribeLastCheck_SaysHowOldTheAnswerIs(t *testing.T) {
	t.Parallel()

	assert.Contains(t, describeLastCheck(nil), "No update check has completed")

	recent := time.Now().Add(-10 * time.Minute)
	assert.Contains(t, describeLastCheck(&recent), "less than an hour ago")

	hours := time.Now().Add(-5 * time.Hour)
	assert.Contains(t, describeLastCheck(&hours), "5 hours ago")

	stale := time.Now().Add(-6 * 24 * time.Hour)
	assert.Contains(t, describeLastCheck(&stale), "6 days ago")
}

// Only automatic installs decline the version that already failed here, so
// asking by hand still installs it. This flag is also how someone just looks at
// where the device stands, so it has to say that is what it is doing.
func TestDescribeInstallIntent_SaysWhenItIsRetryingTheVersionThatFailed(t *testing.T) {
	t.Parallel()

	retry := describeInstallIntent(&models.UpdateCheckResponse{
		CurrentVersion:  "2.10.0",
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
		LastResult: &models.UpdateLastResult{
			Outcome:     updater.OutcomeRolledBack,
			FromVersion: "2.10.0",
			ToVersion:   "2.11.0",
		},
	})
	assert.Contains(t, retry, "already failed to start here")
	assert.Contains(t, retry, "2.10.0 is restored")

	// A newer release is a different release, and has not failed here.
	ordinary := describeInstallIntent(&models.UpdateCheckResponse{
		CurrentVersion:  "2.10.0",
		LatestVersion:   "2.12.0",
		UpdateAvailable: true,
		LastResult: &models.UpdateLastResult{
			Outcome:     updater.OutcomeRolledBack,
			FromVersion: "2.10.0",
			ToVersion:   "2.11.0",
		},
	})
	assert.Contains(t, ordinary, "Installing 2.12.0.")
	assert.NotContains(t, ordinary, "already failed")
}
