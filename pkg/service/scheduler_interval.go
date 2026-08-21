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

import "time"

// intervalState paces recurring service work without making correctness depend
// on persisted wall-clock time. Values captured by time.Now retain monotonic
// readings for the life of the process.
type intervalState struct {
	lastSuccess time.Time
	nextAttempt time.Time
	backoff     time.Duration
	idle        bool
}

func (s *intervalState) due(now time.Time, interval time.Duration) bool {
	if !s.lastSuccess.IsZero() && now.Sub(s.lastSuccess) < interval {
		return false
	}
	return s.nextAttempt.IsZero() || !now.Before(s.nextAttempt)
}

func (s *intervalState) recordFailure(
	now time.Time,
	initialBackoff time.Duration,
	maxBackoff time.Duration,
) {
	s.idle = false
	if s.backoff <= 0 {
		s.backoff = initialBackoff
	}
	s.nextAttempt = now.Add(s.backoff)
	s.backoff = min(s.backoff*2, maxBackoff)
}

func (s *intervalState) recordSuccess(now time.Time, initialBackoff time.Duration) {
	s.idle = false
	s.lastSuccess = now
	s.nextAttempt = time.Time{}
	s.backoff = initialBackoff
}
