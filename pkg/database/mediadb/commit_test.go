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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
)

// TestShouldCheckpointAfterCommit covers the three modes the function actually
// branches on. Indexing status and its lookup error used to select the auto
// behaviour; checkpointLargeWAL's size-driven checkpoint replaced that, so auto
// no longer takes them and only Force asks for a checkpoint of its own.
func TestShouldCheckpointAfterCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode database.WALCheckpointMode
		want bool
	}{
		{name: "auto defers to checkpointLargeWAL", mode: database.WALCheckpointAuto, want: false},
		{name: "skip never checkpoints", mode: database.WALCheckpointSkip, want: false},
		{name: "force always checkpoints", mode: database.WALCheckpointForce, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldCheckpointAfterCommit(tt.mode))
		})
	}
}
