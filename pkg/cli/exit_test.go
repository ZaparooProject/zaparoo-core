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

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCodeFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, ExitCodeFor(nil))
	assert.Equal(t, 1, ExitCodeFor(errors.New("something broke")))

	// The error reaches main through service.Start and makeDatabase, each of
	// which wraps it, so the status has to survive the whole chain rather than
	// only matching the bare sentinel.
	wrapped := fmt.Errorf("error starting service: %w",
		fmt.Errorf("error migrating userdb: %w",
			fmt.Errorf("%w: database is at version 20260828000000 but this binary "+
				"only supports up to 20260818120000", database.ErrSchemaAhead)))
	assert.Equal(t, ExitUnrecoverable, ExitCodeFor(wrapped))
	assert.Equal(t, ExitUnrecoverable, ExitCodeFor(database.ErrSchemaAhead))
}

// The exit status is only useful if the units decline to restart on it, and
// nothing else connects the two: a change to one and not the other reinstates
// the restart loop silently.
func TestServiceUnitsDeclineToRestartOnTheUnrecoverableStatus(t *testing.T) {
	t.Parallel()

	units := []string{
		filepath.Join("..", "..", "cmd", "replayos", "conf", "zaparoo.service"),
		filepath.Join("..", "platforms", "linux", "installer", "conf", "zaparoo.service"),
	}
	want := fmt.Sprintf("RestartPreventExitStatus=%d", ExitUnrecoverable)

	for _, path := range units {
		data, err := os.ReadFile(path) //nolint:gosec // repository-relative test fixture
		require.NoError(t, err, "unit file should be readable")
		unit := string(data)
		require.Contains(t, unit, "Restart=on-failure",
			"%s: this test only matters while the unit restarts on failure", path)
		assert.Contains(t, unit, want,
			"%s: must not restart on the unrecoverable exit status", path)
		assert.NotContains(t, unit, "RestartPreventExitStatus=\n",
			"%s: empty RestartPreventExitStatus", path)
	}
}
