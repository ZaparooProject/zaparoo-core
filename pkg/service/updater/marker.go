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

package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// currentMarkerVersion is the schema of pending.json this build writes.
	currentMarkerVersion = 1

	markerFileName = "pending.json"
	// markerBadSuffix names a marker this build could not parse. It is kept
	// rather than deleted so the failure can be looked at afterwards; an update
	// that went wrong is exactly when the evidence matters.
	markerBadSuffix = ".bad"
)

// markerState is where an update has got to. It is the field the watchdog keys
// its decision off, so the zero value is deliberately not one of them: an empty
// state in a file that exists means the marker is unusable, not that the update
// had not started.
type markerState string

const (
	// markerInstalling is durable before the old binary is moved. A crash in the
	// install swap can therefore restore the old binary instead of leaving no
	// executable at the target path.
	markerInstalling markerState = "installing"
	// markerInstalled means the new binary is in place but has never run. The
	// process that wrote this had not restarted yet.
	markerInstalled markerState = "installed"
	// markerConfirming means the new binary booted far enough to read its own
	// marker. It has not yet proved it can stay up.
	markerConfirming markerState = "confirming"
	// markerRollingBack means a rollback started. It is written before anything
	// is moved, so a rollback interrupted by a power cut resumes rather than
	// leaving half the files swapped.
	markerRollingBack markerState = "rollingBack"
)

// updateTrigger records who asked for the update, for the inbox message and for
// operators reading the file. Nothing branches on it.
type updateTrigger string

const (
	triggerManual updateTrigger = "manual"
	triggerAuto   updateTrigger = "auto"
)

// updateOutcome is written once an update reaches a terminal state.
type updateOutcome string

const (
	outcomeSucceeded        updateOutcome = "succeeded"
	outcomeRolledBack       updateOutcome = "rolledBack"
	outcomeRecoveryRequired updateOutcome = "recoveryRequired"
	// outcomeRollbackBlocked means the binary was rolled back but the user
	// database snapshot could not be restored, or the rollback could not be
	// completed at all, and the new binary was left installed instead. A device
	// running a suspect version beats one that will not boot.
	outcomeRollbackBlocked updateOutcome = "rollbackBlocked"
)

var (
	// errMarkerTooNew means the marker was written by a build with a newer schema.
	// The only safe response is to leave it completely alone — including not
	// deleting it — because a newer version that rolled back to this one must still
	// find its own marker intact when it next runs.
	errMarkerTooNew = errors.New("update marker was written by a newer version")
	// errMarkerUnusable distinguishes a marker that exists but cannot direct
	// recovery from a marker that is genuinely absent. Snapshot collection must
	// never run in the former case.
	errMarkerUnusable = errors.New("update marker exists but is unusable")
)

// markerMu serialises marker readers and writers within the process. The file is
// replaced by rename so a reader never sees a torn write, but the confirmation
// soak and a fresh apply can both be live at once.
var markerMu syncutil.Mutex

// payloadBackup pairs an installed payload file with the copy of what it
// replaced, so a rollback can put the original back without re-deriving where it
// came from. Payload extras are not installed yet; the field exists from the
// first version of the marker so adding them later is not a schema change.
type payloadBackup struct {
	TargetPath string `json:"targetPath"`
	BackupPath string `json:"backupPath"`
}

