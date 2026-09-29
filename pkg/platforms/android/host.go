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

package android

import (
	"context"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// FailureReason is a stable code for why the host could not inspect or start
// a launch target. It carries no user-facing text.
type FailureReason string

const (
	// FailureHostUnavailable means the host could not be reached at all.
	FailureHostUnavailable FailureReason = "host-unavailable"
	// FailureInvalidResponse means the host answered with something malformed.
	FailureInvalidResponse FailureReason = "invalid-response"
	// FailureNotInstalled means the target package is not installed.
	FailureNotInstalled FailureReason = "not-installed"
	// FailureActivityUnavailable means the package lacks a launchable target activity.
	FailureActivityUnavailable FailureReason = "activity-unavailable"
	// FailureStorageDenied means the target app has no storage permission.
	FailureStorageDenied FailureReason = "storage-denied"
	// FailureStorageVersion means the target build's storage access cannot be verified.
	FailureStorageVersion FailureReason = "storage-version"
	// FailureProviderUnsupported means the document provider cannot yield a local file.
	FailureProviderUnsupported FailureReason = "provider-unsupported"
	// FailureStorageUnmounted means the volume holding the media is not mounted.
	FailureStorageUnmounted FailureReason = "storage-unmounted"
	// FailureSourceUnavailable means the media file cannot be reached in its media folder.
	FailureSourceUnavailable FailureReason = "source-unavailable"
	// FailureForegroundRequired means the host UI must be in front to start an activity.
	FailureForegroundRequired FailureReason = "foreground-required"
	// FailureCancelled means the dispatch was cancelled before it started.
	FailureCancelled FailureReason = "cancelled"
	// FailureOutcomeUnknown means the host cannot tell whether the target started.
	FailureOutcomeUnknown FailureReason = "outcome-unknown"
	// FailureRefused covers every other refusal.
	FailureRefused FailureReason = "refused"
)

// HostError is the typed failure a Host returns. Any other error from a Host
// is treated as FailureHostUnavailable.
type HostError struct {
	Reason FailureReason
}

func (e *HostError) Error() string {
	return "android host: " + string(e.Reason)
}

// failureReason extracts the typed reason from a host error.
func failureReason(err error) FailureReason {
	var hostErr *HostError
	if errors.As(err, &hostErr) && hostErr.Reason != "" {
		return hostErr.Reason
	}
	return FailureHostUnavailable
}

// DispatchReceipt names the component the host actually started, so the
// platform can confirm it is the one the definition asked for.
type DispatchReceipt struct {
	Package  string
	Activity string
	Strategy string
}

// Host is the embedding host's authority over the Android framework. The
// platform never touches packages, intents or document providers itself.
// Implementations must be safe for concurrent use.
//
// Media folders the user granted to the host are named by the host's own
// reference for each, which Core treats as opaque. Core turns references into
// source roots and never shows or stores one.
type Host interface {
	// InspectTarget reports whether the definition's package and activity are
	// installed and launchable. A nil error means they are; a *HostError says
	// why not.
	InspectTarget(definition *LaunchDefinition) error
	// InstalledCores lists the launcher core files the host found. scanned is
	// false when the host has not looked, which is not evidence of absence.
	InstalledCores() (files []string, scanned bool)
	// MediaFolders returns the references of the media folders Core may
	// index now. A folder whose grant was revoked is not listed.
	MediaFolders(ctx context.Context) ([]string, error)
	// ReadMediaDir lists the directory at segments below the folder named by
	// reference; no segments lists the folder itself.
	ReadMediaDir(ctx context.Context, reference string, segments []string) ([]platforms.SourceEntry, error)
	// ReadFile returns up to limit+1 bytes of the file at segments below the
	// folder named by reference, so an oversized file is detectable. It is
	// used only at launch, to read the small amount of a media file's own
	// content a launch needs; indexing never reads a file's content.
	ReadFile(ctx context.Context, reference string, segments []string, limit int64) ([]byte, error)
	// Dispatch starts a validated definition for the file at segments below
	// the folder named by reference. Cancelling ctx abandons a dispatch in
	// flight. A *HostError says why the host refused.
	Dispatch(
		ctx context.Context,
		definition *LaunchDefinition,
		reference string,
		segments []string,
	) (DispatchReceipt, error)
	// InstalledApps lists the launchable apps the host found. scanned is false
	// when the host has not looked, which is not evidence of absence.
	InstalledApps() (apps []AppInfo, scanned bool)
	// AppIcon returns a readable, app-private PNG for a launchable package.
	// An unavailable icon is not evidence that the app is absent.
	AppIcon(packageName string) (path string, err error)
	// DispatchApp starts a definition that carries no media. A *HostError says
	// why the host refused.
	DispatchApp(definition *LaunchDefinition) (DispatchReceipt, error)
	// ForegroundState reads a fresh framework snapshot: boot identity, Usage
	// Access permission, and screen/keyguard state, on both the wall and
	// elapsed clocks. It is read fresh before every dispatch and
	// reconciliation pass; the platform never caches it.
	ForegroundState() (ForegroundState, error)
	// ForegroundEvents queries a bounded window of privacy-filtered
	// foreground evidence for one already-dispatched launch's whole
	// package - never a specific activity, so an intent that forwards
	// through more than one activity of the same app still reads as one
	// session. Cancelling ctx abandons a query in flight.
	ForegroundEvents(
		ctx context.Context, launchID, target string, fromMs, toMs int64,
	) (database.ForegroundEvidence, error)
}

// ForegroundState is a host-owned snapshot taken before intent dispatch or a
// reconciliation pass. It also supplies the boot and elapsed clocks needed to
// reject cross-boot replay.
type ForegroundState struct {
	BootID      string
	Permission  string
	SampledMs   int64
	ElapsedMs   int64
	Version     int
	Interactive bool
	Unlocked    bool
}

// valid rejects a snapshot with missing or nonsensical fields. The host is
// expected to always answer with a valid one; treating a broken answer as
// merely "untracked" would hide a host bug from anyone watching for it.
func (s ForegroundState) valid() bool {
	return s.BootID != "" && s.SampledMs > 0 && s.ElapsedMs > 0 &&
		(s.Permission == "granted" || s.Permission == "denied")
}

// AppInfo is one launchable app the host found. Label is the app's own
// display name, which the host reads from the platform, never Core.
type AppInfo struct {
	Package  string
	Activity string
	Label    string
}
