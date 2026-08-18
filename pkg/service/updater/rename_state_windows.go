//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

func replaceStateFile(source, target string) error {
	return replaceFile(source, target)
}
