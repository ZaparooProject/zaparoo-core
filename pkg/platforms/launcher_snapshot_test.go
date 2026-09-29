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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

type launcherCountingPlatform struct {
	Platform
	calls int
}

func (p *launcherCountingPlatform) Launchers(*config.Instance) []Launcher {
	p.calls++
	return []Launcher{{ID: "one"}}
}

func TestLauncherSnapshotAsksPlatformOnce(t *testing.T) {
	t.Parallel()
	pl := &launcherCountingPlatform{}
	snapshot := &LauncherSnapshot{}
	for range 3 {
		assert.Equal(t, "one", snapshot.Get(pl, nil)[0].ID)
	}
	assert.Equal(t, 1, pl.calls)

	var none *LauncherSnapshot
	none.Get(pl, nil)
	none.Get(pl, nil)
	assert.Equal(t, 3, pl.calls, "a nil snapshot asks every time")
}
