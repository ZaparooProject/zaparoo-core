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
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// Profile data swapping on MiSTer works by bind-mounting a per-profile
// directory over the live saves/savestates directories. Main hardcodes
// SAVE_DIR/SAVESTATE_DIR relative to its storage root, so directory-level
// redirection is the only mechanism — and bind mounts do it with zero
// on-disk mutation: the SD card stays a completely standard exFAT layout,
// nothing moves or copies, and a power cut mid-switch can't tear anything.
// Mount state survives a service crash and clears on reboot; the boot
// reconcile pass reapplies it.
const (
	// profileDataItemSaves and ...Savestates are the swappable item IDs.
	profileDataItemSaves      = "saves"
	profileDataItemSavestates = "savestates"

	// nasPoolDirName is the pool directory created inside a foreign mount
	// (e.g. a NAS share bind-mounted over saves/ by cifs_mount.sh). The
	// share root IS the saves directory, so inside it is the only place on
	// that storage we can reach.
	nasPoolDirName = ".zaparoo-profiles"

	// usbRootCount matches main's isUSBMounted probe of /media/usb0-3.
	usbRootCount = 4
)

var (
	// mountLedgerPath records the binds we own. On tmpfs so it lives and
	// dies with the kernel mount state it describes.
	mountLedgerPath = filepath.Join(string(filepath.Separator), "run", "zaparoo", "mounts.json")

	// deviceBinPath is where main's OSD Storage menu persists the storage
	// root selection (FileSave of an int: 0 = SD, nonzero = USB).
	deviceBinPath = filepath.Join(misterconfig.CoreConfigFolder, "device.bin")
	mediaRootPath = filepath.Join(string(filepath.Separator), "media")
)

func profileItemDir(item string) string {
	// Item IDs happen to equal main's directory names.
	return item
}

// profileDataManager owns the mount decisions. All paths are derived per
// apply so storage-root changes (SD vs USB) and foreign mounts (NAS saves)
// are re-resolved every time.
type profileDataManager struct {
	fs     afero.Fs
	m      mounter
	ledger *mountLedger
	mu     syncutil.Mutex
}

func newProfileDataManager(fs afero.Fs) *profileDataManager {
	return &profileDataManager{
		fs:     fs,
		m:      sysMounter{},
		ledger: loadMountLedger(fs, mountLedgerPath),
	}
}

// ProfileItems implements platforms.ProfileDataSwapper.
func (*Platform) ProfileItems() []platforms.ProfileItem {
	return []platforms.ProfileItem{
		{ID: profileDataItemSaves, Label: "Save files", Owner: platforms.ProfileItemOwnerProfile},
		{ID: profileDataItemSavestates, Label: "Save states", Owner: platforms.ProfileItemOwnerProfile},
	}
}

// ApplyProfile implements platforms.ProfileDataSwapper.
func (p *Platform) ApplyProfile(ref platforms.ProfileRef, enabledItems []string) error {
	return p.profileData.apply(ref, enabledItems)
}

type profileItemPlan struct {
	ref      platforms.ProfileRef
	previous platforms.ProfileRef
	item     string
	target   string
	pool     string
}

func (d *profileDataManager) apply(ref platforms.ProfileRef, items []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	mounts, err := d.m.Mounts()
	if err != nil {
		return fmt.Errorf("failed to read mount table: %w", err)
	}
	d.ledger.prune(mounts)

	root, err := d.resolveStorageRoot(mounts)
	if err != nil {
		return err
	}

	// Prepare every destination before changing any live mount. A missing or
	// read-only pool therefore leaves all currently active profile data intact.
	plans := make([]profileItemPlan, 0, len(items))
	for _, item := range items {
		plan, planErr := d.prepareItem(root, item, ref)
		if planErr != nil {
			return fmt.Errorf("%s: %w", item, planErr)
		}
		plans = append(plans, plan)
	}

	for i := range plans {
		if applyErr := d.applyItem(&plans[i]); applyErr != nil {
			rollbackErr := d.rollbackItems(root, plans[:i+1])
			return errors.Join(
				fmt.Errorf("%s: %w", plans[i].item, applyErr),
				rollbackErr,
			)
		}
	}
	return nil
}

