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

package updater

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolloutEligible(t *testing.T) {
	t.Parallel()

	const id = "6f1c9a4e-4f1a-4a6e-9f1d-2b3c4d5e6f70"

	tests := []struct {
		name     string
		deviceID string
		rollout  int
		want     bool
	}{
		{name: "fully released", deviceID: id, rollout: 100, want: true},
		{name: "over 100 is still released", deviceID: id, rollout: 150, want: true},
		{name: "not started", deviceID: id, rollout: 0, want: false},
		{name: "negative is not started", deviceID: id, rollout: -5, want: false},
		{
			name:     "an unidentified device only takes a full release",
			deviceID: "", rollout: 99, want: false,
		},
		{
			name:     "an unidentified device still takes a full release",
			deviceID: "", rollout: 100, want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RolloutEligible(tt.deviceID, "v2.10.0", tt.rollout))
		})
	}
}

// A rollout being widened must keep the devices it already reached, or the
// first cohort would be swapped for a fresh untested one every time.
func TestRolloutWideningKeepsEarlierDevices(t *testing.T) {
	t.Parallel()

	const tag = "v2.10.0"
	for i := range 500 {
		id := fmt.Sprintf("device-%d", i)
		if !RolloutEligible(id, tag, 10) {
			continue
		}
		assert.True(t, RolloutEligible(id, tag, 25),
			"%s was in the 10%% cohort and must stay in the 25%% one", id)
	}
}

// The release is part of the hash so no device is permanently the one that
// gets every update first.
func TestRolloutBucketVariesByRelease(t *testing.T) {
	t.Parallel()

	const id = "6f1c9a4e-4f1a-4a6e-9f1d-2b3c4d5e6f70"
	first := rolloutBucket(id, "v2.10.0")
	second := rolloutBucket(id, "v2.11.0")
	assert.NotEqual(t, first, second)
}

func TestRolloutBucketIsStable(t *testing.T) {
	t.Parallel()

	const id = "6f1c9a4e-4f1a-4a6e-9f1d-2b3c4d5e6f70"
	bucket := rolloutBucket(id, "v2.10.0")
	assert.Equal(t, bucket, rolloutBucket(id, "v2.10.0"))
	assert.GreaterOrEqual(t, bucket, 0)
	assert.Less(t, bucket, 100)
}

// A percentage is only worth publishing if it means roughly that share of the
// fleet, so check the spread over a realistic number of devices.
func TestRolloutSpread(t *testing.T) {
	t.Parallel()

	const (
		devices = 5000
		tag     = "v2.10.0"
		rollout = 25
	)

	eligible := 0
	for i := range devices {
		if RolloutEligible(fmt.Sprintf("device-%d", i), tag, rollout) {
			eligible++
		}
	}

	share := float64(eligible) / float64(devices) * 100
	require.InDelta(t, float64(rollout), share, 3.0,
		"a %d%% rollout reached %.1f%% of devices", rollout, share)
}
