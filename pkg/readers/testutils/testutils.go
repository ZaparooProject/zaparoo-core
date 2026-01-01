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

// Package testutils provides common testing utilities for reader tests.
package testutils

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/stretchr/testify/require"
)

// CreateTestScanChannel creates a buffered channel for reader scans with capacity of 10.
func CreateTestScanChannel(_ *testing.T) chan readers.Scan {
	return make(chan readers.Scan, 10)
}

// AssertScanReceived waits for a scan to be received on the channel within the timeout.
// Returns the received scan. Fails the test if no scan is received within timeout.
func AssertScanReceived(t *testing.T, ch chan readers.Scan, timeout time.Duration) readers.Scan {
	t.Helper()
	select {
	case scan := <-ch:
		return scan
	case <-time.After(timeout):
		require.Fail(t, "expected scan to be received within timeout", "timeout: %v", timeout)
		return readers.Scan{}
	}
}

// AssertNoScan verifies that no scan is received on the channel within the timeout.
// Fails the test if a scan is received.
func AssertNoScan(t *testing.T, ch chan readers.Scan, timeout time.Duration) {
	t.Helper()
	select {
	case scan := <-ch:
		require.Fail(t, "unexpected scan received",
			"scan: source=%s, token=%v, readerError=%v",
			scan.Source, scan.Token, scan.ReaderError)
	case <-time.After(timeout):
		// Expected - no scan received
	}
}

// CreateTempDevicePath creates a temporary file to represent a device path for testing.
// On Windows systems, it returns a COM port path. On Unix systems, it creates a temporary
// file and registers cleanup with t.Cleanup().
func CreateTempDevicePath(t *testing.T) string {
	t.Helper()

	// On Windows, the path check is often skipped, so we can use any path
	if isWindows() {
		return "COM1"
	}

	// On Unix systems, create a temporary file
	f, err := createTempFile(t, "", "device-test-*")
	if err != nil {
		t.Fatalf("failed to create temp device path: %v", err)
	}

	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	// Clean up when test is done
	t.Cleanup(func() {
		_ = removeTempFile(path)
	})

	return path
}
