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

// Every user-facing repair message this platform produces lives in this file.
// The standalone catalog's per-app install hints are the only other source.

const (
	msgCoreMissing = "this RetroArch core was not found in the selected cores folder; " +
		"install it or detect cores again"
	msgWrongMedia           = "this launcher does not support the selected canonical media entry"
	msgOverridesUnsupported = "launcher core and rendering overrides are not supported by this profile"
	msgReceiptMismatch      = "host player result does not match selected definition"
)

// retroArchRepairMessage is the install hint carried by a RetroArch definition.
func retroArchRepairMessage(coreName string) string {
	return fmt.Sprintf("Install %s using RetroArch's Core Downloader, allow RetroArch storage access, "+
		"and quit RetroArch before launching a different game. "+
		"Custom core/config locations are not supported by this profile.", coreName)
}

// repairMessage turns a host failure into what the user can do about it.
// installHint is the definition's own repair text.
func repairMessage(reason FailureReason, installHint string) string {
	//nolint:exhaustive // every reason without its own advice shares the default
	switch reason {
	case FailureHostUnavailable:
		return "Android launcher service unavailable; return to Zaparoo and retry"
	case FailureInvalidResponse:
		return "invalid Android launcher response"
	case FailureNotInstalled, FailureActivityUnavailable:
		return installHint
	case FailureStorageDenied:
		return "allow RetroArch storage access in Android Settings > Apps > RetroArch > Permissions"
	case FailureStorageVersion:
		return "this profile cannot verify storage access for this RetroArch build; " +
			"only legacy target SDK 28 or earlier is supported"
	case FailureProviderUnsupported:
		return "RetroArch needs a local file; choose a game folder from internal storage or an SD card, " +
			"not a cloud or other document provider"
	case FailureStorageUnmounted:
		return "game storage is unavailable; reconnect the SD card or storage and retry"
	case FailureSourceUnavailable:
		return "game file is unavailable or outside supported shared storage; " +
			"choose its folder again and update the media database"
	case FailureForegroundRequired, FailureCancelled:
		return "return to Zaparoo before launching a game"
	case FailureOutcomeUnknown:
		return "launch outcome is unknown; check RetroArch before retrying"
	default:
		return "Android refused the launcher request; check RetroArch and return to Zaparoo"
	}
}

// repairError marks one of this file's fixed messages as safe to show a client.
func repairError(message string) error {
	//nolint:wrapcheck // the repair error is the whole message; a wrapper would only prefix it
	return platforms.NewLaunchRepairError(message)
}
