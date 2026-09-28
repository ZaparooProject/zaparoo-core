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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// Everything this platform sends a client about a failed launch lives in this
// file: the reason it branches on, the display names it may place in its own
// wording, and the English fallback for a client that has no wording of its
// own. The fallbacks stay deliberately generic, because the advice that names
// an app, a menu or a settings screen belongs to the client. The catalog's
// per-profile install hint is the only other source, and it is only a fallback
// for a launcher that is not installed.

const (
	msgNotInstalled         = "this launcher is not installed, or its entry point is missing"
	msgCoreMissing          = "this launcher's plugin for this system is not installed"
	msgWrongMedia           = "this launcher cannot play the selected media entry"
	msgOverridesUnsupported = "this launcher does not support the requested launch options"
	msgReceiptMismatch      = "the launch could not be confirmed: another component answered"
)

// retroArchRepairMessage is the install hint carried by a RetroArch definition.
// It is fallback prose only; a client uses the reason and the launcher and
// plugin names instead.
func retroArchRepairMessage(coreName string) string {
	return fmt.Sprintf("Install %s using RetroArch's Core Downloader, allow RetroArch storage access, "+
		"and quit RetroArch before launching a different game. "+
		"Custom core/config locations are not supported by this profile.", coreName)
}

// repairReason maps a host failure onto the client-facing reason vocabulary.
// Every failure maps onto a reason, so no repair error can leave here without
// one.
func repairReason(reason FailureReason) platforms.LaunchRepairReason {
	switch reason {
	case FailureHostUnavailable, FailureInvalidResponse:
		return platforms.LaunchRepairHostUnavailable
	case FailureNotInstalled:
		return platforms.LaunchRepairLauncherNotInstalled
	case FailureActivityUnavailable:
		return platforms.LaunchRepairLauncherComponentMissing
	case FailureStorageDenied:
		return platforms.LaunchRepairStoragePermissionRequired
	case FailureProviderUnsupported:
		return platforms.LaunchRepairStorageProviderUnsupported
	case FailureStorageUnmounted:
		return platforms.LaunchRepairStorageUnavailable
	case FailureStorageVersion:
		return platforms.LaunchRepairLauncherVersionUnsupported
	case FailureSourceUnavailable:
		return platforms.LaunchRepairMediaUnavailable
	case FailureForegroundRequired:
		return platforms.LaunchRepairHostForegroundRequired
	case FailureCancelled:
		return platforms.LaunchRepairCancelled
	case FailureOutcomeUnknown:
		return platforms.LaunchRepairOutcomeUnknown
	case FailureRefused:
		return platforms.LaunchRepairRefused
	default:
		// A host answering with a code this build does not know says nothing
		// about the operating system, so it cannot claim a refusal.
		return platforms.LaunchRepairUnspecified
	}
}

// repairMessage is the English fallback for a host failure. installHint is the
// definition's own install advice, used only when the launcher itself is absent.
func repairMessage(reason FailureReason, installHint string) string {
	//nolint:exhaustive // every reason without its own wording shares the default
	switch reason {
	case FailureHostUnavailable:
		return "the launcher service is not responding"
	case FailureInvalidResponse:
		return "the launcher service answered with something unusable"
	case FailureNotInstalled, FailureActivityUnavailable:
		if installHint == "" {
			return msgNotInstalled
		}
		return installHint
	case FailureStorageDenied:
		return "this launcher does not have the storage permission it needs"
	case FailureStorageVersion:
		return "this build of the launcher cannot be used for this media; a different build is needed"
	case FailureProviderUnsupported:
		return "this launcher cannot read media from the provider holding it"
	case FailureStorageUnmounted:
		return "the storage holding this media is not available"
	case FailureSourceUnavailable:
		return "this media entry could not be resolved or opened"
	case FailureForegroundRequired:
		return "return to Zaparoo before launching a game"
	case FailureCancelled:
		return "the launch was cancelled before it started"
	case FailureOutcomeUnknown:
		return "the launch was sent but its outcome could not be confirmed"
	case FailureRefused:
		return "Android refused the launcher request"
	default:
		return "the launch could not be completed"
	}
}

// repairParams are the display names a client may place in its own wording.
// They are names only: never a package, path, URI, option or host message.
func (e *catalogEntry) repairParams() map[string]string {
	params := map[string]string{platforms.LaunchRepairParamLauncher: e.group}
	if e.coreName != "" {
		params[platforms.LaunchRepairParamPlugin] = e.coreName
	}
	return params
}

// repairError marks one of this file's fixed messages as safe to show a client
// and carries the reason and names the client uses in place of the wording.
func repairError(reason platforms.LaunchRepairReason, params map[string]string, message string) error {
	//nolint:wrapcheck // the repair error is the whole message; a wrapper would only prefix it
	return platforms.NewLaunchRepairErrorWithReason(reason, params, message)
}

// hostRepairError turns a host failure into entry's client-facing repair error.
func hostRepairError(failure FailureReason, entry *catalogEntry) error {
	return repairError(repairReason(failure), entry.repairParams(),
		repairMessage(failure, entry.definition.Repair))
}
