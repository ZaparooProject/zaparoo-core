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
	// currentStateVersion is the schema of state.json this build writes.
	currentStateVersion = 1

	stateFileName        = "state.json"
	manifestCacheName    = "manifest.yaml"
	stateDirPerm         = 0o750
	stateFilePerm        = 0o600
	maxCachedManifestLen = maxManifestBytes
)

// stateMu serialises readers and writers of state.json within the process. The
// file is replaced by rename so a concurrent reader never sees a torn write,
// but two updates racing could still lose a generation without this.
var stateMu syncutil.Mutex

// updaterState is the small amount of update bookkeeping that has to outlive
// the process. It lives in the data directory rather than the user database on
// purpose: the database is included in backups, so a watermark stored there
// would roll backwards whenever an old backup was restored, which is a
// self-inflicted downgrade window.
type updaterState struct {
	ManifestSeenAt       time.Time       `json:"manifestSeenAt"`
	LastResult           *updateResult   `json:"lastResult,omitempty"`
	Deferral             *updateDeferral `json:"deferral,omitempty"`
	ManifestETag         string          `json:"manifestETag"`
	ManifestLastModified string          `json:"manifestLastModified"`
	ManifestGeneration   int64           `json:"manifestGeneration"`
	StateVersion         int             `json:"stateVersion"`
}

// updateDeferral records that an automatic install has been putting a version
// off, and since when. It is what lets a check say the device is waiting for a
// quiet moment instead of leaving it looking stalled, and it is what the
// 24-hour deadline is measured from.
type updateDeferral struct {
	Since   time.Time `json:"since"`
	Version string    `json:"version"`
	Reason  string    `json:"reason"`
}

// updateResult records the terminal outcome of an update for the boot that
// follows it.
type updateResult struct {
	At          time.Time     `json:"at"`
	Outcome     updateOutcome `json:"outcome"`
	FromVersion string        `json:"fromVersion"`
	ToVersion   string        `json:"toVersion"`
	Detail      string        `json:"detail,omitempty"`
	Reported    bool          `json:"reported"`
}

// stateDirFor returns the updater's private directory inside the data dir.
func stateDirFor(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "updater")
}

// loadState reads state.json for non-persisting callers. A missing, unreadable
// or corrupt file yields a zero state so one bad bookkeeping file does not stop
// update checks. Read-modify-write callers must use loadStateWithError instead.
func loadState(dir string) updaterState {
	st, err := loadStateWithError(dir)
	if err != nil {
		log.Warn().Err(err).Msg("could not load updater state, treating as unseen")
		return updaterState{}
	}
	return st
}

func loadStateWithError(dir string) (updaterState, error) {
	if dir == "" {
		return updaterState{}, nil
	}

	//nolint:gosec // path is derived from the platform data dir
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return updaterState{}, nil
		}
		return updaterState{}, fmt.Errorf("reading updater state: %w", err)
	}

	var st updaterState
	if err := json.Unmarshal(data, &st); err != nil {
		return updaterState{}, fmt.Errorf("decoding updater state: %w", err)
	}

	// A newer build may have written fields this one does not know about. The
	// generation means the same thing in every version, so it is still honoured
	// for the replay check; saveState is what refuses to overwrite the file.
	if st.StateVersion > currentStateVersion {
		log.Warn().
			Int("found", st.StateVersion).
			Int("understood", currentStateVersion).
			Msg("updater state was written by a newer version, not updating it")
	}
	return st, nil
}

// saveState replaces state.json atomically.
func saveState(dir string, st *updaterState) error {
	if dir == "" {
		return nil
	}
	if st.StateVersion > currentStateVersion {
		return nil
	}
	st.StateVersion = currentStateVersion

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding updater state: %w", err)
	}
	return writeFileAtomic(dir, stateFileName, data)
}

