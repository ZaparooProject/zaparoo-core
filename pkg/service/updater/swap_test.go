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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errBusy stands in for the sharing violation Windows reports while another
// process holds a file this swap has to move.
var errBusy = errors.New("the file is in use by another process")

// swapFixture is a real directory with a target and an incoming binary in it,
// so the sequence is checked against what ends up on disk rather than against a
// record of which calls were made.
type swapFixture struct {
	t          *testing.T
	dir        string
	target     string
	source     string
	superseded string
	slept      int
}

func newSwapFixture(t *testing.T) *swapFixture {
	t.Helper()
	dir := t.TempDir()
	f := &swapFixture{
		t:          t,
		dir:        dir,
		target:     filepath.Join(dir, "zaparoo.exe"),
		source:     filepath.Join(dir, "zaparoo.zaparoo-update-new.exe"),
		superseded: filepath.Join(dir, "zaparoo"+installSupersededSuffix+".exe"),
	}
	require.NoError(t, os.WriteFile(f.target, []byte("old binary"), 0o600))
	require.NoError(t, os.WriteFile(f.source, []byte("new binary"), 0o600))
	return f
}

// ops builds swap behaviour that renames for real. fail decides which renames
// report an error instead, keyed on the path being moved: the outgoing binary
// leaving its own name, the incoming binary taking it, and the undo putting the
// outgoing one back are three distinct moves out of three distinct paths.
func (f *swapFixture) ops(vacate bool, fail func(source string) error) swapOps {
	return swapOps{
		replace: func(source, target string) error {
			if fail != nil {
				if err := fail(source); err != nil {
					return err
				}
			}
			return os.Rename(source, target)
		},
		remove:    os.Remove,
		exists:    fileExists,
		transient: func(err error) bool { return errors.Is(err, errBusy) },
		sleep:     func(time.Duration) { f.slept++ },
		vacate:    vacate,
	}
}

// vacatingBinaryOps drives the install and its unwind through the swap Windows
// needs, so the sequence is exercised on the host running the tests rather than
// only on the one platform that reaches it in production.
func vacatingBinaryOps(ops swapOps) installBinaryOps {
	return installBinaryOps{
		replaceRunning: func(source, target string) error {
			return replaceRunningBinaryWith(source, target, ops)
		},
		sweepSuperseded: func(targetPath string) { sweepSupersededBinaryWith(targetPath, ops) },
	}
}

// realVacatingOps renames for real and never pretends anything is transient, so
// it stands in for a Windows host with no scanner holding anything.
func realVacatingOps() swapOps {
	ops := defaultSwapOps()
	ops.vacate = true
	ops.transient = func(error) bool { return false }
	return ops
}

// mappedImageOps stands in for a Windows host where the named file is the image
// a process is running from: it will not delete and will not be renamed over,
// but renaming into the name while it is empty, or away from it, is fine.
//
// The restriction follows the file, not the name. Windows holds a mapped image
// by handle, so renaming it moves the whole thing including the lock, and a
// double stuck on the original path would say a file was free at the one place
// it cannot be.
func mappedImageOps(mapped string) swapOps {
	image := mapped
	held := func(path string) bool {
		if path != image {
			return false
		}
		_, err := os.Lstat(path)
		return err == nil
	}
	ops := realVacatingOps()
	ops.remove = func(path string) error {
		if held(path) {
			return errBusy
		}
		return os.Remove(path)
	}
	ops.replace = func(source, target string) error {
		if held(target) {
			return errBusy
		}
		moving := held(source)
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("renaming %q to %q: %w", source, target, err)
		}
		if moving {
			image = target
		}
		return nil
	}
	return ops
}

// failing makes moves out of path report err, until it has done so that many
// times. A negative count never relents.
func failing(path string, err error, times int) func(string) error {
	remaining := times
	return func(source string) error {
		if source != path || remaining == 0 {
			return nil
		}
		if remaining > 0 {
			remaining--
		}
		return err
	}
}

func (f *swapFixture) contents(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	require.NoError(f.t, err)
	return string(data)
}

// Platforms that let a running binary be overwritten do exactly one rename, and
// must not leave a superseded copy behind for a sweep that has no reason to run.
func TestReplaceRunningBinary_ReplacesInPlaceWhenNoVacatingIsNeeded(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, f.ops(false, nil)))

	assert.Equal(t, "new binary", f.contents(f.target))
	assert.NoFileExists(t, f.source)
	assert.NoFileExists(t, f.superseded)
}

