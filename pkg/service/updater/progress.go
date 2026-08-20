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
	"io"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// ProgressStage is where an update has got to.
type ProgressStage string

const (
	ProgressIdle        ProgressStage = "idle"
	ProgressChecking    ProgressStage = "checking"
	ProgressDownloading ProgressStage = "downloading"
	ProgressVerifying   ProgressStage = "verifying"
	ProgressProbing     ProgressStage = "probing"
	ProgressInstalling  ProgressStage = "installing"
	ProgressRestarting  ProgressStage = "restarting"
	// ProgressConfirming through ProgressRolledBack happen on the boot after
	// the restart, before any client has reconnected, so they reach clients
	// through the last result an update check reports rather than live.
	ProgressConfirming ProgressStage = "confirming"
	ProgressSucceeded  ProgressStage = "succeeded"
	ProgressRolledBack ProgressStage = "rolledBack"
	ProgressFailed     ProgressStage = "failed"
)

// progressInterval is how often a download reports its byte count. Anything
// faster is wasted on a progress bar and costs a notification round trip per
// update on hardware that has better things to do.
const progressInterval = 500 * time.Millisecond

// Progress is one update on how an update is going.
type Progress struct {
	Stage           ProgressStage `json:"stage"`
	Version         string        `json:"version,omitempty"`
	Trigger         string        `json:"trigger,omitempty"`
	Error           string        `json:"error,omitempty"`
	BytesDownloaded int64         `json:"bytesDownloaded,omitempty"`
	BytesTotal      int64         `json:"bytesTotal,omitempty"`
}

// ProgressFn receives progress updates. It is called from whichever goroutine
// is doing the work, so it must not block.
type ProgressFn func(Progress)

// progressReporter turns stage changes and downloaded bytes into Progress
// values, filling in the version and trigger every one of them carries. A nil
// reporter, or one with no function to call, silently does nothing, so callers
// never have to check.
type progressReporter struct {
	lastAt  time.Time
	emit    ProgressFn
	now     func() time.Time
	version string
	trigger string
	mu      syncutil.Mutex
}

func newProgressReporter(emit ProgressFn, trigger updateTrigger) *progressReporter {
	if emit == nil {
		return nil
	}
	return &progressReporter{emit: emit, trigger: string(trigger), now: time.Now}
}

// setVersion records the version every later update carries. It is only known
// once the release has been selected.
func (r *progressReporter) setVersion(version string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.version = version
}

// stage reports a move to a new stage.
func (r *progressReporter) stage(stage ProgressStage) {
	if r == nil {
		return
	}
	r.mu.Lock()
	progress := Progress{Stage: stage, Version: r.version, Trigger: r.trigger}
	r.lastAt = time.Time{}
	r.mu.Unlock()
	r.emit(progress)
}

// failed reports that the update stopped here.
func (r *progressReporter) failed(err error) {
	if r == nil {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	r.mu.Lock()
	progress := Progress{
		Stage:   ProgressFailed,
		Version: r.version,
		Trigger: r.trigger,
		Error:   message,
	}
	r.mu.Unlock()
	r.emit(progress)
}

// downloaded reports how far a download has got. Updates are rate limited
// except for the one that completes the transfer, which always goes out so a
// progress bar never stops short of the end.
func (r *progressReporter) downloaded(done, total int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	now := r.now()
	final := total > 0 && done >= total
	if !final && !r.lastAt.IsZero() && now.Sub(r.lastAt) < progressInterval {
		r.mu.Unlock()
		return
	}
	r.lastAt = now
	progress := Progress{
		Stage:           ProgressDownloading,
		Version:         r.version,
		Trigger:         r.trigger,
		BytesDownloaded: done,
		BytesTotal:      total,
	}
	r.mu.Unlock()
	r.emit(progress)
}

// progressWriter counts bytes on their way past and reports the running total.
// It sits in the download's writer chain so the count is of bytes that reached
// the file, not bytes the transport claims to have read.
type progressWriter struct {
	report *progressReporter
	total  int64
	done   int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	w.report.downloaded(w.done, w.total)
	return len(p), nil
}

var _ io.Writer = (*progressWriter)(nil)
