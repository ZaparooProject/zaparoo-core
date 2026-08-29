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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
)

// runUpdate prints what the device knows about updates and installs one if
// there is one to install, returning the process exit status.
//
// Status first, always. Someone running this wants to know where they stand
// even when the answer is that nothing is available, and printing it costs a
// local read. The install only follows when there is something to install, so
// the command is safe to run out of curiosity.
func runUpdate(ctx context.Context, cfg *config.Instance, call reloadAPICaller) int {
	return runUpdateTo(ctx, os.Stdout, os.Stderr, cfg, call)
}

func runUpdateTo(
	ctx context.Context, stdout, stderr io.Writer, cfg *config.Instance, call reloadAPICaller,
) int {
	status, err := fetchUpdateStatus(ctx, cfg, call, models.MethodUpdateStatus)
	if err != nil {
		logClientCommandError(err, "error reading update status")
		_, _ = fmt.Fprintf(stderr, "Error reading update status: %v\n", err)
		return 1
	}

	// Status answers from the last scheduled check, which runs every twelve
	// hours. Someone typing this command is asking now, so look again rather
	// than acting on an answer that old: it decides both whether anything is
	// installed and which version this says it is installing. The stored answer
	// is kept only where looking again could not change it.
	if updater.EligibilityCanOfferUpdates(status.Eligibility) {
		checked, checkErr := fetchUpdateStatus(ctx, cfg, call, models.MethodUpdateCheck)
		if checkErr != nil {
			logClientCommandError(checkErr, "error checking for updates")
			_, _ = fmt.Fprintf(stderr, "Error checking for updates: %v\n", checkErr)
			return 1
		}
		status = checked
	}

	_, _ = fmt.Fprintln(stdout, describeUpdateStatus(status))

	if !status.UpdateAvailable {
		return 0
	}
	if status.BlockedBy != nil && !status.BlockedBy.Forceable {
		// Refusing here rather than letting the apply fail: the gate's message
		// already says what to do, and a second copy of it as an error is worse
		// than the first as an explanation.
		return 1
	}

	_, _ = fmt.Fprintln(stdout, describeInstallIntent(status))
	applied, err := fetchUpdateApply(ctx, cfg, call)
	if err != nil {
		logClientCommandError(err, "error installing update")
		_, _ = fmt.Fprintf(stderr, "Error installing update: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Installed %s. It is confirmed once Core has run for a short while;\n"+
		"if it fails to start, the previous version is restored automatically.\n", applied.NewVersion)
	return 0
}

// describeUpdateStatus turns a status into the one line worth reading.
//
// Ordered by what a person needs to know first. An update that failed and was
// undone outranks one that is waiting, which outranks one that is available,
// because the earlier entries are the ones where something has already
// happened to the device.
func describeUpdateStatus(s *models.UpdateCheckResponse) string {
	if s == nil {
		return "Update status is unavailable."
	}

	switch s.Eligibility {
	case updater.EligibilityDevelopment:
		return fmt.Sprintf("Running %s, a development build. Updates do not apply to it.", s.CurrentVersion)
	case updater.EligibilityManaged:
		return fmt.Sprintf("Running %s. This install is managed by a package manager, "+
			"which is where its updates come from.", s.CurrentVersion)
	case updater.EligibilityUnsupported:
		return fmt.Sprintf("Running %s. This install cannot update itself; "+
			"use the installer it came from.", s.CurrentVersion)
	}

	if outcome := describeLastUpdate(s.LastResult); outcome != "" {
		return outcome
	}
	if s.UpdateAvailable {
		return describeAvailableUpdate(s)
	}
	return fmt.Sprintf("Running %s. %s", s.CurrentVersion, describeLastCheck(s.CheckedAt))
}

// describeInstallIntent says what is about to happen, and says it differently
// when the release on offer is the one that already failed to start here.
//
// Only automatic installs decline that version; asking for it by hand is taken
// as asking on purpose. But this flag is also how someone just looks at where
// the device stands, so reinstalling the build that broke it should be stated
// rather than done quietly under a line that reads like any other update.
func describeInstallIntent(s *models.UpdateCheckResponse) string {
	if s.LastResult != nil &&
		s.LastResult.Outcome == updater.OutcomeRolledBack &&
		s.LastResult.ToVersion == s.LatestVersion {
		return fmt.Sprintf("Installing %s again, the version that already failed to start here. "+
			"If it fails again, %s is restored as before.", s.LatestVersion, s.CurrentVersion)
	}
	return fmt.Sprintf("Installing %s. Core will restart when it finishes.", s.LatestVersion)
}

// describeLastUpdate reports an update that did not simply work. A success is
// deliberately silent: the version on screen already says it happened, and a
// device that is fine should not keep announcing it.
func describeLastUpdate(last *models.UpdateLastResult) string {
	if last == nil {
		return ""
	}
	switch last.Outcome {
	case updater.OutcomeRolledBack:
		return fmt.Sprintf("Update to %s failed to start and was rolled back. "+
			"Running %s again, and your data was restored with it.", last.ToVersion, last.FromVersion)
	case updater.OutcomeRollbackBlocked:
		return fmt.Sprintf("Update to %s failed and could not be undone. "+
			"The device is still running it. See docs/ota-runbook.md.", last.ToVersion)
	case updater.OutcomeRecoveryRequired:
		return "An interrupted update could not be resolved automatically. " +
			"Updates are paused until it is. See docs/ota-runbook.md."
	default:
		return ""
	}
}

func describeAvailableUpdate(s *models.UpdateCheckResponse) string {
	line := fmt.Sprintf("%s is available. Running %s.", s.LatestVersion, s.CurrentVersion)
	switch {
	case s.BlockedBy != nil:
		return fmt.Sprintf("%s Cannot install right now: %s", line, s.BlockedBy.Message)
	case s.RolloutHeld:
		return line + " It is rolling out gradually and has not reached this device yet, " +
			"so it will not install on its own. Asking for it by hand still works."
	case s.DeferredReason != "":
		return line + " Waiting for a quiet moment."
	default:
		return line
	}
}

// describeLastCheck says how old the answer is. Status reads what the last
// check found rather than looking again, so claiming to be up to date without
// saying when that was established would be overstating it.
func describeLastCheck(checkedAt *time.Time) string {
	if checkedAt == nil || checkedAt.IsZero() {
		return "No update check has completed yet."
	}
	age := time.Since(*checkedAt)
	switch {
	case age < 0:
		return "Up to date."
	case age < time.Hour:
		return "Up to date, checked less than an hour ago."
	case age < 48*time.Hour:
		return fmt.Sprintf("Up to date, checked %d hours ago.", int(age.Hours()))
	default:
		return fmt.Sprintf("Up to date, last checked %d days ago.", int(age.Hours()/24))
	}
}

func fetchUpdateStatus(
	ctx context.Context, cfg *config.Instance, call reloadAPICaller, method string,
) (*models.UpdateCheckResponse, error) {
	raw, err := call(ctx, cfg, method, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, err)
	}
	var resp models.UpdateCheckResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", method, err)
	}
	return &resp, nil
}

func fetchUpdateApply(
	ctx context.Context, cfg *config.Instance, call reloadAPICaller,
) (*models.UpdateApplyResponse, error) {
	raw, err := call(ctx, cfg, models.MethodUpdateApply, "")
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", models.MethodUpdateApply, err)
	}
	var resp models.UpdateApplyResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", models.MethodUpdateApply, err)
	}
	return &resp, nil
}
