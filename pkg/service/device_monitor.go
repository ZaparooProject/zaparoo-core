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

package service

import (
	"context"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/device"
	"github.com/jonboulle/clockwork"
)

// newDeviceMonitor builds the monitor behind the device API. It reads nothing
// until it is run and a client connects.
func newDeviceMonitor(
	pl platforms.Platform,
	cfg *config.Instance,
	publish func(models.Notification),
) *device.Monitor {
	return device.NewMonitor(&device.Options{
		Platform: pl,
		Config:   cfg,
		Clock:    clockwork.NewRealClock(),
		Publish:  publish,
		Readers:  platforms.ResolveDeviceReaders(pl, device.DefaultReaders()),
		Prober:   hoststatus.NewProber(hoststatus.DefaultConnectivityURLs, nil),
	})
}

func startDeviceMonitor(ctx context.Context, monitor *device.Monitor, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor.Run(ctx)
	}()
}
