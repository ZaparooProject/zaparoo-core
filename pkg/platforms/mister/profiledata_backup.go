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
// hide shared saves and savestates. Backup preparation creates private bind
// aliases to that underlying data, then immediately restores active profile
// mounts. Restore temporarily removes Zaparoo's binds so writes reach storage.

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
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

func profileBackupDefinitionsForRoot(
	settings platforms.Settings, definitions []platforms.BackupDefinition, root string,
) []platforms.BackupDefinition {
	baseRoot := BackupRestoreRoot(settings)
	if filepath.Clean(root) == filepath.Clean(baseRoot) {
		return definitions
	}
	for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
		definitions = removeBackupDefinition(
			definitions, filepath.Join(baseRoot, item), item, item,
		)
		definitions = removeBackupDefinition(
			definitions, filepath.Join(baseRoot, "zaparoo", "profiles"),
			filepath.Join("zaparoo", "profiles"), item,
		)
		profilePatterns := []platforms.BackupPattern{{Contains: "/" + item + "/"}}
		if item == profileDataItemSaves {
			profilePatterns = append(profilePatterns, platforms.BackupPattern{Glob: profileNameFile})
		}
		definitions = append(definitions,
			platforms.BackupDefinition{
				Category: item, SourceRoot: filepath.Join(root, item), RestoreRoot: item,
				Include: []platforms.BackupPattern{{All: true}},
			},
			platforms.BackupDefinition{
				Category: item, SourceRoot: filepath.Join(root, "zaparoo", "profiles"),
				RestoreRoot: filepath.Join("zaparoo", "profiles"), Include: profilePatterns,
			},
		)
	}
	return definitions
}

func (d *profileDataManager) prepareBackup(
	settings platforms.Settings, definitions []platforms.BackupDefinition,
) (platforms.BackupPlan, func() error, error) {
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
		return plan, func() error { return nil }, nil
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
		return plan, func() error { return nil }, nil
	}
	plan.Definitions = profileBackupDefinitionsForRoot(settings, plan.Definitions, root)

	hasActiveBind := false
	for _, item := range []string{profileDataItemSaves, profileDataItemSavestates} {
		stack := mountsAt(mounts, filepath.Join(root, item))
		if len(stack) > 0 && d.ledger.find(&stack[len(stack)-1]) != nil {
			hasActiveBind = true
			break
		}
	}
	if !hasActiveBind {
		return plan, func() error { return nil }, nil
	}

	if err = d.fs.MkdirAll(settings.TempDir, 0o750); err != nil {
		return platforms.BackupPlan{}, nil, fmt.Errorf("creating profile backup temp root: %w", err)
	}
	aliasRoot, err := afero.TempDir(d.fs, settings.TempDir, "profile-backup-")
	if err != nil {
		return platforms.BackupPlan{}, nil, fmt.Errorf("creating profile backup alias root: %w", err)
	}
	aliases := make([]string, 0, 2)
	cleanupAliases := func() error {
		var errs []error
		for i := len(aliases) - 1; i >= 0; i-- {
			if unmountErr := d.m.Unmount(aliases[i]); unmountErr != nil {
				errs = append(errs, fmt.Errorf("unmounting profile backup alias %s: %w", aliases[i], unmountErr))
			}
		}
		if removeErr := d.fs.RemoveAll(aliasRoot); removeErr != nil {
			errs = append(errs, fmt.Errorf("removing profile backup alias root: %w", removeErr))
		}
		return errors.Join(errs...)
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

		originalDefinition := platforms.BackupDefinition{
			Category: item, SourceRoot: target, RestoreRoot: item,
			Include: []platforms.BackupPattern{{All: true}},
		}
		plan.Definitions = removeBackupDefinition(plan.Definitions, target, item, item)
		itemSpec, ok := findProfileDataItem(item)
		if !ok {
			return platforms.BackupPlan{}, nil, errors.Join(
				fmt.Errorf("unknown backup profile data item %q", item), cleanupAliases(),
			)
		}
		previous := platforms.ProfileRef{ID: entry.ProfileID}
		if err = d.applyItem(&profileItemPlan{item: itemSpec, target: target}); err != nil {
			plan.Definitions = append(plan.Definitions, originalDefinition)
			plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
				Category: item, Path: item, Reason: "shared profile data unavailable during backup",
			})
			continue
		}

		alias := filepath.Join(aliasRoot, item)
		if err = d.fs.MkdirAll(alias, 0o750); err == nil {
			_, err = d.m.BindMount(target, alias)
			if err == nil {
				aliases = append(aliases, alias)
			}
		}
		restorePlan, restoreErr := d.prepareItem(root, itemSpec, previous)
		if restoreErr == nil {
			restoreErr = d.applyItem(&restorePlan)
		}
		if restoreErr != nil {
			return platforms.BackupPlan{}, nil, errors.Join(
				fmt.Errorf("restoring %s profile mount after preparing backup: %w", item, restoreErr),
				cleanupAliases(),
			)
		}
		mounts, restoreErr = d.m.Mounts()
		if restoreErr != nil {
			return platforms.BackupPlan{}, nil, errors.Join(
				fmt.Errorf("refreshing profile mounts after preparing backup: %w", restoreErr),
				cleanupAliases(),
			)
		}
		if err != nil {
			plan.Definitions = append(plan.Definitions, originalDefinition)
			plan.Warnings = append(plan.Warnings, platforms.BackupWarning{
				Category: item, Path: item, Reason: "shared profile data unavailable during backup",
			})
			continue
		}
		plan.Definitions = append(plan.Definitions, platforms.BackupDefinition{
			Category: item, SourceRoot: alias, RestoreRoot: item,
			SourceTrustedRoots: []string{alias}, Include: []platforms.BackupPattern{{All: true}},
		})
	}

	cleaned := false
	return plan, func() error {
		d.mu.Lock()
		defer d.mu.Unlock()
		if cleaned {
			return nil
		}
		cleaned = true
		return cleanupAliases()
	}, nil
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
	for _, candidate := range profileDataItems {
		if candidate.kind != profileDataItemKindDir {
			continue
		}
		itemID := candidate.id
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
