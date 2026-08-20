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
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateDirFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("data", "updater"), stateDirFor("data"))
	assert.Empty(t, stateDirFor(""))
}

func TestState_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	seen := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)

	require.NoError(t, saveState(dir, &updaterState{
		ManifestGeneration:   412,
		ManifestETag:         `"abc123"`,
		ManifestLastModified: "Mon, 17 Aug 2026 01:26:54 GMT",
		ManifestSeenAt:       seen,
	}))

	got := loadState(dir)
	assert.Equal(t, int64(412), got.ManifestGeneration)
	assert.Equal(t, `"abc123"`, got.ManifestETag)
	assert.Equal(t, "Mon, 17 Aug 2026 01:26:54 GMT", got.ManifestLastModified)
	assert.True(t, seen.Equal(got.ManifestSeenAt))
	assert.Equal(t, currentStateVersion, got.StateVersion)
}

// A device with no state yet accepts any generation, which is what makes a
// first check work at all.
func TestLoadState_Missing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, updaterState{}, loadState(filepath.Join(t.TempDir(), "nothing-here")))
	assert.Equal(t, updaterState{}, loadState(""))
}

// Failing closed here would mean one bad write permanently stops a device
// updating, which is worse than the replay the watermark prevents.
func TestLoadState_Corrupt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, stateFileName), []byte("{not json"), 0o600))

	assert.Equal(t, updaterState{}, loadState(dir))
}

