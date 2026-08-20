//go:build !windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"os"
	"time"
)

// defaultSwapOps replaces a running binary the way every platform but Windows
// allows: one rename over the old name. Nothing here is worth retrying, because
// rename does not fail for reasons that pass.
func defaultSwapOps() swapOps {
	return swapOps{
		replace:   replaceFile,
		remove:    os.Remove,
		exists:    fileExists,
		transient: func(error) bool { return false },
		sleep:     time.Sleep,
		vacate:    false,
	}
}