func (d *profileDataManager) prepareItem(root, item string, ref platforms.ProfileRef) (profileItemPlan, error) {
	plan := profileItemPlan{
		ref:    ref,
		item:   item,
		target: filepath.Join(root, profileItemDir(item)),
	}

	mounts, err := d.m.Mounts()
	if err != nil {
		return profileItemPlan{}, fmt.Errorf("failed to read mount table: %w", err)
	}
	stack := mountsAt(mounts, plan.target)
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		entry := d.ledger.find(&top)
		if entry == nil {
			break
		}
		if plan.previous.ID == "" {
			plan.previous.ID = entry.ProfileID
		}
		stack = stack[:len(stack)-1]
	}

	if ref.ID == "" {
		return plan, nil
	}

	var profileDir string
	if len(stack) > 0 {
		// A foreign mount (e.g. NAS saves) owns the target: the pool must
		// live inside it, and our bind stacks on top. Saves stay on the
		// user's storage; we never touch their mount.
		profileDir = filepath.Join(plan.target, nasPoolDirName, ref.ID)
	} else {
		profileDir = filepath.Join(root, "zaparoo", "profiles", ref.ID)
	}
	plan.pool = filepath.Join(profileDir, profileItemDir(item))

	if mkdirErr := d.fs.MkdirAll(plan.pool, 0o755); mkdirErr != nil {
		if len(stack) > 0 {
			return profileItemPlan{}, fmt.Errorf(
				"cannot create profile pool inside mounted %s (%s): %w",
				plan.target, stack[len(stack)-1].FSType, platforms.ErrProfileDataUnavailable)
		}
		return profileItemPlan{}, fmt.Errorf("failed to create profile pool %s: %w", plan.pool, mkdirErr)
	}
	d.writeNameFile(profileDir, ref)
	return plan, nil
}

func (d *profileDataManager) rollbackItems(root string, plans []profileItemPlan) error {
	var errs []error
	for i := len(plans) - 1; i >= 0; i-- {
		rollback, err := d.prepareItem(root, plans[i].item, plans[i].previous)
		if err == nil {
			err = d.applyItem(&rollback)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to restore %s: %w", plans[i].item, err))
		}
	}
	return errors.Join(errs...)
}