// A newer build's state is still read for its generation — that field means the
// same thing in every version — but never overwritten, so rolling back to an
// older build cannot rewind the watermark it wrote.
func TestState_NewerVersionIsReadButNotOverwritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	future := []byte(`{"stateVersion":99,"manifestGeneration":500,"manifestETag":"\"z\""}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, stateFileName), future, 0o600))

	got := loadState(dir)
	assert.Equal(t, int64(500), got.ManifestGeneration)
	assert.Equal(t, 99, got.StateVersion)

	require.NoError(t, saveState(dir, &got))

	after, err := os.ReadFile(filepath.Join(dir, stateFileName)) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Equal(t, future, after)
}

func TestRecordUpdateResult_DoesNotOverwriteCorruptState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)
	corrupt := []byte("{not json")
	require.NoError(t, os.WriteFile(path, corrupt, stateFilePerm))

	err := recordUpdateResult(dir, &updateResult{
		At:      time.Now().UTC(),
		Outcome: outcomeSucceeded,
	})
	require.Error(t, err)
	after, readErr := os.ReadFile(path) //nolint:gosec // test-owned path
	require.NoError(t, readErr)
	assert.Equal(t, corrupt, after)
}

func TestMarkUpdateResultReported_DoesNotOverwriteCorruptState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)
	corrupt := []byte("{not json")
	require.NoError(t, os.WriteFile(path, corrupt, stateFilePerm))

	err := markUpdateResultReported(dir, &updateResult{
		At:      time.Now().UTC(),
		Outcome: outcomeSucceeded,
	})
	require.Error(t, err)
	after, readErr := os.ReadFile(path) //nolint:gosec // test-owned path
	require.NoError(t, readErr)
	assert.Equal(t, corrupt, after)
}

func TestSaveState_NoDir(t *testing.T) {
	t.Parallel()

	require.NoError(t, saveState("", &updaterState{ManifestGeneration: 1}))
}

func TestCachedManifest_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	body := []byte("manifest_version: 1\ngeneration: 412\n")

	require.NoError(t, saveCachedManifest(dir, body))
	assert.Equal(t, body, loadCachedManifest(dir))
}

func TestLoadCachedManifest_MissingOrEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.Nil(t, loadCachedManifest(dir))
	assert.Nil(t, loadCachedManifest(""))

	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestCacheName), nil, 0o600))
	assert.Nil(t, loadCachedManifest(dir))
}

// The cap stops a cache that grew past what the fetch path would ever accept
// from being read back in.
func TestLoadCachedManifest_OverSizeLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	oversized := make([]byte, maxCachedManifestLen+1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestCacheName), oversized, 0o600))

	assert.Nil(t, loadCachedManifest(dir))
}

// A reader must see either the old contents or the new ones, never a partial
// write, and no temporary files may be left behind.
func TestWriteFileAtomic_ReplacesAndLeavesNoTemps(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, writeFileAtomic(dir, "f", []byte("first")))
	require.NoError(t, writeFileAtomic(dir, "f", []byte("second")))

	data, err := os.ReadFile(filepath.Join(dir, "f")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Equal(t, "second", string(data))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "f", entries[0].Name())
}

func TestWriteFileAtomic_ReportsDirectorySyncFailure(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	errSync := errors.New("directory sync failed")
	err := writeFileAtomicWithSync(dir, markerFileName, []byte("{}"), func(string) error {
		return errSync
	})
	require.ErrorIs(t, err, errSync)

	// Rename may already be visible, but callers must retain rollback files
	// because durability across reboot was not established.
	assert.FileExists(t, filepath.Join(dir, markerFileName))
}

func TestWriteFileAtomic_FilePermissions(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, writeFileAtomic(dir, stateFileName, []byte("{}")))

	info, err := os.Stat(filepath.Join(dir, stateFileName))
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		// Windows has no POSIX mode bits, so Chmod only toggles the read-only
		// flag and Perm reports 0666. The write itself is still worth asserting.
		return
	}
	assert.Equal(t, os.FileMode(stateFilePerm), info.Mode().Perm())
}

// Delivery peeks a result, writes it to the inbox, then acknowledges it. If a
// newer outcome is recorded in between, the acknowledgement must not swallow it.
func TestMarkUpdateResultReported_OnlyAcknowledgesTheResultItWasGiven(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rolledBack := &updateResult{
		At:          time.Date(2026, 8, 18, 4, 30, 0, 0, time.UTC),
		Outcome:     outcomeRolledBack,
		FromVersion: "2.1.0",
		ToVersion:   "2.2.0",
	}
	require.NoError(t, recordUpdateResult(dir, rolledBack))
	require.Equal(t, rolledBack.Outcome, peekUpdateResult(dir).Outcome)

	succeeded := &updateResult{
		At:          time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC),
		Outcome:     outcomeSucceeded,
		FromVersion: "2.1.0",
		ToVersion:   "2.3.0",
	}
	require.NoError(t, recordUpdateResult(dir, succeeded))
	require.NoError(t, markUpdateResultReported(dir, rolledBack))

	pending := peekUpdateResult(dir)
	require.NotNil(t, pending, "the newer result must still be waiting to be shown")
	assert.Equal(t, outcomeSucceeded, pending.Outcome)
	assert.Equal(t, "2.3.0", pending.ToVersion)
}

// Recording the same outcome again — the watchdog re-finalizing after an
// interrupted boot — must not resurrect a result the user has already seen.
func TestRecordUpdateResult_RepeatDoesNotUnreportADeliveredResult(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	res := &updateResult{
		At:          time.Date(2026, 8, 18, 4, 30, 0, 0, time.UTC),
		Outcome:     outcomeSucceeded,
		FromVersion: "2.1.0",
		ToVersion:   "2.2.0",
	}
	require.NoError(t, recordUpdateResult(dir, res))
	require.NoError(t, markUpdateResultReported(dir, res))
	require.Nil(t, peekUpdateResult(dir))

	require.NoError(t, recordUpdateResult(dir, res))
	assert.Nil(t, peekUpdateResult(dir))
}

func TestUpdateResult_NilIsNotRecorded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, recordUpdateResult(dir, nil))
	require.NoError(t, markUpdateResultReported(dir, nil))

	assert.Nil(t, peekUpdateResult(dir))
	assert.NoFileExists(t, filepath.Join(dir, stateFileName))
}

func TestRecordDeferral_KeepsSinceAcrossReasons(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))

	first := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, first)
	assert.Equal(t, ReasonActiveMedia, first.Reason)
	assert.False(t, first.Since.IsZero())
	assert.Equal(t, time.UTC, first.Since.Location())

	// The clock an automatic install runs down starts at the first deferral of
	// a version, not at the most recent one, so someone who keeps the device
	// busy cannot push it back forever.
	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonAPIBusy))
	second := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, second)
	assert.Equal(t, ReasonAPIBusy, second.Reason)
	assert.Equal(t, first.Since, second.Since)
}

func TestRecordDeferral_ReusesExistingNonZeroTimestamp(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	prior := time.Date(2026, 8, 1, 12, 30, 0, 0, time.FixedZone("prior", 9*60*60))
	require.NoError(t, saveState(dir, &updaterState{Deferral: &updateDeferral{
		Since: prior, Version: "v2.5.0", Reason: ReasonActiveMedia,
	}}))

	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonAPIBusy))
	deferral := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, deferral)
	assert.True(t, prior.Equal(deferral.Since))
	assert.Equal(t, ReasonAPIBusy, deferral.Reason)
}

func TestRecordDeferral_SameReasonDoesNotRewrite(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	first := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, first)

	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	second := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, second)
	assert.Equal(t, first.Since, second.Since)
	assert.Equal(t, first.Reason, second.Reason)
}

func TestRecordDeferral_NewVersionRestartsTheClock(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	old := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, old)

	// Backdate the deferral so a fresh timestamp is distinguishable without
	// waiting for the wall clock to move.
	stale := old.Since.Add(-48 * time.Hour)
	st := loadState(dir)
	st.Deferral.Since = stale
	require.NoError(t, saveState(dir, &st))

	require.NoError(t, recordDeferral(dir, "v2.6.0", ReasonActiveMedia))
	assert.Nil(t, peekDeferral(dir, "v2.5.0"))
	fresh := peekDeferral(dir, "v2.6.0")
	require.NotNil(t, fresh)
	assert.True(t, fresh.Since.After(stale), "a different version waits from scratch")
}

func TestPeekDeferral_OtherVersionOrNothingRecorded(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	assert.Nil(t, peekDeferral(dir, "v2.5.0"))

	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	assert.Nil(t, peekDeferral(dir, "v2.6.0"))
}

func TestClearDeferral(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	require.NotNil(t, peekDeferral(dir, "v2.5.0"))

	require.NoError(t, clearDeferral(dir, ""))
	assert.Nil(t, peekDeferral(dir, "v2.5.0"))

	// Clearing again is not an error, because an update that was never held up
	// still clears the deferral when it installs.
	require.NoError(t, clearDeferral(dir, ""))

	require.NoError(t, recordDeferral(dir, "v2.6.0", ReasonActiveMedia))
	require.NoError(t, clearDeferral(dir, "v2.5.0"))
	require.NotNil(t, peekDeferral(dir, "v2.6.0"), "stale cleanup must not clear a replacement deferral")
}

func TestRecordDeferral_KeepsOtherState(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	seen := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
	require.NoError(t, saveState(dir, &updaterState{
		ManifestSeenAt:     seen,
		ManifestETag:       "etag-1",
		ManifestGeneration: 7,
	}))

	require.NoError(t, recordDeferral(dir, "v2.5.0", ReasonActiveMedia))
	require.NoError(t, clearDeferral(dir, ""))

	st := loadState(dir)
	assert.Equal(t, "etag-1", st.ManifestETag)
	assert.Equal(t, int64(7), st.ManifestGeneration)
	assert.True(t, seen.Equal(st.ManifestSeenAt))
}
