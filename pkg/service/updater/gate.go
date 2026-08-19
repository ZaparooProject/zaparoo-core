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
	"context"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
)

// Mode is who asked for the update.
type Mode string

const (
	// ModeManual is a person pressing update in a client. They are at the
	// device, they can see what it is doing, and they can be asked about
	// anything short of a real risk to their data.
	ModeManual Mode = "manual"
	// ModeAuto is the device deciding for itself. Nobody is watching, so
	// anything that would surprise a user is a reason to wait.
	ModeAuto Mode = "auto"
)

// autoInstallDeadline is how long a version may sit deferred by a soft signal
// before an automatic install goes ahead regardless. A cabinet that plays
// something every waking hour would otherwise never be idle and never get a
// security fix.
const autoInstallDeadline = 24 * time.Hour

// Battery charge an install needs, as a percentage. Automatic installs ask for
// more because nobody is there to plug the device in when it gets close.
const (
	manualBatteryFloor = 20
	autoBatteryFloor   = 40
)

// Reasons an update cannot be applied right now. These are the machine-readable
// half of a refusal; clients match on them to decide what to say.
const (
	ReasonMediaIndexing   = "mediaIndexing"
	ReasonMediaOptimizing = "mediaOptimizing"
	ReasonMediaScraping   = "mediaScraping"
	ReasonBackupActive    = "backupActive"
	ReasonReaderWriting   = "readerWriting"
	ReasonRestoreActive   = "restoreActive"
	ReasonActiveMedia     = "activeMedia"
	ReasonBackgroundMedia = "backgroundMedia"
	ReasonActivePlaylist  = "activePlaylist"
	ReasonPowerLow        = "powerLow"
	ReasonPowerUnknown    = "powerUnknown"
	ReasonAPIBusy         = "apiBusy"
)

// GateDeps is everything the gate needs to look at, as plain functions so the
// gate can be tested without a running service. A nil function means the caller
// has nothing to report for that signal and it is skipped.
type GateDeps struct {
	// IndexingStatus, OptimizationStatus and ScrapingStatus each return a
	// mediadb status string. An error is treated as "not running": a database
	// that cannot answer is a problem for the caller to notice elsewhere, and
	// refusing every update because of it would leave the device unfixable.
	IndexingStatus     func() (string, error)
	OptimizationStatus func() (string, error)
	ScrapingStatus     func() (string, error)

	// BackupActive reports whether a backup, restore or upload is running.
	BackupActive func() bool
	// ReaderWriteActive reports whether a reader is part-way through writing
	// a token.
	ReaderWriteActive func() bool
	// AcquireRestore takes the restore gate, which the install then holds so
	// a restore cannot start underneath it. The release function it returns
	// is carried on the decision.
	AcquireRestore func() (func(), error)
	// AcquireMediaGate stops anything new launching and waits for what is
	// already launching to settle, so the install's own look at what is
	// playing cannot be overtaken by a launch that starts a moment later. The
	// install holds it until the restart.
	//
	// The gate takes this after AcquireRestore, which is the order the rest of
	// the service takes those two locks in. Taking them the other way round is
	// a lock inversion, which is why neither is the caller's to take.
	AcquireMediaGate func(context.Context) (func(), error)

	// ActiveMedia, BackgroundMedia and ActivePlaylist report what the user
	// would lose if the service restarted now.
	ActiveMedia     func() bool
	BackgroundMedia func() bool
	ActivePlaylist  func() bool

	// Power reports where the device's power is coming from.
	Power func() power.Status

	// WaitForIdle blocks until the API has been quiet for a while. Only
	// automatic installs wait for it; a person pressing update is the request
	// that would otherwise stop it ever being idle.
	WaitForIdle func(context.Context) error

	// DeferredSince is when this version was first put off by a soft signal,
	// or the zero time if it has not been. Once that is more than
	// autoInstallDeadline ago the soft signals stop counting.
	DeferredSince func() time.Time

	// Now reads the clock. Tests replace it.
	Now func() time.Time
}