// resolveStorageRoot mirrors main's FindStorage: config/device.bin selects
// SD or USB, and USB means the first mounted non-ext filesystem of
// /media/usb0-3 (main's isPathMounted quirk: an ext-formatted drive is
// never a storage root).
func (d *profileDataManager) resolveStorageRoot(mounts []mountEntry) (string, error) {
	data, err := afero.ReadFile(d.fs, deviceBinPath)
	if err != nil || len(data) < 4 {
		return misterconfig.SDRootDir, nil //nolint:nilerr // absent/short file means SD root
	}
	if binary.LittleEndian.Uint32(data[:4]) == 0 {
		return misterconfig.SDRootDir, nil
	}

	for i := range usbRootCount {
		path := filepath.Join(mediaRootPath, fmt.Sprintf("usb%d", i))
		stack := mountsAt(mounts, path)
		if len(stack) == 0 {
			continue
		}
		if strings.HasPrefix(stack[len(stack)-1].FSType, "ext") {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf(
		"USB storage root selected but no USB drive mounted: %w",
		platforms.ErrProfileDataUnavailable)
}

func (d *profileDataManager) applyItem(plan *profileItemPlan) error {
	// Idempotency: if the topmost mount is already our bind for this
	// profile, there is nothing to do. Reconciles run on every mount-table
	// change and must not churn mounts.
	if plan.ref.ID != "" {
		mounts, err := d.m.Mounts()
		if err != nil {
			return fmt.Errorf("failed to read mount table: %w", err)
		}
		if stack := mountsAt(mounts, plan.target); len(stack) > 0 {
			top := stack[len(stack)-1]
			if e := d.ledger.find(&top); e != nil && e.ProfileID == plan.ref.ID && e.Item == plan.item {
				return nil
			}
		}
	}

	// Remove our own binds from the top of the stack. Anything of ours
	// buried under a later foreign mount is unreachable without touching
	// the foreign mount, so it stays until reboot — harmless, since the
	// foreign mount defines what paths resolve to.
	for {
		mounts, err := d.m.Mounts()
		if err != nil {
			return fmt.Errorf("failed to read mount table: %w", err)
		}
		stack := mountsAt(mounts, plan.target)
		if len(stack) == 0 {
			break
		}
		top := stack[len(stack)-1]
		if !d.ledger.owns(&top) {
			break
		}
		if unmountErr := d.m.Unmount(plan.target); unmountErr != nil {
			return fmt.Errorf("failed to remove profile bind on %s: %w", plan.target, unmountErr)
		}
		d.ledger.remove(&top)
	}

	if plan.ref.ID == "" {
		// Shared profile: the un-mounted state (plain directory or the
		// user's own NAS mount) is the shared data.
		return nil
	}

	top, bindErr := d.m.BindMount(plan.pool, plan.target)
	if bindErr != nil {
		return fmt.Errorf("failed to bind profile pool: %w", bindErr)
	}
	entry := &mountLedgerEntry{
		Target:    plan.target,
		Root:      top.Root,
		Source:    top.Source,
		ProfileID: plan.ref.ID,
		Item:      plan.item,
	}
	if ledgerErr := d.ledger.add(entry); ledgerErr != nil {
		// Ownership must be durable before reporting success. Undo the bind;
		// otherwise a service restart could no longer prove it is ours.
		unmountErr := d.m.Unmount(plan.target)
		d.ledger.removeInMemory(&top)
		return errors.Join(
			fmt.Errorf("failed to record profile bind: %w", ledgerErr),
			unmountErr,
		)
	}
	log.Info().Str("profileId", plan.ref.ID).Str("pool", plan.pool).Str("target", plan.target).
		Msg("profiles: bind mounted profile data")
	return nil
}

// writeNameFile labels the profile pool directory with the profile's
// display name so the UUID directories are decodable by a human browsing
// the storage outside Zaparoo. Best-effort.
func (d *profileDataManager) writeNameFile(profileDir string, ref platforms.ProfileRef) {
	if ref.Name == "" {
		return
	}
	path := filepath.Join(profileDir, "name.txt")
	if err := afero.WriteFile(d.fs, path, []byte(ref.Name+"\n"), 0o644); err != nil {
		log.Warn().Err(err).Str("path", path).
			Msg("profiles: failed to write profile name file")
	}
}

// WatchProfileData implements platforms.ProfileDataWatcher: it invokes
// onChange whenever the mount table changes (the kernel signals POLLPRI on
// /proc/self/mountinfo), debounced, until ctx is done. This is how the
// service notices a cifs boot script mounting over saves after us, a USB
// root drive appearing late, or a manual unmount.
func (*Platform) WatchProfileData(ctx context.Context, onChange func()) {
	go func() {
		file, err := os.Open(mountInfoPath)
		if err != nil {
			log.Error().Err(err).Msg("profiles: mount watcher unavailable")
			return
		}
		defer func() { _ = file.Close() }()

		rawFd := file.Fd()
		if rawFd > math.MaxInt32 {
			log.Error().Uint64("fd", uint64(rawFd)).Msg("profiles: mount watcher fd out of range")
			return
		}
		fd := int32(rawFd)

		lastSum := mountInfoSum()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			fds := []unix.PollFd{{Fd: fd, Events: unix.POLLPRI}}
			if _, err := unix.Poll(fds, 1000); err != nil && !errors.Is(err, unix.EINTR) {
				log.Warn().Err(err).Msg("profiles: mount watcher poll failed, stopping")
				return
			}
			if fds[0].Revents == 0 {
				continue
			}

			// Debounce bursts (a cifs script mounts several dirs at once).
			time.Sleep(200 * time.Millisecond)

			sum := mountInfoSum()
			if sum == lastSum {
				continue
			}
			lastSum = sum
			log.Debug().Msg("profiles: mount table changed, reconciling")
			onChange()
		}
	}()
}

// mountInfoSum fingerprints the current mount table so poll wakeups that
// end up changing nothing (mount + matching unmount) don't trigger work.
func mountInfoSum() [32]byte {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}
