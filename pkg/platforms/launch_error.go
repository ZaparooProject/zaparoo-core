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

package platforms

import (
	"maps"
	"slices"
	"strings"
)

// LaunchRepairReason is the closed set of reasons a launch needs the user to
// fix something before it can succeed. A client branches on the reason and
// writes its own wording and localization; the error's message is only the
// fallback for a client that has none.
//
// The set is closed. Adding a value means mapping every producer onto it and
// documenting it in docs/api/methods.md in the same change.
type LaunchRepairReason string

const (
	// LaunchRepairLauncherNotInstalled means the launcher application is not installed.
	LaunchRepairLauncherNotInstalled LaunchRepairReason = "launcher_not_installed"
	// LaunchRepairLauncherComponentMissing means the launcher is installed but the
	// entry point it declares is gone or disabled.
	LaunchRepairLauncherComponentMissing LaunchRepairReason = "launcher_component_missing"
	// LaunchRepairLauncherPluginMissing means the launcher is installed but its
	// plugin or core for this system is absent.
	LaunchRepairLauncherPluginMissing LaunchRepairReason = "launcher_plugin_missing"
	// LaunchRepairLauncherVersionUnsupported means the installed build of the
	// launcher cannot be used for this media, for example because its storage
	// model is unsupported. The user needs a different build of that launcher.
	LaunchRepairLauncherVersionUnsupported LaunchRepairReason = "launcher_version_unsupported"
	// LaunchRepairLauncherAmbiguous means several launchers are usable and no
	// reviewed default applies, so the user must choose one. It is reserved: no
	// producer emits it yet, because launcher selection resolves by catalog
	// precedence without asking.
	LaunchRepairLauncherAmbiguous LaunchRepairReason = "launcher_ambiguous"
	// LaunchRepairLauncherUnsupportedMedia means this launcher cannot play the
	// selected media entry.
	LaunchRepairLauncherUnsupportedMedia LaunchRepairReason = "launcher_unsupported_media"
	// LaunchRepairLauncherOptionsUnsupported means the launch options requested are
	// not supported by this launcher.
	LaunchRepairLauncherOptionsUnsupported LaunchRepairReason = "launcher_options_unsupported"
	// LaunchRepairStoragePermissionRequired means the launcher lacks the storage
	// permission it needs.
	LaunchRepairStoragePermissionRequired LaunchRepairReason = "storage_permission_required"
	// LaunchRepairStorageProviderUnsupported means the media lives on a provider
	// this launcher cannot read.
	LaunchRepairStorageProviderUnsupported LaunchRepairReason = "storage_provider_unsupported"
	// LaunchRepairStorageUnavailable means the storage holding the media is not present.
	LaunchRepairStorageUnavailable LaunchRepairReason = "storage_unavailable"
	// LaunchRepairMediaUnavailable means the media file cannot be resolved or opened.
	LaunchRepairMediaUnavailable LaunchRepairReason = "media_unavailable"
	// LaunchRepairHostUnavailable means the host's launch service is not answering.
	LaunchRepairHostUnavailable LaunchRepairReason = "host_unavailable"
	// LaunchRepairHostForegroundRequired means the launch needs the user to return
	// to the app first.
	LaunchRepairHostForegroundRequired LaunchRepairReason = "host_foreground_required"
	// LaunchRepairCancelled means the launch was cancelled before it started.
	LaunchRepairCancelled LaunchRepairReason = "cancelled"
	// LaunchRepairOutcomeUnknown means the launch was dispatched but its result
	// could not be confirmed.
	LaunchRepairOutcomeUnknown LaunchRepairReason = "outcome_unknown"
	// LaunchRepairRefused means the operating system refused the request. It is
	// not a catch-all: a producer that cannot say that much reports
	// LaunchRepairUnspecified instead.
	LaunchRepairRefused LaunchRepairReason = "refused"
	// LaunchRepairUnspecified means the producer sent no structured reason, so a
	// client shows the error's message verbatim. It is the default for a producer
	// with no code yet and for any reason this build does not recognise.
	LaunchRepairUnspecified LaunchRepairReason = "unspecified"
)

// Parameter names a repair error may carry. The key set is closed: these are
// display names a client substitutes into its own wording, never identifiers,
// paths or host text.
const (
	// LaunchRepairParamLauncher names the launcher application, such as "RetroArch".
	LaunchRepairParamLauncher = "launcher"
	// LaunchRepairParamPlugin names the launcher's plugin or core, such as "Mesen".
	LaunchRepairParamPlugin = "plugin"
)

