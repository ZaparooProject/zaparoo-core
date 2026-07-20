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

// Backup-facing adaptation of profile data mounts. Active profile binds
// hide the shared saves/savestates underneath them, so the backup plan
// must describe what is actually visible (and warn about what is not),
// and restore must temporarily remove Zaparoo's own binds so writes reach
// real storage instead of a profile pool.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

type profileRestoreMount struct {
	ref  platforms.ProfileRef
	item profileDataItemSpec
}

func removeBackupDefinition(
	definitions []platforms.BackupDefinition, sourceRoot, restoreRoot, category string,
) []platforms.BackupDefinition {
	filtered := make([]platforms.BackupDefinition, 0, len(definitions))
	for i := range definitions {
		definition := definitions[i]
		if definition.Category == category &&
			filepath.Clean(definition.SourceRoot) == filepath.Clean(sourceRoot) &&
			filepath.Clean(definition.RestoreRoot) == filepath.Clean(restoreRoot) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

func profileBindUsesNAS(entry *mountLedgerEntry) bool {
	root := filepath.ToSlash(entry.Root)
	return strings.Contains(root, "/"+nasPoolDirName+"/")
}

func (d *profileDataManager) backupPlan(
	settings platforms.Settings, definitions []platforms.BackupDefinition,
) platforms.BackupPlan {
	d.mu.Lock()
	defer d.mu.Unlock()
	plan := platforms.BackupPlan{Definitions: definitions}
	mounts, err := d.m.Mounts()
	if err != nil {
		for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
			plan.Definitions = removeBackupDefinition(
				plan.Definitions, filepath.Join(BackupRestoreRoot(settings), item), item, item,
			)
			plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
				Category: item, Path: item, Reason: "profile mount state unavailable",
			})
		}
		return plan
	}
	d.ledger.prune(mounts)
	root, err := d.resolveStorageRoot(mounts)
	if err != nil {
		for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
			plan.Definitions = removeBackupDefinition(
				plan.Definitions, filepath.Join(BackupRestoreRoot(settings), item), item, item,
			)
			plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
				Category: item, Path: item, Reason: "profile storage root unavailable",
			})
		}
		return plan
	}
	baseRoot := BackupRestoreRoot(settings)
	if filepath.Clean(root) != filepath.Clean(baseRoot) {
		for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
			plan.Definitions = removeBackupDefinition(
				plan.Definitions, filepath.Join(baseRoot, item), item, item,
			)
			plan.Definitions = removeBackupDefinition(
				plan.Definitions, filepath.Join(baseRoot, "zaparoo", "profiles"),
				filepath.Join("zaparoo", "profiles"), item,
			)
			plan.Definitions = append(plan.Definitions,
				platforms.BackupDefinition{
					Category: item, SourceRoot: filepath.Join(root, item), RestoreRoot: item,
					Include: []platforms.BackupPattern{{All: true}},
				},
				platforms.BackupDefinition{
					Category: item, SourceRoot: filepath.Join(root, "zaparoo", "profiles"),
					RestoreRoot: filepath.Join("zaparoo", "profiles"),
					Include:     []platforms.BackupPattern{{Contains: "/" + item + "/"}},
				},
			)
		}
	}
	for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
		target := filepath.Join(root, item)
		stack := mountsAt(mounts, target)
		if len(stack) == 0 {
			continue
		}
		entry := d.ledger.find(&stack[len(stack)-1])
		if entry == nil {
			continue
		}
		plan.Definitions = removeBackupDefinition(plan.Definitions, target, item, item)
		if profileBindUsesNAS(entry) {
			plan.Definitions = append(plan.Definitions, platforms.BackupDefinition{
				Category: item, SourceRoot: target,
				RestoreRoot:        filepath.Join(item, nasPoolDirName, entry.ProfileID, item),
				SourceTrustedRoots: []string{target}, Include: []platforms.BackupPattern{{All: true}},
			})
			plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
				Category: item, Path: filepath.ToSlash(filepath.Join(item, nasPoolDirName)),
				Reason: "shared and inactive NAS profile data hidden by active profile mount",
			})
			continue
		}
		plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
			Category: item, Path: item, Reason: "shared profile data hidden by active profile mount",
		})
	}
	return plan
}

func (d *profileDataManager) restoreProfileMounts(root string, mounts []profileRestoreMount) error {
	var errs []error
	for _, mount := range mounts {
		plan, err := d.prepareItem(root, mount.item, mount.ref)
		if err == nil {
			err = d.applyItem(&plan)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restoring %s profile mount: %w", mount.item.id, err))
		}
	}
	return errors.Join(errs...)
}

func (d *profileDataManager) prepareBackupRestore() (func(bool) error, error) {
	d.mu.Lock()
	mounts, err := d.m.Mounts()
	if err != nil {
		d.mu.Unlock()
		return nil, fmt.Errorf("reading profile mounts before backup restore: %w", err)
	}
	d.ledger.prune(mounts)
	root, err := d.resolveStorageRoot(mounts)
	if err != nil {
		d.mu.Unlock()
		return nil, err
	}
	previous := make([]profileRestoreMount, 0, 2)
	for _, itemID := range []string{profileDataItemSaves, profileDataItemSavestates} {
		item, ok := findProfileDataItem(itemID)
		if !ok {
			d.mu.Unlock()
			return nil, fmt.Errorf("unknown backup profile data item %q", itemID)
		}
		target := filepath.Join(root, item.id)
		stack := mountsAt(mounts, target)
		if len(stack) > 0 {
			if entry := d.ledger.find(&stack[len(stack)-1]); entry != nil {
				previous = append(previous, profileRestoreMount{
					ref: platforms.ProfileRef{ID: entry.ProfileID}, item: item,
				})
			}
		}
		if err = d.applyItem(&profileItemPlan{item: item, target: target}); err != nil {
			restoreErr := d.restoreProfileMounts(root, previous)
			d.mu.Unlock()
			return nil, errors.Join(fmt.Errorf("exposing %s for backup restore: %w", item.id, err), restoreErr)
		}
		mounts, err = d.m.Mounts()
		if err != nil {
			restoreErr := d.restoreProfileMounts(root, previous)
			d.mu.Unlock()
			return nil, errors.Join(fmt.Errorf("refreshing profile mounts: %w", err), restoreErr)
		}
	}
	finished := false
	return func(success bool) error {
		if finished {
			return nil
		}
		finished = true
		defer d.mu.Unlock()
		if success {
			return nil
		}
		return d.restoreProfileMounts(root, previous)
	}, nil
}
