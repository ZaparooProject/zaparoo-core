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

package mediadb

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
)

func TestShouldCheckpointAfterCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err    error
		name   string
		status string
		mode   database.WALCheckpointMode
		want   bool
	}{
		{name: "auto running", mode: database.WALCheckpointAuto, status: IndexingStatusRunning, want: true},
		{name: "auto pending", mode: database.WALCheckpointAuto, status: IndexingStatusPending, want: true},
		{name: "auto completed", mode: database.WALCheckpointAuto, status: IndexingStatusCompleted, want: false},
		{name: "auto failed", mode: database.WALCheckpointAuto, status: IndexingStatusFailed, want: false},
		{
			name: "auto status error", mode: database.WALCheckpointAuto,
			status: "", err: errors.New("status failed"), want: true,
		},
		{name: "auto no rows", mode: database.WALCheckpointAuto, status: "", err: sql.ErrNoRows, want: false},
		{name: "skip running", mode: database.WALCheckpointSkip, status: IndexingStatusRunning, want: false},
		{name: "force completed", mode: database.WALCheckpointForce, status: IndexingStatusCompleted, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldCheckpointAfterCommit(tt.mode, tt.status, tt.err))
		})
	}
}
