//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func replaceFile(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encoding replacement source path: %w", err)
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encoding replacement target path: %w", err)
	}
	if err := windows.MoveFileEx(from, to,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("replacing %q with %q: %w", target, source, err)
	}
	return nil
}
