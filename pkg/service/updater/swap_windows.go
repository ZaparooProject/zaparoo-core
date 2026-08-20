//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// defaultSwapOps replaces a running binary by moving the outgoing one aside
// first. Windows will not let a mapped image be overwritten, but it does let one
// be renamed, so the swap is two renames in the same directory instead of one.
func defaultSwapOps() swapOps {
	return swapOps{
		replace:   replaceFile,
		remove:    os.Remove,
		exists:    fileExists,
		transient: transientSwapError,
		sleep:     time.Sleep,
		vacate:    true,
		conceal:   hideFile,
	}
}

// hideFile marks a file hidden. The binary an update superseded cannot be
// deleted until the process running from it exits, so it sits in the install
// directory until a later sweep clears it: the boot that confirms the update,
// or, where the swap moved this process's own image aside to roll back, the
// next install. Hiding it keeps a user from finding a second executable beside
// the one they launch.
func hideFile(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encoding the path to hide: %w", err)
	}
	// SetFileAttributes writes the whole set rather than adding to it, so the
	// existing one has to be read first. Sending the hidden bit on its own
	// would clear everything else the file carries.
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return fmt.Errorf("reading the attributes of %q: %w", path, err)
	}
	if err := windows.SetFileAttributes(name, attrs|windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		return fmt.Errorf("hiding %q: %w", path, err)
	}
	return nil
}

// transientSwapError reports whether a rename failed for a reason that passes.
//
// A binary that was written moments ago is exactly what a virus scanner opens,
// and an indexer or a backup agent can hold one at any time. Both surface as a
// sharing or lock violation and both let go on their own. Access denied is
// included because a scanner holding a handle without sharing delete reports it
// that way too. A permission problem that will never pass is reported that way
// as well and is waited out for nothing, which is why the move that leaves the
// target name empty is the one with the short budget.
func transientSwapError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