// GateDecision is the gate's answer.
type GateDecision struct {
	// Release gives back whatever the gate took. It is never nil, so callers can
	// defer it without checking, and it does nothing when the gate was never
	// taken.
	Release func()
	// Reason is the machine-readable refusal, empty when OK.
	Reason string
	// Message says the same thing in words, for a client with nothing better
	// to show.
	Message string
	// Forceable means a person may go ahead anyway. It is never true for
	// automatic installs, and never true for anything that risks data rather
	// than the user's session.
	Forceable bool
	// Expires means this is a soft signal that an automatic install may
	// ignore once the version has waited out autoInstallDeadline.
	Expires bool
	// OK means the update may go ahead.
	OK bool
}

// blocked builds a refusal that has not taken the restore gate.
func blocked(reason, message string, forceable, expires bool) GateDecision {
	return GateDecision{
		Release:   func() {},
		Reason:    reason,
		Message:   message,
		Forceable: forceable,
		Expires:   expires,
	}
}

// CanApplyUpdate reports whether an update may be installed right now.
//
// A decision that is OK carries the gates the install needs held, so the caller
// must call Release once the install has finished or failed. force lets a
// person past the signals that are only about their own session; it never gets
// past a signal that risks their data, and mode auto ignores it entirely.
//
// The error is separate from the decision on purpose: a refusal is an answer,
// but a cancelled request is not an answer at all.
func CanApplyUpdate(ctx context.Context, deps *GateDeps, mode Mode, force bool) (GateDecision, error) {
	auto := mode == ModeAuto
	if auto {
		force = false
	}
	expired := auto && softSignalsExpired(deps)

	if decision, blocking := checkDataSignals(deps); blocking {
		return decision, nil
	}
	if decision, blocking := checkPower(deps, mode, force); blocking {
		return decision, nil
	}
	// The idle wait comes before the gates are held, because waiting for the
	// API to go quiet while blocking every launch is how an automatic install
	// makes the device look broken.
	if decision, blocking := checkIdle(ctx, deps, auto, expired); blocking {
		return decision, nil
	}

	release, decision, err := acquireHolds(ctx, deps)
	if err != nil || !decision.OK {
		return decision, err
	}

	// Read after the media gate is held, so a launch cannot start between the
	// answer and the install that would close it.
	if blocking, isBlocking := checkSessionSignals(deps, auto, force, expired); isBlocking {
		release()
		return blocking, nil
	}
	return GateDecision{OK: true, Release: release}, nil
}

// acquireHolds takes the restore gate and then the media gate, in that order,
// and hands back one release function for both.
func acquireHolds(ctx context.Context, deps *GateDeps) (func(), GateDecision, error) {
	releaseRestore := func() {}
	if deps.AcquireRestore != nil {
		acquired, err := deps.AcquireRestore()
		if err != nil {
			//nolint:nilerr // a restore already running is a refusal, not a failure
			return nil, blocked(
				ReasonRestoreActive,
				"a backup restore is in progress",
				false, false,
			), nil
		}
		releaseRestore = acquired
	}

	releaseMedia := func() {}
	if deps.AcquireMediaGate != nil {
		acquired, err := deps.AcquireMediaGate(ctx)
		if err != nil {
			releaseRestore()
			return nil, GateDecision{Release: func() {}},
				fmt.Errorf("waiting for media activity to settle: %w", err)
		}
		releaseMedia = acquired
	}

	return func() {
		releaseMedia()
		releaseRestore()
	}, GateDecision{OK: true, Release: func() {}}, nil
}

// checkDataSignals covers the work that would lose or corrupt something if the
// service went away mid-write. None of it can be forced and none of it expires.
func checkDataSignals(deps *GateDeps) (GateDecision, bool) {
	statuses := []struct {
		read    func() (string, error)
		reason  string
		message string
	}{
		{deps.IndexingStatus, ReasonMediaIndexing, "the media database is being generated"},
		{deps.OptimizationStatus, ReasonMediaOptimizing, "the media database is being optimized"},
		{deps.ScrapingStatus, ReasonMediaScraping, "media metadata is being downloaded"},
	}
	for _, status := range statuses {
		if statusBusy(status.read) {
			return blocked(status.reason, status.message, false, false), true
		}
	}

	if deps.BackupActive != nil && deps.BackupActive() {
		return blocked(ReasonBackupActive, "a backup or restore is in progress", false, false), true
	}
	if deps.ReaderWriteActive != nil && deps.ReaderWriteActive() {
		return blocked(ReasonReaderWriting, "a token is being written", false, false), true
	}
	return GateDecision{}, false
}

