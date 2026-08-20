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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// progressRecorder collects what a reporter emitted.
type progressRecorder struct {
	got []Progress
	mu  syncutil.Mutex
}

func (c *progressRecorder) fn() ProgressFn {
	return func(progress Progress) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.got = append(c.got, progress)
	}
}

func (c *progressRecorder) all() []Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Progress(nil), c.got...)
}

// A caller that wants no progress passes no function, and every reporter call
// has to survive that without a nil check at the call site.
func TestProgressReporterWithoutFn(t *testing.T) {
	t.Parallel()

	report := newProgressReporter(nil, triggerManual)
	require.Nil(t, report)

	assert.NotPanics(t, func() {
		report.setVersion("2.10.0")
		report.stage(ProgressDownloading)
		report.downloaded(1, 2)
		report.failed(errors.New("boom"))
	})
}

func TestProgressReporterStages(t *testing.T) {
	t.Parallel()

	recorder := &progressRecorder{}
	report := newProgressReporter(recorder.fn(), triggerManual)

	report.stage(ProgressChecking)
	report.setVersion("2.10.0")
	report.stage(ProgressDownloading)
	report.failed(errors.New("archive digest does not match"))

	got := recorder.all()
	require.Len(t, got, 3)

	assert.Equal(t, ProgressChecking, got[0].Stage)
	assert.Empty(t, got[0].Version, "the version is not known until the release is picked")
	assert.Equal(t, string(triggerManual), got[0].Trigger)

	assert.Equal(t, ProgressDownloading, got[1].Stage)
	assert.Equal(t, "2.10.0", got[1].Version)

	assert.Equal(t, ProgressFailed, got[2].Stage)
	assert.Equal(t, "2.10.0", got[2].Version)
	assert.Equal(t, "archive digest does not match", got[2].Error)
}

// A progress bar wants a steady trickle, not one message per network read, but
// it does need the one that says the transfer finished.
func TestProgressReporterThrottlesDownload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	recorder := &progressRecorder{}
	report := newProgressReporter(recorder.fn(), triggerAuto)
	report.now = func() time.Time { return now }

	report.downloaded(10, 100)
	report.downloaded(20, 100)
	report.downloaded(30, 100)
	now = now.Add(progressInterval)
	report.downloaded(40, 100)
	now = now.Add(time.Millisecond)
	report.downloaded(100, 100)

	got := recorder.all()
	require.Len(t, got, 3)
	assert.Equal(t, int64(10), got[0].BytesDownloaded)
	assert.Equal(t, int64(40), got[1].BytesDownloaded)
	assert.Equal(t, int64(100), got[2].BytesDownloaded, "the last byte always reports")
	assert.Equal(t, int64(100), got[2].BytesTotal)
	assert.Equal(t, string(triggerAuto), got[2].Trigger)
}

// A stage change resets the throttle so the first bytes of a download are
// reported straight away rather than half a second in.
func TestProgressReporterStageResetsThrottle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	recorder := &progressRecorder{}
	report := newProgressReporter(recorder.fn(), triggerManual)
	report.now = func() time.Time { return now }

	report.downloaded(10, 100)
	report.stage(ProgressVerifying)
	report.downloaded(20, 100)

	got := recorder.all()
	require.Len(t, got, 3)
	assert.Equal(t, int64(20), got[2].BytesDownloaded)
}

func TestProgressWriterCountsBytes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	recorder := &progressRecorder{}
	report := newProgressReporter(recorder.fn(), triggerManual)
	report.now = func() time.Time { return now }

	writer := &progressWriter{report: report, total: 6}
	written, err := writer.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, written)

	written, err = writer.Write([]byte("def"))
	require.NoError(t, err)
	assert.Equal(t, 3, written)

	got := recorder.all()
	require.Len(t, got, 2)
	assert.Equal(t, int64(3), got[0].BytesDownloaded)
	assert.Equal(t, int64(6), got[1].BytesDownloaded)
	assert.Equal(t, int64(6), got[1].BytesTotal)
}
