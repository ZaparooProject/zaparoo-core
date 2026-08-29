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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/rs/zerolog/log"
)

// updateStatusLine is the version line on the main screen.
//
// It calls update.status, which reports what the last check found without
// contacting the release server, so drawing the screen costs a local read
// rather than a request and a flash write. A check still happens on its own
// every twelve hours behind it.
func updateStatusLine(cfg *config.Instance) string {
	return updateStatusLineWith(cfg, client.LocalClient)
}

func updateStatusLineWith(cfg *config.Instance, call apiCaller) string {
	status, err := readUpdateStatus(cfg, call)
	if err != nil {
		// The daemon may not be up yet, which is normal while the TUI is
		// starting. The running version is still worth showing on its own.
		log.Debug().Err(err).Msg("could not read update status for the TUI")
		return config.AppVersion
	}
	return formatUpdateStatus(status)
}

// formatUpdateStatus keeps the running version first and appends only what adds
// something: a line that says the version and then nothing else is the normal,
// uninteresting case and should look like it.
func formatUpdateStatus(status *models.UpdateCheckResponse) string {
	if status == nil {
		return config.AppVersion
	}
	current := status.CurrentVersion
	if current == "" {
		current = config.AppVersion
	}

	if status.LastResult != nil {
		switch status.LastResult.Outcome {
		case updater.OutcomeRolledBack:
			return fmt.Sprintf("%s (update to %s was rolled back)", current, status.LastResult.ToVersion)
		case updater.OutcomeRollbackBlocked:
			return current + " (update failed and could not be undone)"
		case updater.OutcomeRecoveryRequired:
			return current + " (an interrupted update needs attention)"
		}
	}

	switch {
	case status.UpdateAvailable && status.RolloutHeld:
		return fmt.Sprintf("%s (%s available, rolling out gradually)", current, status.LatestVersion)
	case status.UpdateAvailable:
		return fmt.Sprintf("%s (%s available)", current, status.LatestVersion)
	case status.Eligibility == updater.EligibilityManaged:
		return current + " (managed externally)"
	default:
		return current
	}
}

// apiCaller is how the line reaches Core, injected so the fallback behaviour
// can be tested without a running service.
type apiCaller func(ctx context.Context, cfg *config.Instance, method, params string) (string, error)

func readUpdateStatus(cfg *config.Instance, call apiCaller) (*models.UpdateCheckResponse, error) {
	raw, err := call(context.Background(), cfg, models.MethodUpdateStatus, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", models.MethodUpdateStatus, err)
	}
	var resp models.UpdateCheckResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", models.MethodUpdateStatus, err)
	}
	return &resp, nil
}