// loadCachedManifest returns the manifest bytes kept alongside the cache
// validators. The caller re-verifies them, so a caller receiving nil simply
// refetches.
func loadCachedManifest(dir string) []byte {
	if dir == "" {
		return nil
	}

	path := filepath.Join(dir, manifestCacheName)
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 || info.Size() > maxCachedManifestLen {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the platform data dir
	if err != nil {
		log.Debug().Err(err).Msg("could not read cached update manifest")
		return nil
	}
	// Re-checked against what was actually read: the file can grow between the
	// stat and the read, and the cap has to hold for the bytes in hand.
	if int64(len(data)) > maxCachedManifestLen {
		return nil
	}
	return data
}

func saveCachedManifest(dir string, data []byte) error {
	if dir == "" {
		return nil
	}
	return writeFileAtomic(dir, manifestCacheName, data)
}

// writeFileAtomic writes name inside dir via a temporary file and a rename, so
// a reader either sees the previous contents or the new ones and never a
// partial write. Success means both file contents and directory entry reached a
// durability barrier; callers may delete rollback prerequisites afterwards.
func writeFileAtomic(dir, name string, data []byte) error {
	return writeFileAtomicWithSync(dir, name, data, syncDir)
}

func writeFileAtomicWithSync(
	dir, name string, data []byte, syncDirectory func(string) error,
) error {
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("creating updater state directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return fmt.Errorf("creating temporary updater state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Only fires on the failure paths; the rename below has already moved
		// the file away by the time a successful write returns.
		if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Debug().Err(removeErr).Msg("could not remove temporary updater state file")
		}
	}()

	if err := tmp.Chmod(stateFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting updater state file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing updater state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing updater state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing updater state file: %w", err)
	}

	if err := replaceStateFile(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("replacing updater state file: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("flushing updater state file %q: %w", name, err)
	}
	return nil
}

// recordUpdateResult stores how an update finished so the boot after it can say
// so. Terminal cleanup keeps its marker until this succeeds, allowing a later
// boot to retry result persistence without repeating rollback.
//
// Callers already holding markerMu take it before stateMu; nothing takes them
// the other way round.
func recordUpdateResult(dir string, res *updateResult) error {
	if res == nil {
		return nil
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	st, err := loadStateWithError(dir)
	if err != nil {
		return fmt.Errorf("loading updater state before recording result: %w", err)
	}
	if sameUpdateResult(st.LastResult, res) {
		return nil
	}
	copyResult := *res
	copyResult.Reported = false
	st.LastResult = &copyResult
	if err := saveState(dir, &st); err != nil {
		return fmt.Errorf("recording the update result for the next boot: %w", err)
	}
	return nil
}

// peekUpdateResult returns an update result that has not been shown yet without
// acknowledging it. Delivery acknowledges only after the inbox write succeeds.
func peekUpdateResult(dir string) *updateResult {
	stateMu.Lock()
	defer stateMu.Unlock()

	st := loadState(dir)
	if st.LastResult == nil || st.LastResult.Reported {
		return nil
	}
	res := *st.LastResult
	return &res
}

func markUpdateResultReported(dir string, res *updateResult) error {
	if res == nil {
		return nil
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	st, err := loadStateWithError(dir)
	if err != nil {
		return fmt.Errorf("loading updater state before marking result reported: %w", err)
	}
	if st.LastResult == nil || st.LastResult.Reported || !sameUpdateResult(st.LastResult, res) {
		return nil
	}
	st.LastResult.Reported = true
	if err := saveState(dir, &st); err != nil {
		return fmt.Errorf("marking the update result as reported: %w", err)
	}
	return nil
}

func sameUpdateResult(a, b *updateResult) bool {
	return a != nil && b != nil &&
		a.At.Equal(b.At) &&
		a.Outcome == b.Outcome &&
		a.FromVersion == b.FromVersion &&
		a.ToVersion == b.ToVersion &&
		a.Detail == b.Detail
}

// recordDeferral notes that an automatic install of version was put off for
// reason. The start time survives repeated deferrals of the same version, since
// that is what the deadline is measured from; a different version starts the
// clock again.
func recordDeferral(dir, version, reason string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	st, err := loadStateWithError(dir)
	if err != nil {
		return fmt.Errorf("loading updater state before recording a deferral: %w", err)
	}
	if st.Deferral != nil && st.Deferral.Version == version && st.Deferral.Reason == reason {
		return nil
	}

	since := time.Now()
	if st.Deferral != nil && st.Deferral.Version == version && !st.Deferral.Since.IsZero() {
		since = st.Deferral.Since
	}
	st.Deferral = &updateDeferral{Since: since, Version: version, Reason: reason}
	if err := saveState(dir, &st); err != nil {
		return fmt.Errorf("recording the update deferral: %w", err)
	}
	return nil
}

// clearDeferral forgets any deferral, for when the update goes ahead or the
// version it was waiting on is no longer the one on offer.
func clearDeferral(dir string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	st, err := loadStateWithError(dir)
	if err != nil {
		return fmt.Errorf("loading updater state before clearing a deferral: %w", err)
	}
	if st.Deferral == nil {
		return nil
	}
	st.Deferral = nil
	if err := saveState(dir, &st); err != nil {
		return fmt.Errorf("clearing the update deferral: %w", err)
	}
	return nil
}

// peekDeferral returns the recorded deferral for version, or nil when the
// device is not waiting on that version.
func peekDeferral(dir, version string) *updateDeferral {
	stateMu.Lock()
	defer stateMu.Unlock()

	st := loadState(dir)
	if st.Deferral == nil || st.Deferral.Version != version {
		return nil
	}
	deferral := *st.Deferral
	return &deferral
}
