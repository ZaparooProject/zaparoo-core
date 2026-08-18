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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVersionLine_IsFrozen is a tripwire, not a restatement of the code. The
// self-update probe runs in the binary a device already has installed and
// compares what a freshly downloaded one prints against this text, so the older
// build is always the one judging the newer. Editing the format would make
// every device in the field refuse the release that changed it and every
// release after it, unrecoverably from the new release's side.
//
// If this test fails, the change is a compatibility break, not a typo fix.
func TestVersionLine_IsFrozen(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Zaparoo v2.11.0 (mister)", VersionLine("2.11.0", "mister"))
	assert.Equal(t, "Zaparoo v2.11.0-beta4 (linux)", VersionLine("2.11.0-beta4", "linux"))
}

// TestVersionFlagName_IsFrozen guards the other half of the same contract: the
// probe invokes a downloaded binary with this flag, so an installed build can
// only ask a future one for its version by the name it knows today.
func TestVersionFlagName_IsFrozen(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "version", VersionFlagName)
}
