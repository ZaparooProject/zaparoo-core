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

package config

import (
	"fmt"
	"strings"
	"time"
)

var AppVersion = "DEVELOPMENT"

// IsDevelopmentVersion returns true if AppVersion indicates a non-release build.
// This includes the literal "DEVELOPMENT" default and "<hash>-dev" builds.
func IsDevelopmentVersion() bool {
	return AppVersion == "DEVELOPMENT" || strings.HasSuffix(AppVersion, "-dev")
}

const (
	AppName              = "zaparoo"
	MediaDbFile          = "media.db"
	UserDbFile           = "user.db"
	LogFile              = "core.log"
	PidFile              = "core.pid"
	CfgFile              = "config.toml"
	AuthFile             = "auth.toml"
	TUIFile              = "tui.toml"
	UserDir              = "user"
	LogsDir              = "logs"
	APIRequestTimeout    = 30 * time.Second
	SuccessSoundFilename = "success.ogg"
	FailSoundFilename    = "fail.ogg"
	LimitSoundFilename   = "limit.ogg"
	PendingSoundFilename = "pending.ogg"
	ReadySoundFilename   = "ready.ogg"
	AssetsDir            = "assets"
	MappingsDir          = "mappings"
	LaunchersDir         = "launchers"
	MediaDir             = "media"
	CacheDir             = "cache"
	LogUploadURL         = "https://logs.zaparoo.org/"
	MinFreeDiskBytes     = 500 * 1024 * 1024 // 500 MB

	// VersionFlagName is the flag that prints VersionLine and exits. The
	// self-update probe passes it to a binary it has just downloaded, so the
	// name is part of the same frozen contract as the line itself.
	VersionFlagName = "version"
)

// VersionLine is the line the version flag prints, and the line the self-update
// probe looks for in a staged binary's output.
//
// It is a compatibility surface between releases, not a cosmetic string. The
// probe runs in the binary that is already installed and checks what the
// incoming one prints, so it is always the *older* build that decides whether a
// newer release is acceptable. Changing this text would make every device
// already in the field reject the release that changed it, and every release
// after that, with no way to fix it from the new release's side. Both the
// producer and the probe read it from here so they cannot drift, and the probe
// matches this as one line of output rather than the whole stream so that
// adding another line elsewhere stays harmless.
func VersionLine(version, platformID string) string {
	return fmt.Sprintf("Zaparoo v%s (%s)", version, platformID)
}
