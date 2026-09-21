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

package helpers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectDetectedLauncher(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	launcher, result := SelectDetectedLauncher([]platforms.Launcher{{ID: "A"}, {ID: "B"}})
	require.Equal(t, LauncherDetectionUnscanned, result)
	assert.Empty(t, launcher.ID)

	launcher, result = SelectDetectedLauncher([]platforms.Launcher{
		{ID: "A", Detected: &no}, {ID: "B", Detected: &no},
	})
	require.Equal(t, LauncherDetectionNone, result)
	assert.Empty(t, launcher.ID)

	launcher, result = SelectDetectedLauncher([]platforms.Launcher{
		{ID: "A", Detected: &no}, {ID: "B", Detected: &yes},
	})
	require.Equal(t, LauncherDetectionUnique, result)
	assert.Equal(t, "B", launcher.ID)

	launcher, result = SelectDetectedLauncher([]platforms.Launcher{
		{ID: "A", Detected: &yes}, {ID: "B", Detected: &yes},
	})
	require.Equal(t, LauncherDetectionAmbiguous, result)
	assert.Empty(t, launcher.ID)
}

func TestLauncherKnownMissing(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	assert.False(t, LauncherKnownMissing(&platforms.Launcher{}), "unscanned is not evidence of absence")
	assert.False(t, LauncherKnownMissing(&platforms.Launcher{Detected: &yes}))
	assert.True(t, LauncherKnownMissing(&platforms.Launcher{Detected: &no}))
}
