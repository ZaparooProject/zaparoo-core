//go:build !windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"fmt"
	"os"
)

func replaceStateFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("replacing state file %q with %q: %w", target, source, err)
	}
	return nil
}
