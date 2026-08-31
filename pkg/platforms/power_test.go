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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/stretchr/testify/assert"
)

// plainPlatform implements nothing: the embedded interface is nil and the test
// only ever asks whether it provides a power reading, which it does not.
type plainPlatform struct {
	Platform
}

type poweredPlatform struct {
	Platform
	read   func() (power.Status, error)
	err    error
	status power.Status
}

func (p *poweredPlatform) PowerStatus() (power.Status, error) {
	if p.read != nil {
		return p.read()
	}
	return p.status, p.err
}

func TestPowerStatus_PlatformReadingWins(t *testing.T) {
	pl := &poweredPlatform{status: power.Status{Source: power.SourceBattery, Percent: 42}}
	status := PowerStatus(pl)

	assert.Equal(t, power.SourceBattery, status.Source)
	assert.Equal(t, 42, status.Percent)
}

func TestPowerStatus_PlatformErrorReadsAsUnknown(t *testing.T) {
	pl := &poweredPlatform{
		status: power.Status{Source: power.SourceBattery, Percent: 90},
		err:    errors.New("battery driver not responding"),
	}
	status := PowerStatus(pl)

	// A percentage that came back alongside an error is not trustworthy, and
	// treating it as a full battery is the mistake that costs a device.
	assert.Equal(t, power.SourceUnknown, status.Source)
	assert.Zero(t, status.Percent)
}

func TestPowerStatus_UnsetSourceReadsAsUnknown(t *testing.T) {
	status := PowerStatus(&poweredPlatform{})

	assert.Equal(t, power.SourceUnknown, status.Source)
}

func TestPowerStatus_FallsBackToTheOSReading(t *testing.T) {
	called := false
	status := resolvePowerStatus(&plainPlatform{}, func() (power.Status, error) {
		called = true
		return power.Status{Source: power.SourceExternal}, nil
	}, time.Second)

	assert.True(t, called)
	assert.Equal(t, power.SourceExternal, status.Source)
}

func TestPowerStatus_TimesOutEveryReaderPath(t *testing.T) {
	tests := []struct {
		name     string
		provider bool
	}{
		{name: "OS fallback"},
		{name: "platform provider", provider: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{})
			unblock := make(chan struct{})
			finished := make(chan struct{})
			read := func() (power.Status, error) {
				close(started)
				<-unblock
				close(finished)
				return power.Status{Source: power.SourceExternal}, nil
			}

			var pl Platform = &plainPlatform{}
			fallback := read
			fallbackCalled := false
			if tt.provider {
				pl = &poweredPlatform{read: read}
				fallback = func() (power.Status, error) {
					fallbackCalled = true
					return power.Status{Source: power.SourceExternal}, nil
				}
			}

			status := resolvePowerStatus(pl, fallback, 20*time.Millisecond)
			assert.Equal(t, power.SourceUnknown, status.Source)
			assert.False(t, fallbackCalled)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("power reader did not start")
			}
			close(unblock)
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("power reader did not finish after release")
			}
		})
	}
}
