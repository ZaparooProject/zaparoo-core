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

package helpers

import (
	"math"
	"os"
	"runtime/debug"

	"github.com/mackerelio/go-osstat/memory"
	"github.com/rs/zerolog/log"
)

// ConfigureMemoryLimit sets GOMEMLIMIT based on available system memory to
// keep idle RSS low on memory-constrained devices like MiSTer (492MB total).
// The soft limit makes the GC more aggressive about returning memory to the
// OS after transient spikes (e.g., media indexing). It does NOT hard-cap
// memory — the runtime will exceed the limit if the live heap requires it.
//
// Skipped when the GOMEMLIMIT environment variable is already set, so users
// can override with their own value.
func ConfigureMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}

	limit := CalculateMemoryLimit()
	if limit <= 0 {
		return
	}

	prev := debug.SetMemoryLimit(limit)
	log.Info().
		Int64("limit_bytes", limit).
		Int64("limit_mb", limit/(1<<20)).
		Int64("prev_limit", prev).
		Msg("set GOMEMLIMIT based on system memory")
}

// CalculateMemoryLimit returns a GOMEMLIMIT value based on system memory:
// 10% of total RAM, clamped to [32MB, 128MB]. Returns -1 if system memory
// cannot be read.
func CalculateMemoryLimit() int64 {
	mem, err := memory.Get()
	if err != nil {
		log.Warn().Err(err).Msg("failed to read system memory for GOMEMLIMIT")
		return -1
	}

	totalMB := mem.Total / (1 << 20)
	const minLimitMB, maxLimitMB uint64 = 32, 128
	limitMB := max(minLimitMB, min(maxLimitMB, totalMB/10))

	return int64(limitMB) * (1 << 20) //nolint:gosec // clamped to [32,128], no overflow
}

// SuspendMemoryLimit disables GOMEMLIMIT for memory-intensive operations
// like media indexing where speed matters more than RSS.
func SuspendMemoryLimit() {
	debug.SetMemoryLimit(math.MaxInt64)
}

// RestoreMemoryLimit re-applies the calculated GOMEMLIMIT after a
// memory-intensive operation completes.
func RestoreMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}

	limit := CalculateMemoryLimit()
	if limit <= 0 {
		return
	}

	debug.SetMemoryLimit(limit)
}
