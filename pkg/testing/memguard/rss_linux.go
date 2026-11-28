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

package memguard

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const supported = true

func residentBytes() (uint64, error) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, fmt.Errorf("reading statm: %w", err)
	}
	return parseStatm(string(data), uint64(os.Getpagesize())) //nolint:gosec // the page size is a small positive int
}

// parseStatm reads the resident page count, the second field of
// /proc/self/statm, and converts it to bytes.
func parseStatm(statm string, pageSize uint64) (uint64, error) {
	fields := strings.Fields(statm)
	if len(fields) < 2 {
		return 0, errors.New("statm has fewer than two fields")
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing resident pages: %w", err)
	}
	return pages * pageSize, nil
}
