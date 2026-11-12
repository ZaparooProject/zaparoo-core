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

package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/device"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The device monitor reads hardware only while somebody is listening, and it
// learns that from the notification transports.
func TestNotificationClientPresenceDrivesDeviceMonitor(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.Anything).Return([]string{})

	var reads atomic.Int32
	read := make(chan struct{}, 8)
	monitor := device.NewMonitor(&device.Options{
		Platform: pl,
		Clock:    clockwork.NewFakeClockAt(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		Readers: hoststatus.Readers{
			Power: func() (power.Detail, error) {
				reads.Add(1)
				select {
				case read <- struct{}{}:
				default:
				}
				return power.Detail{Source: power.SourceExternal}, nil
			},
		},
	})

	st, _ := state.NewState(pl, "boot")
	t.Cleanup(st.StopService)
	st.SetDeviceMonitor(monitor)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	assert.Zero(t, reads.Load(), "nothing is read before a client connects")

	notificationClientConnected(st)
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("connecting a client did not start the monitor reading")
	}
	notificationClientDisconnected(st)
}

func TestNotificationClientPresenceWithoutMonitor(t *testing.T) {
	t.Parallel()

	st, _ := state.NewState(mocks.NewMockPlatform(), "boot")
	t.Cleanup(st.StopService)
	require.Nil(t, st.DeviceMonitor())

	notificationClientConnected(st)
	notificationClientDisconnected(st)
}