// Windows will not overwrite a mapped image but will rename one, so the
// outgoing binary has to leave its own name before the incoming one takes it.
func TestReplaceRunningBinary_MovesTheOutgoingBinaryAside(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, f.ops(true, nil)))

	assert.Equal(t, "new binary", f.contents(f.target))
	assert.NoFileExists(t, f.source)
	// The process that asked for the swap is normally still running from it, so
	// it is removed here only because this test is not.
	assert.NoFileExists(t, f.superseded)
}

// A superseded copy an earlier update could not delete must not stop the next
// one: the swap overwrites it rather than refusing.
func TestReplaceRunningBinary_OverwritesAnEarlierSupersededCopy(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, os.WriteFile(f.superseded, []byte("even older binary"), 0o600))

	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, f.ops(true, nil)))
	assert.Equal(t, "new binary", f.contents(f.target))
}

// If the incoming binary cannot take the name the outgoing one vacated, there
// is nothing at the target at all. Leaving it that way would take the device's
// executable away over an update that did not happen.
func TestReplaceRunningBinary_PutsTheOutgoingBinaryBackWhenTheSwapFails(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	fatal := errors.New("permission denied")
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, failing(f.source, fatal, -1)))

	require.ErrorIs(t, err, fatal)
	assert.Equal(t, "old binary", f.contents(f.target))
	assert.NoFileExists(t, f.superseded)
}

// The one outcome worse than a failed update: neither binary holds the target
// name. Nothing can fix that from here, so the error has to carry both halves
// of what went wrong rather than only the swap that started it.
func TestReplaceRunningBinary_ReportsBothFailuresWhenTheOutgoingBinaryCannotGoBack(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	fatal := errors.New("permission denied")
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, func(source string) error {
		// The outgoing binary gets out of its own name; nothing gets back into it.
		if source == f.target {
			return nil
		}
		return fatal
	}))

	require.ErrorIs(t, err, fatal)
	assert.Contains(t, err.Error(), f.target,
		"the error has to name the binary that is no longer there")
	assert.NoFileExists(t, f.target)
	assert.FileExists(t, f.superseded, "the outgoing binary is still recoverable by hand")
}

// A scanner holding a binary for a moment is not a failed update.
func TestReplaceRunningBinary_WaitsOutAFileAnotherProcessIsHolding(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, f.ops(true, failing(f.source, errBusy, 3))))

	assert.Equal(t, "new binary", f.contents(f.target))
	assert.Equal(t, 3, f.slept, "each retry has to wait before trying again")
}

func TestReplaceRunningBinary_GivesUpOnAFileThatIsNeverReleased(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, failing(f.target, errBusy, -1)))

	require.ErrorIs(t, err, errBusy)
	assert.Equal(t, "old binary", f.contents(f.target),
		"a swap that never started must leave the running binary where it is")
	assert.Equal(t, swapAttempts-1, f.slept)
}

// Anything that is not going to pass has to be reported the first time. Waiting
// five seconds to repeat a permission error only delays telling the user.
func TestReplaceRunningBinary_DoesNotRetryAnErrorTimeWillNotFix(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	fatal := errors.New("permission denied")
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, failing(f.target, fatal, -1)))

	require.ErrorIs(t, err, fatal)
	assert.Zero(t, f.slept)
}

// A swap interrupted between its two renames leaves nothing at the target. That
// is the state the unwind exists to fix, and moving the target aside first
// would fail on the file that is not there and leave the device with nothing to
// start.
func TestReplaceRunningBinary_FillsATargetNameLeftEmptyByAnInterruptedSwap(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, os.Remove(f.target))
	require.NoError(t, os.WriteFile(f.superseded, []byte("old binary"), 0o600))

	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, f.ops(true, nil)))

	assert.Equal(t, "new binary", f.contents(f.target))
	assert.Equal(t, "old binary", f.contents(f.superseded),
		"nothing was moved aside, so nothing was cleared")
}

