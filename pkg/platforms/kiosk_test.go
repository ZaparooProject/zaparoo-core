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

package platforms_test

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostPlatform is a platform with a lock of its own.
type hostPlatform struct {
	*mocks.MockPlatform
	err      error
	unsealed []string
	locked   []bool
}

func (p *hostPlatform) KioskUnsealed() []string { return p.unsealed }

func (p *hostPlatform) SetKioskLocked(locked bool) error {
	if p.err != nil {
		return p.err
	}
	p.locked = append(p.locked, locked)
	return nil
}

func TestKioskUnsealed(t *testing.T) {
	t.Parallel()

	// A platform that takes no part in the lock is one whose own menus
	// Zaparoo cannot close.
	assert.Equal(t, []string{platforms.KioskUnsealedHostMenus}, platforms.KioskUnsealed(mocks.NewMockPlatform()))

	sealed := &hostPlatform{MockPlatform: mocks.NewMockPlatform()}
	reasons := platforms.KioskUnsealed(sealed)
	assert.NotNil(t, reasons, "a sealed host reports an empty list, not a missing one")
	assert.Empty(t, reasons)

	open := &hostPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		unsealed:     []string{platforms.KioskUnsealedMainControls},
	}
	assert.Equal(t, []string{platforms.KioskUnsealedMainControls}, platforms.KioskUnsealed(open))
}

func TestSetKioskLocked(t *testing.T) {
	t.Parallel()

	require.NoError(t, platforms.SetKioskLocked(mocks.NewMockPlatform(), true), "no host lock to set")

	host := &hostPlatform{MockPlatform: mocks.NewMockPlatform()}
	require.NoError(t, platforms.SetKioskLocked(host, true))
	require.NoError(t, platforms.SetKioskLocked(host, false))
	assert.Equal(t, []bool{true, false}, host.locked)

	refused := errors.New("host is between processes")
	failing := &hostPlatform{MockPlatform: mocks.NewMockPlatform(), err: refused}
	require.ErrorIs(t, platforms.SetKioskLocked(failing, true), refused)
}
