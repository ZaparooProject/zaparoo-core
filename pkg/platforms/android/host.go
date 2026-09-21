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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/hostmedia"
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
	// FailureSourceUnavailable means the document cannot be reached through its source.
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
type Host interface {
	// InspectTarget reports whether the definition's package and activity are
	// installed and launchable. A nil error means they are; a *HostError says
	// why not.
	InspectTarget(definition *LaunchDefinition) error
	// InstalledCores lists the launcher core files the host found. scanned is
	// false when the host has not looked, which is not evidence of absence.
	InstalledCores() (files []string, scanned bool)
	// OpenDocuments begins one bounded, read-only document session that is
	// cancelled with ctx. The caller must Close it.
	OpenDocuments(ctx context.Context) (DocumentSession, error)
}

// DocumentSession is the host's document storage for one index run or one
// launch. Documents resolved in a session can only be dispatched through it.
type DocumentSession interface {
	hostmedia.Backend
	// Dispatch starts a validated definition for a document resolved in this
	// session. A *HostError says why the host refused.
	Dispatch(definition *LaunchDefinition, document hostmedia.Document) (DispatchReceipt, error)
	// CancelDispatch abandons a Dispatch in flight. It may race Dispatch.
	CancelDispatch() error
}
