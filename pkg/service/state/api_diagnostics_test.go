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

package state

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestAPIDiagnosticActivityDoesNotWaitOrExposeMedia(t *testing.T) {
	t.Parallel()
	st := &State{activeMedia: &models.ActiveMedia{Name: "PRIVATE_GAME"}, mediaDBRecoveryActive: true}
	playing, recovery := st.APIDiagnosticActivity()
	assert.Equal(t, apidiag.Active, playing)
	assert.Equal(t, apidiag.Active, recovery)
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		st.mu.Lock()
		close(locked)
		<-release
		st.mu.Unlock()
		close(done)
	}()
	<-locked
	playing, recovery = st.APIDiagnosticActivity()
	close(release)
	<-done
	assert.Equal(t, apidiag.Unknown, playing)
	assert.Equal(t, apidiag.Unknown, recovery)
	var missing *State
	playing, recovery = missing.APIDiagnosticActivity()
	assert.Equal(t, apidiag.Unknown, playing)
	assert.Equal(t, apidiag.Unknown, recovery)
}
