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

package updater

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// installSupersededSuffix names the outgoing binary after it has been moved
	// out of its own name to let the incoming one take it. It is a sibling of
	// the target because the move has to stay on one volume to be a rename, and
	// it is a third name rather than the rollback backup so that the copy
	// rollback depends on is never the file being juggled.
	installSupersededSuffix = ".zaparoo-update-old"

	// supersededSlots is how many of those names one target may have. One is
	// not enough: after a swap the first holds the image this process is
	// running from, and an install that then fails has to move the binary it
	// swapped in aside to put the old one back. Renaming onto a mapped image is
	// exactly what vacating exists to avoid, so the unwind takes a name the
	// running image is not sitting on.
	supersededSlots = 8

	// swapAttempts and swapDelay wait out a file another process is briefly
	// holding. On Windows a virus scanner opening a binary the moment it is
	// written is common enough that a single attempt would fail updates for
	// reasons that have nothing to do with the release. Five seconds is far
	// longer than a scanner holds a file and far shorter than a user waits
	// before deciding the update hung.
	swapAttempts = 20

	// swapUrgentAttempts is the budget for a move made while the target name
	// holds nothing. Windows starts Core only from the install path, so a
	// device interrupted during that gap has nothing left to launch and no
	// watchdog to recover with. A move that runs before the gap opens keeps the
	// long wait, because nothing is at risk while it waits; the one that runs
	// inside it gets a second, because waiting there is not free.
	swapUrgentAttempts = 4

	swapDelay = 250 * time.Millisecond
)

// swapOps is everything a binary swap needs from the platform underneath it,
// gathered in one place so the sequence can be exercised on any host rather
// than only on the one it exists for.
type swapOps struct {
	replace   func(source, target string) error
	remove    func(path string) error
	exists    func(path string) (bool, error)
	transient func(error) bool
	sleep     func(time.Duration)
	// conceal hides an outgoing binary that could not be removed, so a user
	// looking at their install directory does not find a second executable
	// sitting next to the real one. Optional.
	conceal func(string) error
	// vacate says the outgoing file has to be moved out of its own name before
	// the incoming one can take it.
	vacate bool
}

// installBinaryOps is what an install and its unwind need from the binary swap.
// The zero value is the real thing, so only a test that wants to drive the
// vacating sequence on a host that does not need it has to fill it in.
type installBinaryOps struct {
	replaceRunning  func(source, target string) error
	sweepSuperseded func(targetPath string)
}

func defaultInstallBinaryOps() installBinaryOps {
	return installBinaryOps{
		replaceRunning:  replaceRunningBinary,
		sweepSuperseded: sweepSupersededBinary,
	}
}

func (o installBinaryOps) replace(source, target string) error {
	if o.replaceRunning == nil {
		return replaceRunningBinary(source, target)
	}
	return o.replaceRunning(source, target)
}

func (o installBinaryOps) sweep(targetPath string) {
	if o.sweepSuperseded == nil {
		sweepSupersededBinary(targetPath)
		return
	}
	o.sweepSuperseded(targetPath)
}

// replaceRunningBinary puts source at target, where target may be the image the
// calling process is running from.
//
// On Unix that is the ordinary atomic rename: the running process keeps the
// inode it already opened, and the name comes to mean the new file. Windows
// refuses to overwrite a mapped image, but it does allow one to be renamed,
// because the loader holds the file by handle and shares it for delete. So the
// outgoing binary is moved to a sibling name and the incoming one takes the
// name it vacated.
//
// Either way the target only ever holds one whole binary or the other, never a
// partially written one. Where vacating is needed it can also hold neither for
// as long as the two renames take, which is the state the caller's marker and
// the sweep on the next boot exist to recover from.
func replaceRunningBinary(source, target string) error {
	return replaceRunningBinaryWith(source, target, defaultSwapOps())
}

