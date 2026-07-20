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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileBackupPlanOmitsLiveAliasAndIncludesProfilePools(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo")}
	plan := manager.backupPlan(settings, BackupDefinitions(settings))

	for _, definition := range plan.Definitions {
		assert.False(t, definition.Category == profileDataItemSaves &&
			filepath.Clean(definition.SourceRoot) == filepath.Join(misterconfig.SDRootDir, "saves") &&
			filepath.Clean(definition.RestoreRoot) == profileDataItemSaves)
	}
	assert.Contains(t, plan.Warnings, platforms.BackupWarning{
		Category: profileDataItemSaves, Path: profileDataItemSaves,
		Reason: "shared profile data hidden by active profile mount",
	})
	assert.Contains(t, plan.Warnings, platforms.BackupWarning{
		Category: profileDataItemSavestates, Path: profileDataItemSavestates,
		Reason: "shared profile data hidden by active profile mount",
	})
}

func TestProfileBackupPlanWarnsWhenMountStateIsUnavailable(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{mountsErr: errors.New("mount table unavailable")}
	manager, _ := newTestManager(mounter)
	settings := platforms.Settings{DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo")}
	plan := manager.backupPlan(settings, BackupDefinitions(settings))

	for _, item := range allItems() {
		assert.Contains(t, plan.Warnings, platforms.BackupWarning{
			Category: item, Path: item, Reason: "profile mount state unavailable",
		})
		for _, definition := range plan.Definitions {
			assert.False(t,
				definition.Category == item &&
					filepath.Clean(definition.SourceRoot) == filepath.Join(misterconfig.SDRootDir, item) &&
					filepath.Clean(definition.RestoreRoot) == item,
				"live %s alias must be omitted when active mount state is unknown", item,
			)
		}
	}
}

func TestProfileBackupPlanMapsActiveNASPoolToOwnedPath(t *testing.T) {
	t.Parallel()
	savesTarget := filepath.Join(misterconfig.SDRootDir, profileDataItemSaves)
	statesTarget := filepath.Join(misterconfig.SDRootDir, profileDataItemSavestates)
	mounter := &fakeMounter{mounts: []mountEntry{
		{Root: "/", Mountpoint: savesTarget, FSType: "cifs", Source: "//nas/saves"},
		{Root: "/", Mountpoint: statesTarget, FSType: "cifs", Source: "//nas/states"},
	}}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo")}
	plan := manager.backupPlan(settings, BackupDefinitions(settings))

	for _, item := range allItems() {
		expectedRestoreRoot := filepath.Join(item, nasPoolDirName, kidA().ID, item)
		assert.Contains(t, plan.Definitions, platforms.BackupDefinition{
			Category: item, SourceRoot: filepath.Join(misterconfig.SDRootDir, item),
			RestoreRoot:        expectedRestoreRoot,
			SourceTrustedRoots: []string{filepath.Join(misterconfig.SDRootDir, item)},
			Include:            []platforms.BackupPattern{{All: true}},
		})
	}
}

func TestPrepareBackupRestoreUnmountsAndRestoresProfileBinds(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))

	finish, err := manager.prepareBackupRestore()
	require.NoError(t, err)
	for _, item := range allItems() {
		stack := mountsAt(mounter.mounts, filepath.Join(misterconfig.SDRootDir, item))
		assert.Empty(t, stack)
	}
	require.NoError(t, finish(false))
	for _, item := range allItems() {
		stack := mountsAt(mounter.mounts, filepath.Join(misterconfig.SDRootDir, item))
		require.NotEmpty(t, stack)
		entry := manager.ledger.find(&stack[len(stack)-1])
		require.NotNil(t, entry)
		assert.Equal(t, kidA().ID, entry.ProfileID)
	}
}

func TestPrepareBackupRestoreFailsWhenMountStateIsUnavailable(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{mountsErr: errors.New("mount table unavailable")}
	manager, _ := newTestManager(mounter)

	finish, err := manager.prepareBackupRestore()
	require.Error(t, err)
	assert.Nil(t, finish)
	assert.Contains(t, err.Error(), "reading profile mounts before backup restore")
}

func TestPrepareBackupRestoreLeavesBindsUnmountedAfterSuccess(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))

	finish, err := manager.prepareBackupRestore()
	require.NoError(t, err)
	require.NoError(t, finish(true))
	for _, item := range allItems() {
		assert.Empty(t, mountsAt(mounter.mounts, filepath.Join(misterconfig.SDRootDir, item)))
	}
}