// checkIdle makes an automatic install wait for the API to go quiet. A person
// pressing update is the request that would otherwise stop it ever being idle,
// so it does not apply to them.
func checkIdle(ctx context.Context, deps *GateDeps, auto, expired bool) (GateDecision, bool) {
	if !auto || expired || deps.WaitForIdle == nil {
		return GateDecision{}, false
	}
	if err := deps.WaitForIdle(ctx); err != nil {
		return blocked(ReasonAPIBusy, "the device is still handling requests", false, true), true
	}
	return GateDecision{}, false
}

// checkSessionSignals covers what the user would lose if the service restarted
// now. A person can decide that for themselves; an automatic install waits,
// until the version has waited long enough.
func checkSessionSignals(deps *GateDeps, auto, force, expired bool) (GateDecision, bool) {
	if expired {
		return GateDecision{}, false
	}

	media := []struct {
		active  func() bool
		reason  string
		message string
	}{
		{deps.ActiveMedia, ReasonActiveMedia, "media is playing"},
		{deps.BackgroundMedia, ReasonBackgroundMedia, "media is playing in the background"},
		{deps.ActivePlaylist, ReasonActivePlaylist, "a playlist is running"},
	}
	for _, signal := range media {
		if signal.active == nil || !signal.active() {
			continue
		}
		if force {
			continue
		}
		return blocked(signal.reason, signal.message, !auto, true), true
	}
	return GateDecision{}, false
}

// softSignalsExpired reports whether this version has been put off for longer
// than an automatic install is willing to wait.
func softSignalsExpired(deps *GateDeps) bool {
	if deps.DeferredSince == nil {
		return false
	}
	since := deps.DeferredSince()
	if since.IsZero() {
		return false
	}
	now := time.Now
	if deps.Now != nil {
		now = deps.Now
	}
	return now().Sub(since) >= autoInstallDeadline
}

// checkPower refuses an install the device may not have the charge to finish.
// A known charge below the floor is a hard block: force is a person saying they
// accept losing what is playing, not a person able to make the battery last.
func checkPower(deps *GateDeps, mode Mode, force bool) (GateDecision, bool) {
	if deps.Power == nil {
		return GateDecision{}, false
	}

	floor := manualBatteryFloor
	if mode == ModeAuto {
		floor = autoBatteryFloor
	}

	status := deps.Power()
	switch status.Source {
	case power.SourceNoBattery, power.SourceExternal:
		return GateDecision{}, false
	case power.SourceBattery:
		if status.Percent >= floor {
			return GateDecision{}, false
		}
		return blocked(
			ReasonPowerLow,
			fmt.Sprintf(
				"the battery is at %d%%, and an update needs at least %d%% or a charger",
				status.Percent, floor,
			),
			false, false,
		), true
	case power.SourceUnknown:
		// The device may be on battery and there is no way to tell. An
		// automatic install waits for a reading; a person can decide to go
		// ahead once they have been told the charge is unknown.
		if mode == ModeManual && force {
			return GateDecision{}, false
		}
		return blocked(
			ReasonPowerUnknown,
			"the battery level could not be read",
			mode == ModeManual, false,
		), true
	default:
		return GateDecision{}, false
	}
}

// statusBusy reports whether a mediadb status function says work is running or
// queued to run.
func statusBusy(read func() (string, error)) bool {
	if read == nil {
		return false
	}
	status, err := read()
	if err != nil {
		return false
	}
	return status == mediadb.IndexingStatusRunning || status == mediadb.IndexingStatusPending
}

// GateError is a refusal from the gate, as an error, so a check made deep
// inside an install can be recognised again by the caller that started it.
type GateError struct {
	Reason    string
	Message   string
	Forceable bool
}

func (e *GateError) Error() string {
	return e.Message
}

// Err turns a refusal into an error. It returns nil for a decision that is OK.
func (d *GateDecision) Err() error {
	if d.OK {
		return nil
	}
	return &GateError{
		Reason:    d.Reason,
		Message:   d.Message,
		Forceable: d.Forceable,
	}
}

// PowerReady re-runs only the power part of the gate. The install calls it once
// the download is finished, because a download long enough to matter is also
// long enough to outlive a charger being unplugged.
func PowerReady(deps *GateDeps, mode Mode, force bool) GateDecision {
	if decision, blocking := checkPower(deps, mode, force); blocking {
		return decision
	}
	return GateDecision{OK: true, Release: func() {}}
}
