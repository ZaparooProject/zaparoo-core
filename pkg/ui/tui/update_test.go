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
	"encoding/json"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatUpdateStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status *models.UpdateCheckResponse
		want   string
	}{
		// The ordinary case should read as just a version. Anything appended
		// implies something needs doing.
		"up to date says only the version": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    updater.EligibilityEligible,
			},
			want: "2.11.0",
		},
		"available": {
			status: &models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				Eligibility:     updater.EligibilityEligible,
			},
			want: "2.10.0 (2.11.0 available)",
		},
		"held by rollout explains why it is not installing": {
			status: &models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				RolloutHeld:     true,
				Eligibility:     updater.EligibilityEligible,
			},
			want: "2.10.0 (2.11.0 available, rolling out gradually)",
		},
		"a rollback outranks an available update": {
			status: &models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				Eligibility:     updater.EligibilityEligible,
				LastResult: &models.UpdateLastResult{
					Outcome:   updater.OutcomeRolledBack,
					ToVersion: "2.11.0",
				},
			},
			want: "2.10.0 (update to 2.11.0 was rolled back)",
		},
		"managed installs say where updates come from": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    updater.EligibilityManaged,
			},
			want: "2.11.0 (managed externally)",
		},
		// Nothing read yet still has to draw, because the line exists before
		// the first check completes.
		"nothing known falls back to the running version": {
			status: nil,
			want:   config.AppVersion,
		},
		"a status with no version of its own uses the running one": {
			status: &models.UpdateCheckResponse{Eligibility: updater.EligibilityEligible},
			want:   config.AppVersion,
		},
		// An update that failed and could not be undone is the one state where
		// the device is running something nobody chose.
		"a rollback that could not be completed": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.11.0",
				Eligibility:    updater.EligibilityEligible,
				LastResult: &models.UpdateLastResult{
					Outcome: updater.OutcomeRollbackBlocked, ToVersion: "2.11.0",
				},
			},
			want: "2.11.0 (update failed and could not be undone)",
		},
		"an interrupted update still needs attention": {
			status: &models.UpdateCheckResponse{
				CurrentVersion: "2.10.0",
				Eligibility:    updater.EligibilityEligible,
				LastResult: &models.UpdateLastResult{
					Outcome: updater.OutcomeRecoveryRequired,
				},
			},
			want: "2.10.0 (an interrupted update needs attention)",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, formatUpdateStatus(tt.status))
		})
	}
}

// The daemon may not be up yet while the TUI is starting. The running version
// is still worth showing on its own, so a failed read must not blank the line.
func TestUpdateStatusLine_FallsBackToTheRunningVersion(t *testing.T) {
	t.Parallel()

	line := updateStatusLineWith(nil,
		func(context.Context, *config.Instance, string, string) (string, error) {
			return "", errors.New("connection refused")
		})
	assert.Equal(t, config.AppVersion, line)

	// A response that arrives but cannot be read is the same situation.
	line = updateStatusLineWith(nil,
		func(context.Context, *config.Instance, string, string) (string, error) {
			return "not json", nil
		})
	assert.Equal(t, config.AppVersion, line)
}

func TestUpdateStatusLine_ShowsWhatStatusReported(t *testing.T) {
	t.Parallel()

	line := updateStatusLineWith(nil,
		func(context.Context, *config.Instance, string, string) (string, error) {
			body, err := json.Marshal(models.UpdateCheckResponse{
				CurrentVersion:  "2.10.0",
				LatestVersion:   "2.11.0",
				UpdateAvailable: true,
				Eligibility:     updater.EligibilityEligible,
			})
			require.NoError(t, err)
			return string(body), nil
		})
	assert.Equal(t, "2.10.0 (2.11.0 available)", line)
}
