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

package shared

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

// Custom URI scheme constants for Zaparoo virtual paths.
// These schemes are used to create virtual paths for media that doesn't have
// traditional file paths (e.g., Steam games, Kodi media, LaunchBox collections, etc.).
const (
	SchemeSteam      = "steam"
	SchemeFlashpoint = "flashpoint"
	SchemeLaunchBox  = "launchbox"
	SchemeScummVM    = "scummvm"
	SchemeLutris     = "lutris"
	SchemeHeroic     = "heroic"
	SchemeBottles    = "bottles"
	SchemeFaugus     = "faugus"
	SchemeGOG        = "gog"
)

// Kodi URI scheme constants for Kodi media library items.
const (
	SchemeKodiMovie   = "kodi-movie"
	SchemeKodiEpisode = "kodi-episode"
	SchemeKodiSong    = "kodi-song"
	SchemeKodiAlbum   = "kodi-album"
	SchemeKodiArtist  = "kodi-artist"
	SchemeKodiShow    = "kodi-show"
)

// Standard URI schemes that should have URL decoding applied.
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// customSchemes is the central registry of all Zaparoo custom URI schemes
var customSchemes = []string{
	SchemeSteam,
	SchemeFlashpoint,
	SchemeLaunchBox,
	SchemeScummVM,
	SchemeLutris,
	SchemeHeroic,
	SchemeBottles,
	SchemeFaugus,
	SchemeGOG,
	SchemeKodiMovie,
	SchemeKodiEpisode,
	SchemeKodiSong,
	SchemeKodiAlbum,
	SchemeKodiArtist,
	SchemeKodiShow,
}

// standardSchemesForDecoding lists standard URI schemes that should have URL decoding applied
var standardSchemesForDecoding = []string{
	SchemeHTTP,
	SchemeHTTPS,
}

// ValidCustomSchemes returns a slice of all registered Zaparoo custom URI schemes
func ValidCustomSchemes() []string {
	return customSchemes
}

// StandardSchemesForDecoding returns a slice of standard URI schemes that should have URL decoding
func StandardSchemesForDecoding() []string {
	return standardSchemesForDecoding
}

// IsCustomScheme checks if the given scheme is a registered Zaparoo custom URI scheme
func IsCustomScheme(scheme string) bool {
	scheme = strings.ToLower(scheme)
	for _, s := range customSchemes {
		if s == scheme {
			return true
		}
	}
	return false
}

// IsStandardSchemeForDecoding checks if the given scheme is a standard URI scheme that should have URL decoding
func IsStandardSchemeForDecoding(scheme string) bool {
	scheme = strings.ToLower(scheme)
	for _, s := range standardSchemesForDecoding {
		if s == scheme {
			return true
		}
	}
	return false
}

// ShouldDecodeURIScheme checks if the given scheme should have URL decoding applied
// Returns true for Zaparoo custom schemes and standard web schemes (http/https)
func ShouldDecodeURIScheme(scheme string) bool {
	return IsCustomScheme(scheme) || IsStandardSchemeForDecoding(scheme)
}

// SchemeIDTest builds a Launcher.Test that accepts only paths carrying a
// usable ID for this scheme.
//
// A launcher that declares Schemes and no Test is selected for anything with
// the right prefix, and DoLaunch stops the running media as soon as a launcher
// is chosen. Without this, a malformed path takes down whatever is playing and
// only then fails inside Launch. Every scheme launcher whose Launch parses an
// ID out of the path wants this; those that hand the whole path to an external
// opener (the browser launchers) reject nothing and so need nothing.
func SchemeIDTest(scheme string) func(*config.Instance, string) bool {
	return func(_ *config.Instance, path string) bool {
		id, err := virtualpath.ExtractSchemeID(path, scheme)
		return err == nil && id != ""
	}
}