// pendingMarker is the record an update leaves behind for the boot that follows
// it. It is deliberately self-contained: the watchdog reads it before any
// config, database or network is available, because the failure it exists to
// catch is precisely a binary that cannot get that far.
type pendingMarker struct {
	InstalledAt        time.Time       `json:"installedAt"`
	OutcomeAt          time.Time       `json:"outcomeAt,omitempty"`
	StagingDir         string          `json:"stagingDir"`
	PreviousVersion    string          `json:"previousVersion"`
	OutcomeDetail      string          `json:"outcomeDetail,omitempty"`
	Trigger            updateTrigger   `json:"trigger"`
	TargetPath         string          `json:"targetPath"`
	BackupPath         string          `json:"backupPath"`
	State              markerState     `json:"state"`
	Outcome            updateOutcome   `json:"outcome,omitempty"`
	TargetVersion      string          `json:"targetVersion"`
	PlatformID         string          `json:"platformId"`
	UserDBSnapshotPath string          `json:"userDbSnapshotPath"`
	RestoringPath      string          `json:"restoringPath,omitempty"`
	PayloadBackups     []payloadBackup `json:"payloadBackups,omitempty"`
	ManifestGeneration int64           `json:"manifestGeneration"`
	Attempts           int             `json:"attempts"`
	// RollbackAttempts counts the boots that have tried to roll this update
	// back. A rollback that keeps failing leaves its marker in place so the
	// next boot resumes it, and without a count that resumption never ends.
	RollbackAttempts int `json:"rollbackAttempts,omitempty"`
	MarkerVersion    int `json:"markerVersion"`
	// BinaryReplaced records that the install swap took the target's name. A
	// swap that failed leaves the binary it started with there, and the unwind
	// has to know the difference: putting the backup back over a name that is
	// already correct is not free on a platform that has to vacate it first.
	BinaryReplaced bool `json:"binaryReplaced,omitempty"`
	// UserDBRestored records that a rollback has already written the snapshot
	// over the live database. A rollback that fails afterwards resumes on the
	// next boot, and repeating this step would discard everything written in
	// between.
	UserDBRestored bool `json:"userDbRestored,omitempty"`
}

// markerPath returns the marker file inside the updater's state directory.
func markerPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, markerFileName)
}

// loadMarker reads pending.json.
//
// A missing marker is the ordinary case and yields (nil, nil): almost every boot
// has no update to resolve. An unreadable or invalid marker returns
// errMarkerUnusable and is quarantined when possible, so startup can continue
// without mistaking it for confirmed absence and deleting its snapshots. A
// marker from a newer schema returns errMarkerTooNew and is left exactly as it
// was found.
func loadMarker(dir string) (*pendingMarker, error) {
	if dir == "" {
		return nil, nil //nolint:nilnil // no state directory means no marker, which is not an error
	}

	path := markerPath(dir)
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the platform data dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, badErr := os.Stat(path + markerBadSuffix); badErr == nil {
				return nil, fmt.Errorf("%w: quarantined marker remains at %s", errMarkerUnusable, path+markerBadSuffix)
			} else if !errors.Is(badErr, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: checking quarantined marker: %w", errMarkerUnusable, badErr)
			}
			return nil, nil //nolint:nilnil // confirmed absence is the ordinary case
		}
		log.Warn().Err(err).Str("path", path).Msg("could not read update marker")
		return nil, fmt.Errorf("%w: reading %s: %w", errMarkerUnusable, path, err)
	}

	var m pendingMarker
	if err := json.Unmarshal(data, &m); err != nil {
		log.Error().Err(err).Str("path", path).Msg("update marker is corrupt, quarantining it")
		quarantineMarker(path)
		return nil, fmt.Errorf("%w: decoding %s: %w", errMarkerUnusable, path, err)
	}

	if m.MarkerVersion > currentMarkerVersion {
		return nil, fmt.Errorf("%w: found %d, this build understands %d",
			errMarkerTooNew, m.MarkerVersion, currentMarkerVersion)
	}
	switch m.State {
	case markerInstalling, markerInstalled, markerConfirming, markerRollingBack:
		return &m, nil
	default:
		log.Error().Str("path", path).Str("state", string(m.State)).
			Msg("update marker has an unusable state, quarantining it")
		quarantineMarker(path)
		return nil, fmt.Errorf("%w: state %q", errMarkerUnusable, m.State)
	}
}

// quarantineMarker moves an unusable marker aside for diagnosis. loadMarker
// continues noticing the quarantined file on later boots so snapshots remain
// protected until an operator resolves it. Failing to move it is logged; the
// caller still refuses to treat the marker as absent.
func quarantineMarker(path string) {
	if err := os.Rename(path, path+markerBadSuffix); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("could not quarantine the update marker")
	}
}

// saveMarker replaces pending.json atomically.
func saveMarker(dir string, m *pendingMarker) error {
	if dir == "" || m == nil {
		return nil
	}
	if m.MarkerVersion > currentMarkerVersion {
		return nil
	}
	m.MarkerVersion = currentMarkerVersion

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding update marker: %w", err)
	}
	return writeFileAtomic(dir, markerFileName, data)
}

// clearMarker removes pending.json once its update has reached a terminal state.
func clearMarker(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.Remove(markerPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing the update marker: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("flushing the removed update marker: %w", err)
	}
	return nil
}