const (
	maxRepairMessageBytes = 1024
	maxRepairParamBytes   = 64
	// repairParamPunctuation is every non-alphanumeric character a display name
	// may contain. It admits no path, URI, script, query or assignment syntax.
	repairParamPunctuation = " !()+-._"
	defaultRepairMessage   = "player request could not be completed"
)

// launchRepairReasons is the closed set, in documentation order.
var launchRepairReasons = []LaunchRepairReason{
	LaunchRepairLauncherNotInstalled,
	LaunchRepairLauncherComponentMissing,
	LaunchRepairLauncherPluginMissing,
	LaunchRepairLauncherVersionUnsupported,
	LaunchRepairLauncherAmbiguous,
	LaunchRepairLauncherUnsupportedMedia,
	LaunchRepairLauncherOptionsUnsupported,
	LaunchRepairStoragePermissionRequired,
	LaunchRepairStorageProviderUnsupported,
	LaunchRepairStorageUnavailable,
	LaunchRepairMediaUnavailable,
	LaunchRepairHostUnavailable,
	LaunchRepairHostForegroundRequired,
	LaunchRepairCancelled,
	LaunchRepairOutcomeUnknown,
	LaunchRepairRefused,
	LaunchRepairUnspecified,
}

// launchRepairParams is the closed parameter key set, in documentation order.
var launchRepairParams = []string{LaunchRepairParamLauncher, LaunchRepairParamPlugin}

// LaunchRepairReasons returns every reason in the closed set.
func LaunchRepairReasons() []LaunchRepairReason { return slices.Clone(launchRepairReasons) }

// LaunchRepairParams returns every parameter name in the closed key set.
func LaunchRepairParams() []string { return slices.Clone(launchRepairParams) }

// Valid reports whether r belongs to the closed set.
func (r LaunchRepairReason) Valid() bool { return slices.Contains(launchRepairReasons, r) }

// LaunchRepairError explicitly marks a launch failure as one a client may show.
// The reason and parameters are the contract; the message is a fixed English
// fallback kept for clients that only read error.message. Wrapping error details
// remain private to Core logs.
type LaunchRepairError struct {
	params  map[string]string
	message string
	reason  LaunchRepairReason
}

func (e *LaunchRepairError) Error() string {
	if e == nil || e.message == "" {
		return defaultRepairMessage
	}
	return e.message
}

// Reason is never empty: an error built without a usable one reports
// LaunchRepairUnspecified, which tells a client to show the message as it is.
func (e *LaunchRepairError) Reason() LaunchRepairReason {
	if e == nil || !e.reason.Valid() {
		return LaunchRepairUnspecified
	}
	return e.reason
}

// Params returns a copy of the bounded display names, or nil when there are
// none, so a client-facing payload cannot be edited through the error.
func (e *LaunchRepairError) Params() map[string]string {
	if e == nil || len(e.params) == 0 {
		return nil
	}
	return maps.Clone(e.params)
}

// NewLaunchRepairError must never receive paths, scripts, credentials, provider
// errors or other runtime input. Use fixed messages selected by a bounded code.
// It reports LaunchRepairUnspecified, for producers that have no code yet.
func NewLaunchRepairError(message string) error {
	return NewLaunchRepairErrorWithReason(LaunchRepairUnspecified, nil, message)
}

// NewLaunchRepairErrorWithReason builds the client-facing repair error. A reason
// outside the closed set becomes LaunchRepairUnspecified, an unusable message
// becomes the generic fallback, and a parameter outside the closed key set or
// not shaped like a display name is dropped rather than sent.
func NewLaunchRepairErrorWithReason(
	reason LaunchRepairReason,
	params map[string]string,
	message string,
) error {
	if !reason.Valid() {
		reason = LaunchRepairUnspecified
	}
	return &LaunchRepairError{
		params:  safeRepairParams(params),
		message: safeRepairMessage(message),
		reason:  reason,
	}
}

func safeRepairMessage(message string) string {
	if message == "" || len(message) > maxRepairMessageBytes ||
		strings.ContainsAny(message, "\x00\r\n") {
		return defaultRepairMessage
	}
	return message
}

// safeRepairParams keeps only the closed key set, and only values that still
// read as a display name. Anything longer, differently punctuated or otherwise
// unrecognised is dropped, so a filesystem path, URI, script fragment,
// credential or provider error message cannot reach a client through here.
func safeRepairParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	safe := make(map[string]string, len(launchRepairParams))
	for _, key := range launchRepairParams {
		if value, present := params[key]; present && validRepairParam(value) {
			safe[key] = value
		}
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}

func validRepairParam(value string) bool {
	if value == "" || len(value) > maxRepairParamBytes ||
		strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case strings.ContainsRune(repairParamPunctuation, char):
		default:
			return false
		}
	}
	return true
}
