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
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errGeneric = errors.New("generic reader")

func genericReaders() hoststatus.Readers {
	return hoststatus.Readers{
		Power:       func() (power.Detail, error) { return power.Detail{}, errGeneric },
		Network:     func() (hoststatus.Network, error) { return hoststatus.Network{}, errGeneric },
		Bluetooth:   func() (hoststatus.Bluetooth, error) { return hoststatus.Bluetooth{}, errGeneric },
		Storage:     func([]hoststatus.StorageRoot) ([]hoststatus.Volume, error) { return nil, errGeneric },
		Display:     func() (hoststatus.Display, error) { return hoststatus.Display{}, errGeneric },
		Controllers: func() ([]hoststatus.Controller, error) { return nil, errGeneric },
		System:      func() (hoststatus.System, error) { return hoststatus.System{}, errGeneric },
	}
}

// plainPlatform implements none of the device providers.
type plainPlatform struct {
	platforms.Platform
}

func TestResolveDeviceReaders_GenericWhenPlatformHasNone(t *testing.T) {
	t.Parallel()

	readers := platforms.ResolveDeviceReaders(plainPlatform{}, genericReaders())

	_, err := readers.Power()
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.Network()
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.Bluetooth()
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.Storage(nil)
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.Display()
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.Controllers()
	require.ErrorIs(t, err, errGeneric)
	_, err = readers.System()
	require.ErrorIs(t, err, errGeneric)

	require.NotNil(t, readers.PowerControl, "a device with no power control still answers")
	assert.Empty(t, readers.PowerControl.PowerActions(context.Background()))
}

func TestResolveDeviceReaders_PlatformProvidersWin(t *testing.T) {
	t.Parallel()

	readers := platforms.ResolveDeviceReaders(mocks.NewMockPlatform(), genericReaders())

	detail, err := readers.Power()
	require.NoError(t, err)
	assert.Equal(t, power.SourceExternal, detail.Source)

	network, err := readers.Network()
	require.NoError(t, err)
	assert.True(t, network.InternetAuthoritative)

	_, err = readers.Bluetooth()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = readers.Storage([]hoststatus.StorageRoot{{Path: "/ignored"}})
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = readers.Display()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = readers.Controllers()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = readers.System()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = readers.PowerControl.PreparePowerAction(context.Background(), hoststatus.PowerReboot)
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
}
