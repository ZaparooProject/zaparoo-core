// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package middleware

import (
	"testing"
)

const RejectAfterMessages = 1<<64 - 1<<13 - 1

func TestReplayFilter(t *testing.T) {
	t.Parallel()
	var filter Filter
	const tLim = windowSize + 1
	testNumber := 0

	testFunc := func(n uint64, expected bool) {
		testNumber++
		if filter.ValidateCounter(n, RejectAfterMessages) != expected {
			t.Fatal("Test", testNumber, "failed", n, expected)
		}
	}

	filter.Reset()

	testFunc(0, true)
	testFunc(1, true)
	testFunc(1, false)
	testFunc(9, true)
	testFunc(8, true)
	testFunc(7, true)
	testFunc(7, false)
	testFunc(tLim, true)
	testFunc(tLim-1, true)
	testFunc(tLim-1, false)
	testFunc(tLim-2, true)
	testFunc(2, true)
	testFunc(2, false)
	testFunc(tLim+16, true)
	testFunc(3, false)
	testFunc(tLim+16, false)
	testFunc(tLim*4, true)
	testFunc(tLim*4-(tLim-1), true)
	testFunc(10, false)
	testFunc(tLim*4-tLim, false)
	testFunc(tLim*4-(tLim+1), false)
	testFunc(tLim*4-(tLim-2), true)
	testFunc(tLim*4+1-tLim, false)
	testFunc(0, false)
	testFunc(RejectAfterMessages, false)
	testFunc(RejectAfterMessages-1, true)
	testFunc(RejectAfterMessages, false)
	testFunc(RejectAfterMessages-1, false)
	testFunc(RejectAfterMessages-2, true)
	testFunc(RejectAfterMessages+1, false)
	testFunc(RejectAfterMessages+2, false)
	testFunc(RejectAfterMessages-2, false)
	testFunc(RejectAfterMessages-3, true)
	testFunc(0, false)

	t.Log("Bulk test 1")
	filter.Reset()
	testNumber = 0
	for i := uint64(1); i <= windowSize; i++ {
		testFunc(i, true)
	}
	testFunc(0, true)
	testFunc(0, false)

	t.Log("Bulk test 2")
	filter.Reset()
	testNumber = 0
	for i := uint64(2); i <= windowSize+1; i++ {
		testFunc(i, true)
	}
	testFunc(1, true)
	testFunc(0, false)

	t.Log("Bulk test 3")
	filter.Reset()
	testNumber = 0
	for i := uint64(windowSize + 1); i > 0; i-- {
		testFunc(i, true)
	}

	t.Log("Bulk test 4")
	filter.Reset()
	testNumber = 0
	for i := uint64(windowSize + 2); i > 1; i-- {
		testFunc(i, true)
	}
	testFunc(0, false)

	t.Log("Bulk test 5")
	filter.Reset()
	testNumber = 0
	for i := uint64(windowSize); i > 0; i-- {
		testFunc(i, true)
	}
	testFunc(windowSize+1, true)
	testFunc(0, false)

	t.Log("Bulk test 6")
	filter.Reset()
	testNumber = 0
	for i := uint64(windowSize); i > 0; i-- {
		testFunc(i, true)
	}
	testFunc(0, true)
	testFunc(windowSize+1, true)
}
