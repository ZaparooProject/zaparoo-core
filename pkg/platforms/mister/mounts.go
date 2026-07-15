//go:build linux

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

package mister

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

var mountInfoPath = filepath.Join(string(filepath.Separator), "proc", "self", "mountinfo")

// mountEntry is one line of /proc/self/mountinfo, reduced to the fields
// mount ownership decisions need. Entries appear in mount order, so of two
// entries with the same Mountpoint the later one shadows the earlier.
type mountEntry struct {
	Root       string // path inside the source filesystem, e.g. /zaparoo/profiles/x/saves
	Mountpoint string // where it is mounted, e.g. /media/fat/saves
	FSType     string // e.g. exfat, cifs
	Source     string // device or share, e.g. /dev/mmcblk0p1, //nas/share
}

// mounter abstracts the mount syscalls and mount table so the swap logic
// is unit-testable without root or a MiSTer.
type mounter interface {
	Mounts() ([]mountEntry, error)
	// BindMount returns the new mount's identity. On error it must leave no
	// bind mounted at target, so callers never own an untracked mount.
	BindMount(source, target string) (mountEntry, error)
	Unmount(target string) error
}

type sysMounter struct{}

func (sysMounter) Mounts() ([]mountEntry, error) {
	data, err := os.ReadFile(mountInfoPath) //nolint:gosec // fixed procfs path assembled from constants
	if err != nil {
		return nil, fmt.Errorf("failed to read mountinfo: %w", err)
	}
	return parseMountInfo(string(data)), nil
}

func (m sysMounter) BindMount(source, target string) (mountEntry, error) {
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		return mountEntry{}, fmt.Errorf("failed to bind mount %s on %s: %w", source, target, err)
	}

	mounts, err := m.Mounts()
	if err == nil {
		if stack := mountsAt(mounts, target); len(stack) > 0 {
			return stack[len(stack)-1], nil
		}
		err = errors.New("bind mount not visible in mountinfo")
	}

	// Verification failed after the syscall succeeded. Remove the bind now;
	// returning an error with it still live would leave an untracked mount
	// that later reconciles cannot safely identify or remove.
	if unmountErr := unix.Unmount(target, 0); unmountErr != nil {
		return mountEntry{}, errors.Join(
			fmt.Errorf("failed to verify bind mount %s on %s: %w", source, target, err),
			fmt.Errorf("failed to roll back unverified bind mount: %w", unmountErr),
		)
	}
	return mountEntry{}, fmt.Errorf("failed to verify bind mount %s on %s: %w", source, target, err)
}

// Unmount removes the topmost mount at target. EBUSY gets a few brief
// retries (a file may be transiently open), then a lazy detach so the
// mount at least disappears from the namespace once its users exit.
func (sysMounter) Unmount(target string) error {
	var err error
	for range 3 {
		err = unix.Unmount(target, 0)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("failed to unmount %s: %w", target, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Warn().Str("target", target).
		Msg("profiles: unmount busy, detaching lazily")
	if err := unix.Unmount(target, unix.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to lazily unmount %s: %w", target, err)
	}
	return nil
}

// parseMountInfo parses /proc/self/mountinfo content. Format per line:
//
//	36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw
//	(1) (2) (3) (root) (mountpoint) (opts) (optional...) - (fstype) (source) (superopts)
//
// Malformed lines are skipped.
func parseMountInfo(data string) []mountEntry {
	var entries []mountEntry
	for line := range strings.Lines(data) {
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" && i >= 6 {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) || len(fields) < 5 {
			continue
		}
		entries = append(entries, mountEntry{
			Root:       unescapeMountField(fields[3]),
			Mountpoint: unescapeMountField(fields[4]),
			FSType:     fields[sep+1],
			Source:     unescapeMountField(fields[sep+2]),
		})
	}
	return entries
}

// unescapeMountField decodes the octal escapes the kernel uses for
// whitespace in mountinfo fields (e.g. \040 for space).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if code, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				_ = b.WriteByte(byte(code))
				i += 3
				continue
			}
		}
		_ = b.WriteByte(s[i])
	}
	return b.String()
}

// mountsAt returns the mounts stacked on target in mount order: the last
// entry is the one paths currently resolve through.
func mountsAt(mounts []mountEntry, target string) []mountEntry {
	var stack []mountEntry
	for _, m := range mounts {
		if m.Mountpoint == target {
			stack = append(stack, m)
		}
	}
	return stack
}

// mountLedgerEntry records one bind mount we created: enough of its
// mountinfo identity to prove a mount at Target is ours before we ever
// unmount it.
type mountLedgerEntry struct {
	Target    string `json:"target"`
	Root      string `json:"root"`
	Source    string `json:"source"`
	ProfileID string `json:"profileId"`
	Item      string `json:"item"`
}

// mountLedger persists the binds we own. It lives on tmpfs (/run) so it
// has exactly the lifetime of the kernel mount state it describes: both
// survive a service restart and both vanish on reboot.
type mountLedger struct {
	fs      afero.Fs
	path    string
	entries []mountLedgerEntry
}

func loadMountLedger(fs afero.Fs, path string) *mountLedger {
	l := &mountLedger{fs: fs, path: path}
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return l
	}
	if err := json.Unmarshal(data, &l.entries); err != nil {
		log.Warn().Err(err).Str("path", path).
			Msg("profiles: discarding unreadable mount ledger")
		l.entries = nil
	}
	return l
}

func (l *mountLedger) save() error {
	data, err := json.Marshal(l.entries)
	if err != nil {
		return fmt.Errorf("failed to encode mount ledger: %w", err)
	}
	if err := l.fs.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("failed to create mount ledger dir: %w", err)
	}
	if err := afero.WriteFile(l.fs, l.path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write mount ledger: %w", err)
	}
	return nil
}

// find returns our ledger entry for the given live mount, or nil when the
// mount is not one we created.
func (l *mountLedger) find(m *mountEntry) *mountLedgerEntry {
	for i := range l.entries {
		e := &l.entries[i]
		if e.Target == m.Mountpoint && e.Root == m.Root && e.Source == m.Source {
			return e
		}
	}
	return nil
}

// owns reports whether the given mount entry at target is one we created.
func (l *mountLedger) owns(m *mountEntry) bool {
	return l.find(m) != nil
}

func (l *mountLedger) add(entry *mountLedgerEntry) error {
	l.entries = append(l.entries, *entry)
	return l.save()
}

// remove drops the ledger entry matching the given mount.
func (l *mountLedger) remove(m *mountEntry) {
	l.removeInMemory(m)
	if err := l.save(); err != nil {
		log.Error().Err(err).Msg("profiles: failed to update mount ledger")
	}
}

func (l *mountLedger) removeInMemory(m *mountEntry) {
	kept := l.entries[:0]
	for _, e := range l.entries {
		if e.Target == m.Mountpoint && e.Root == m.Root && e.Source == m.Source {
			continue
		}
		kept = append(kept, e)
	}
	l.entries = kept
}

// prune drops ledger entries whose mounts no longer exist (e.g. manually
// unmounted while we weren't looking).
func (l *mountLedger) prune(mounts []mountEntry) {
	kept := l.entries[:0]
	for _, e := range l.entries {
		found := false
		for i := range mounts {
			m := &mounts[i]
			if e.Target == m.Mountpoint && e.Root == m.Root && e.Source == m.Source {
				found = true
				break
			}
		}
		if found {
			kept = append(kept, e)
		}
	}
	if len(kept) != len(l.entries) {
		l.entries = kept
		if err := l.save(); err != nil {
			log.Error().Err(err).Msg("profiles: failed to prune mount ledger")
		}
	}
}
