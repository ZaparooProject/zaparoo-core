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
	err    error
	status power.Status
}

func (p *poweredPlatform) PowerStatus() (power.Status, error) {
	return p.status, p.err
}

func TestPowerStatus_PlatformReadingWins(t *testing.T) {
	t.Parallel()

	pl := &poweredPlatform{status: power.Status{Source: power.SourceBattery, Percent: 42}}
	status := PowerStatus(pl)

	assert.Equal(t, power.SourceBattery, status.Source)
	assert.Equal(t, 42, status.Percent)
}

func TestPowerStatus_PlatformErrorReadsAsUnknown(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	status := PowerStatus(&poweredPlatform{})

	assert.Equal(t, power.SourceUnknown, status.Source)
}

func TestPowerStatus_FallsBackToTheOSReading(t *testing.T) {
	t.Parallel()

	// What the machine running this reports is its own business; what matters
	// is that a platform with no reading of its own still gets an answer the
	// gate can act on rather than an empty one.
	status := PowerStatus(&plainPlatform{})

	assert.NotEmpty(t, status.Source)
}