// Rename retries use the long swapAttempts budget until the target-name gap is
// opened. Only the incoming rename that runs inside that gap uses
// swapUrgentAttempts; restoring into an already empty target and undoing a
// failed swap keep the long budget. On Windows, transientSwapError includes
// access denied, sharing violations and lock violations. Removes are never
// retried: a busy superseded slot is skipped, and cleanup is left to a later
// sweep.
func replaceRunningBinaryWith(source, target string, ops swapOps) error {
	if !ops.vacate {
		return retrySwap(ops, swapAttempts, func() error { return ops.replace(source, target) })
	}

	present, err := ops.exists(target)
	if err != nil {
		return fmt.Errorf("checking what holds %q: %w", target, err)
	}
	if !present {
		// A swap interrupted between its two renames leaves the target name
		// empty, and putting a binary back there is the whole point of the
		// unwind that follows. Vacating first would fail on the file that is
		// not there and leave the device with nothing to start.
		return retrySwap(ops, swapAttempts, func() error { return ops.replace(source, target) })
	}

	superseded, err := reserveSupersededPath(target, ops)
	if err != nil {
		return err
	}
	// The target still holds a whole binary until this lands, so it takes the
	// long wait: nothing is at risk while it runs.
	if err := retrySwap(ops, swapAttempts, func() error { return ops.replace(target, superseded) }); err != nil {
		return fmt.Errorf("moving the outgoing binary to %q: %w", superseded, err)
	}

	// The target name holds nothing from here until one of the next two moves
	// lands, which is why this one does not wait long before giving up on it.
	if err := retrySwap(ops, swapUrgentAttempts, func() error { return ops.replace(source, target) }); err != nil {
		// Putting the outgoing binary back is the difference between an update
		// that did not happen and a device with no executable to start, and it
		// is the only thing left that can close the gap. So it keeps the long
		// wait even though the gap is open while it runs.
		undoErr := retrySwap(ops, swapAttempts, func() error { return ops.replace(superseded, target) })
		if undoErr != nil {
			// Both renames failed, so the install path holds no executable and
			// nothing left in this process can put one there. The outgoing image
			// remains recoverable by hand, but it should still look like the
			// internal sidecar it is rather than a second executable.
			if ops.conceal != nil {
				if concealErr := ops.conceal(superseded); concealErr != nil {
					log.Debug().Err(concealErr).Str("path", superseded).
						Msg("could not hide the superseded binary after the swap failed")
				}
			}
			// Whoever reads these logs has to move the outgoing binary back by
			// hand, so name both paths rather than leaving it to an error string
			// that the caller may only record as a failed update.
			log.Error().Err(errors.Join(err, undoErr)).
				Str("target", target).
				Str("superseded", superseded).
				Msg("binary swap left the install path empty; " +
					"rename the superseded binary back to the target path to recover")
			return errors.Join(err, fmt.Errorf("restoring the outgoing binary to %q: %w", target, undoErr))
		}
		return err
	}

	// Named at info because the file it points at outlives this process: a swap
	// that had to vacate leaves the outgoing binary sitting in the install
	// directory until a later sweep clears it, and this is the only record of
	// which slot it went to.
	log.Info().Str("target", target).Str("superseded", superseded).
		Msg("moved the outgoing binary aside to install over it")

	// This normally fails: the process that asked for the swap is still running
	// from the file, and that is exactly why it had to be moved rather than
	// overwritten. sweepSupersededBinary clears it once the process holding it
	// has gone.
	if err := ops.remove(superseded); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Debug().Err(err).Str("path", superseded).
			Msg("superseded binary is still in use, leaving it for the next boot")
		if ops.conceal != nil {
			if concealErr := ops.conceal(superseded); concealErr != nil {
				log.Debug().Err(concealErr).Str("path", superseded).
					Msg("could not hide the superseded binary")
			}
		}
	}
	return nil
}

// reserveSupersededPath picks a name the outgoing binary can be moved to.
// Clearing a name is the test for it: one still holding an image some process
// is running from will not delete, and will not be renamed over either, so it
// is stepped past rather than treated as a failure.
func reserveSupersededPath(targetPath string, ops swapOps) (string, error) {
	var lastErr error
	for slot := range supersededSlots {
		path := supersededPathFor(targetPath, slot)
		err := ops.remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("no free name to move the binary at %q out to: %w", targetPath, lastErr)
}

func supersededPathFor(targetPath string, slot int) string {
	suffix := installSupersededSuffix
	if slot > 0 {
		suffix = fmt.Sprintf("%s-%d", installSupersededSuffix, slot)
	}
	return installSidecarPath(targetPath, suffix)
}

// retrySwap waits out a rename that failed for a reason time fixes. Anything
// else is reported on the first attempt, because retrying a permission error
// twenty times only delays telling the user what is wrong.
func retrySwap(ops swapOps, attempts int, attempt func() error) error {
	var err error
	for i := range attempts {
		err = attempt()
		if err == nil {
			return nil
		}
		if ops.transient == nil || !ops.transient(err) {
			return err
		}
		if i < attempts-1 {
			ops.sleep(swapDelay)
		}
	}
	return fmt.Errorf("gave up after %d attempts: %w", attempts, err)
}

// sweepSupersededBinary removes the outgoing binaries previous swaps could not,
// once whatever was running from them has exited. Failure is not worth
// reporting: the files are inert, and a later swap reuses their names.
func sweepSupersededBinary(targetPath string) {
	sweepSupersededBinaryWith(targetPath, defaultSwapOps())
}

func sweepSupersededBinaryWith(targetPath string, ops swapOps) {
	if targetPath == "" {
		return
	}
	for slot := range supersededSlots {
		path := supersededPathFor(targetPath, slot)
		if err := ops.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Str("path", path).
				Msg("could not remove the binary a previous update superseded")
		}
	}
}

// fileExists answers only what it is asked. A path that cannot be read at all
// is not an absent one, and a swap that treats it as absent would skip moving
// the binary that is actually there.
func fileExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading %q: %w", path, err)
	}
	return true, nil
}
