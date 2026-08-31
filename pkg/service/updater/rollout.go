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
	"crypto/sha256"
	"encoding/binary"
)

// rolloutBucket places a device in one of a hundred buckets for a release.
//
// The release tag is part of the hash so that widening a rollout from 10% to
// 25% keeps everyone who already has it, while a different release draws an
// unrelated set. Without that, the same unlucky devices would be first every
// single time.
func rolloutBucket(deviceID, releaseTag string) int {
	sum := sha256.Sum256([]byte(deviceID + "\x00" + releaseTag))
	return int(binary.BigEndian.Uint32(sum[:4]) % 100)
}

// RolloutEligible reports whether this device is inside a release's staged
// rollout yet.
//
// A device with no ID only takes releases that have gone out to everyone: an
// unidentified device has no stable bucket, and treating it as bucket 0 would
// quietly make every such device part of the first wave.
func RolloutEligible(deviceID, releaseTag string, rollout int) bool {
	if rollout >= 100 {
		return true
	}
	if rollout <= 0 || deviceID == "" {
		return false
	}
	return rolloutBucket(deviceID, releaseTag) < rollout
}
