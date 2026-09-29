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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppPathRoundTripsIdentityAndKeepsIntentOut(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		identity AppIdentity
		want     string
	}{
		{
			AppIdentity{Package: "com.seleuco.mame4d2024", Name: "MAME4droid"},
			"android://com.seleuco.mame4d2024/MAME4droid",
		},
		{AppIdentity{Package: "com.armsx2", Name: "ARMSX2"}, "android://com.armsx2/ARMSX2"},
		{
			AppIdentity{Package: "com.theboisclub.pokemonred", Variant: "red", Name: "Pokemon Red"},
			"android://com.theboisclub.pokemonred:red/Pokemon%20Red",
		},
	} {
		path := tc.identity.AppPath()
		assert.Equal(t, tc.want, path)
		parsed, err := ParseAppPath(path)
		require.NoError(t, err, path)
		assert.Equal(t, tc.identity, parsed)
	}
}

// A package cannot contain the separator, so the first one always splits the
// variant off cleanly even when the variant itself carries one.
func TestParseAppPathSplitsOnTheFirstSeparator(t *testing.T) {
	t.Parallel()

	parsed, err := ParseAppPath("android://com.example.app:a-b/Name")
	require.NoError(t, err)
	assert.Equal(t, "com.example.app", parsed.Package)
	assert.Equal(t, "a-b", parsed.Variant)
}

func TestParseAppPathRejectsWhatIsNotAnAppIdentity(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"steam://12345/Some Game",
		"android://nodots/Name",
		"android://com.example.app:/Name",
		"android://com.example.app:UPPER/Name",
		"android://com.example.app:with space/Name",
		"android:///Name",
		"source://tree/nes/Game.nes",
		"",
	} {
		_, err := ParseAppPath(path)
		require.ErrorIs(t, err, ErrAppIdentity, path)
	}
}
