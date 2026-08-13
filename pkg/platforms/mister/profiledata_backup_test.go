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
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertActiveProfile(t *testing.T, manager *profileDataManager, mounter *fakeMounter, root string) {
	t.Helper()
	for _, item := range allItems() {
		stack := mountsAt(mounter.mounts, filepath.Join(root, item))
		require.NotEmpty(t, stack)
		entry := manager.ledger.find(&stack[len(stack)-1])
		require.NotNil(t, entry)
		assert.Equal(t, kidA().ID, entry.ProfileID)
	}
}

func TestPrepareBackupWithoutActiveProfileUsesStaticLocations(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, fs := newTestManager(mounter)
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	assert.Empty(t, plan.Warnings)
	assert.Equal(t, BackupDefinitions(settings), plan.Definitions)
	assert.Empty(t, mounter.mounts)
	exists, err := afero.Exists(fs, settings.TempDir)
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, cleanup())
}

func TestPrepareBackupAliasesSharedDataAndPreservesActiveProfile(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, fs := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	assert.Empty(t, plan.Warnings)
	assertActiveProfile(t, manager, mounter, misterconfig.SDRootDir)

	for _, item := range allItems() {
		var alias string
		for _, definition := range plan.Definitions {
			if definition.Category == item && definition.RestoreRoot == item &&
				filepath.Dir(definition.SourceRoot) != misterconfig.SDRootDir {
				alias = definition.SourceRoot
				break
			}
		}
		require.NotEmpty(t, alias)
		assert.Len(t, mountsAt(mounter.mounts, alias), 1)
	}

	require.NoError(t, cleanup())
	require.NoError(t, cleanup())
	assertActiveProfile(t, manager, mounter, misterconfig.SDRootDir)
	entries, err := afero.ReadDir(fs, settings.TempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestPrepareBackupWarnsWhenMountStateIsUnavailable(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{mountsErr: errors.New("mount table unavailable")}
	manager, _ := newTestManager(mounter)
	settings := platforms.Settings{DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo")}
	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)

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
	require.NoError(t, cleanup())
}

func TestPrepareBackupFailsWhenTempRootCannotBeCreated(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}
	manager.fs = failMkdirFS{Fs: manager.fs, path: settings.TempDir}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.ErrorContains(t, err, "creating profile backup temp root")
	assert.Empty(t, plan)
	assert.Nil(t, cleanup)
	assertActiveProfile(t, manager, mounter, misterconfig.SDRootDir)
}

func TestPrepareBackupCleanupReportsUnmountFailure(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, fs := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	_, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	mounter.unmountErr = errors.New("injected cleanup unmount failure")
	err = cleanup()
	require.ErrorContains(t, err, "unmounting profile backup alias")
	require.NoError(t, cleanup(), "cleanup remains idempotent after reporting an error")
	entries, readErr := afero.ReadDir(fs, settings.TempDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestPrepareBackupAliasesUnderlyingNASData(t *testing.T) {
	t.Parallel()
	savesTarget := filepath.Join(misterconfig.SDRootDir, profileDataItemSaves)
	statesTarget := filepath.Join(misterconfig.SDRootDir, profileDataItemSavestates)
	mounter := &fakeMounter{mounts: []mountEntry{
		{Root: "/", Mountpoint: savesTarget, FSType: "cifs", Source: "//nas/saves"},
		{Root: "/", Mountpoint: statesTarget, FSType: "cifs", Source: "//nas/states"},
	}}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	assert.Empty(t, plan.Warnings)
	assertActiveProfile(t, manager, mounter, misterconfig.SDRootDir)
	for _, item := range allItems() {
		var aliasDefinition platforms.BackupDefinition
		for _, definition := range plan.Definitions {
			if definition.Category == item && definition.RestoreRoot == item &&
				filepath.Dir(definition.SourceRoot) != misterconfig.SDRootDir {
				aliasDefinition = definition
				break
			}
		}
		require.NotEmpty(t, aliasDefinition.SourceRoot)
		aliasMounts := mountsAt(mounter.mounts, aliasDefinition.SourceRoot)
		require.Len(t, aliasMounts, 1)
		assert.Equal(t, "cifs", aliasMounts[0].FSType)
	}
	require.NoError(t, cleanup())
}

func TestPrepareBackupUsesUSBStorageRoot(t *testing.T) {
	t.Parallel()
	usbRoot := filepath.Join(mediaRootPath, "usb1")
	mounter := &fakeMounter{mounts: []mountEntry{
		{Root: "/", Mountpoint: usbRoot, FSType: "vfat", Source: "/dev/sdb1"},
	}}
	manager, fs := newTestManager(mounter)
	writeDeviceBin(t, fs, 1)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	assert.Empty(t, plan.Warnings)
	assertActiveProfile(t, manager, mounter, usbRoot)
	assert.Contains(t, plan.Definitions, platforms.BackupDefinition{
		Category: profileDataItemSaves, SourceRoot: filepath.Join(usbRoot, "zaparoo", "profiles"),
		RestoreRoot: filepath.Join("zaparoo", "profiles"),
		Include: []platforms.BackupPattern{
			{Contains: "/" + profileDataItemSaves + "/"},
			{Glob: profileNameFile},
		},
	})
	for _, item := range allItems() {
		if item == profileDataItemSaves {
			continue
		}
		assert.Contains(t, plan.Definitions, platforms.BackupDefinition{
			Category: item, SourceRoot: filepath.Join(usbRoot, "zaparoo", "profiles"),
			RestoreRoot: filepath.Join("zaparoo", "profiles"),
			Include:     []platforms.BackupPattern{{Contains: "/" + item + "/"}},
		})
	}
	require.NoError(t, cleanup())
}

func TestPrepareBackupWarnsWhenSharedAliasCannotBeMounted(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, _ := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	mounter.bindErr = errors.New("injected alias bind failure")
	mounter.failBindAtTry = mounter.bindAttempts + 1
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	assert.Contains(t, plan.Warnings, platforms.BackupWarning{
		Category: profileDataItemSaves, Path: profileDataItemSaves,
		Reason: "shared profile data unavailable during backup",
	})
	assert.Contains(t, plan.Definitions, platforms.BackupDefinition{
		Category:    profileDataItemSaves,
		SourceRoot:  filepath.Join(misterconfig.SDRootDir, profileDataItemSaves),
		RestoreRoot: profileDataItemSaves,
		Include:     []platforms.BackupPattern{{All: true}},
	})
	assertActiveProfile(t, manager, mounter, misterconfig.SDRootDir)
	require.NoError(t, cleanup())
}

func TestPrepareBackupFailsWhenProfileMountCannotBeRestored(t *testing.T) {
	t.Parallel()
	mounter := &fakeMounter{}
	manager, fs := newTestManager(mounter)
	require.NoError(t, manager.apply(kidA(), allItems()))
	settings := platforms.Settings{
		DataDir: filepath.Join(misterconfig.SDRootDir, "zaparoo"),
		TempDir: filepath.Join(string(filepath.Separator), "tmp", "zaparoo"),
	}
	mounter.bindErr = errors.New("injected restore bind failure")
	mounter.failBindAtTry = mounter.bindAttempts + 2

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.Error(t, err)
	assert.Empty(t, plan)
	assert.Nil(t, cleanup)
	require.ErrorContains(t, err, "restoring saves profile mount after preparing backup")
	exists, existsErr := afero.Exists(fs, settings.TempDir)
	require.NoError(t, existsErr)
	if exists {
		entries, readErr := afero.ReadDir(fs, settings.TempDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries)
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