// After a swap, the name the outgoing binary was moved to holds the image this
// process is running from. An install that then fails has to put the old binary
// back, and it cannot do that by renaming onto the file it is running out of.
func TestReplaceRunningBinary_LeavesAMappedOutgoingBinaryWhereItIs(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	require.NoError(t, os.WriteFile(f.superseded, []byte("running image"), 0o600))

	ops := f.ops(true, nil)
	ops.remove = func(path string) error {
		if path == f.superseded {
			return errBusy
		}
		return os.Remove(path)
	}
	ops.replace = func(source, target string) error {
		if source == f.superseded || target == f.superseded {
			t.Errorf("the running image at %s must not be moved", f.superseded)
			return errBusy
		}
		return os.Rename(source, target)
	}

	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, ops))

	assert.Equal(t, "new binary", f.contents(f.target))
	assert.Equal(t, "running image", f.contents(f.superseded))
	assert.NoFileExists(t, supersededPathFor(f.target, 1),
		"the name the swap fell back to is cleared once it is done with it")
}

// Every name being held is not something to keep trying: there is nowhere to
// put the outgoing binary, and the target has not been touched.
func TestReplaceRunningBinary_ReportsWhenNoNameIsFreeToMoveTheOutgoingBinaryTo(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	ops := f.ops(true, nil)
	ops.remove = func(string) error { return errBusy }

	err := replaceRunningBinaryWith(f.source, f.target, ops)

	require.ErrorIs(t, err, errBusy)
	assert.Equal(t, "old binary", f.contents(f.target))
	assert.Equal(t, "new binary", f.contents(f.source))
}

// The gap where the target name holds nothing is the one place a device can be
// interrupted and have nothing left to start, so waiting there is not free.
func TestReplaceRunningBinary_WaitsLessWhileTheTargetNameIsEmpty(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, failing(f.source, errBusy, -1)))

	require.ErrorIs(t, err, errBusy)
	assert.Equal(t, swapUrgentAttempts-1, f.slept,
		"the move that opens the gap must not spend the long budget inside it")
	assert.Equal(t, "old binary", f.contents(f.target))
}

// Putting the outgoing binary back is the only thing left that can close that
// gap, so it keeps the long wait even though the gap is open while it runs.
func TestReplaceRunningBinary_WaitsOutAHeldFileToPutTheOutgoingBinaryBack(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	undoHeld := swapUrgentAttempts + 2
	err := replaceRunningBinaryWith(f.source, f.target, f.ops(true, func(source string) error {
		switch source {
		case f.source:
			return errBusy
		case f.superseded:
			if undoHeld > 0 {
				undoHeld--
				return errBusy
			}
		}
		return nil
	}))

	require.ErrorIs(t, err, errBusy)
	assert.Equal(t, "old binary", f.contents(f.target),
		"the undo has to keep going past the budget of the move that failed")
	assert.Zero(t, undoHeld)
}

func TestSweepSupersededBinary(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	fallback := supersededPathFor(f.target, 1)
	require.NoError(t, os.WriteFile(f.superseded, []byte("old binary"), 0o600))
	require.NoError(t, os.WriteFile(fallback, []byte("older binary"), 0o600))

	sweepSupersededBinary(f.target)
	assert.NoFileExists(t, f.superseded)
	assert.NoFileExists(t, fallback, "a swap that had to fall back to a second name is cleared too")
	assert.FileExists(t, f.target, "the sweep only removes what a swap moved aside")

	// Nothing to sweep is the ordinary case, on every boot that did not update.
	assert.NotPanics(t, func() { sweepSupersededBinary(f.target) })
	assert.NotPanics(t, func() { sweepSupersededBinary("") })
}

// The outgoing binary normally cannot be deleted, because the process asking
// for the swap is still running from it. It should not be left sitting in the
// install directory looking like a second copy of the program.
func TestReplaceRunningBinary_HidesAnOutgoingBinaryItCannotRemove(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	ops := f.ops(true, nil)
	// Nothing is holding the name before the swap moves the outgoing binary
	// into it, which is why the name is free to use in the first place.
	ops.remove = func(path string) error {
		if _, err := os.Lstat(path); err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}
		return errors.New("the file is in use")
	}
	var concealed []string
	ops.conceal = func(path string) error {
		concealed = append(concealed, path)
		return nil
	}

	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, ops))
	assert.Equal(t, "new binary", f.contents(f.target))
	assert.Equal(t, []string{f.superseded}, concealed)
}

// A swap that removed the outgoing binary has nothing to hide.
func TestReplaceRunningBinary_DoesNotHideABinaryItRemoved(t *testing.T) {
	t.Parallel()

	f := newSwapFixture(t)
	ops := f.ops(true, nil)
	ops.conceal = func(string) error {
		t.Error("nothing should be hidden when the outgoing binary is gone")
		return nil
	}

	require.NoError(t, replaceRunningBinaryWith(f.source, f.target, ops))
}
