//go:build linux && integration

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors
// SPDX-License-Identifier: GPL-3.0-or-later

package mister

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareBackupRealBindMountSmoke(t *testing.T) {
	if os.Getenv("ZAPAROO_TEST_REAL_MOUNTS") != "1" {
		t.Skip("set ZAPAROO_TEST_REAL_MOUNTS=1 in an isolated mount namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("real bind-mount smoke test requires effective root inside mount namespace")
	}

	mounter := sysMounter{}
	mounts, err := mounter.Mounts()
	if err != nil {
		t.Skipf("mount table unavailable: %v", err)
	}
	mediaMounts := mountsAt(mounts, mediaRootPath)
	if len(mediaMounts) == 0 || mediaMounts[len(mediaMounts)-1].FSType != "tmpfs" {
		t.Skip("/media is not backed by isolated tmpfs")
	}
	if len(mountsAt(mounts, misterconfig.SDRootDir)) != 0 {
		t.Skip("MiSTer storage root already has a mount")
	}

	testRoot := t.TempDir()
	storageRoot := misterconfig.SDRootDir
	tempDir := filepath.Join(testRoot, "tmp")
	settings := platforms.Settings{DataDir: filepath.Join(storageRoot, "zaparoo"), TempDir: tempDir}
	fs := afero.NewOsFs()
	require.NoError(t, fs.MkdirAll(storageRoot, 0o750))
	manager := &profileDataManager{
		fs: fs, m: mounter, ledger: loadMountLedger(fs, filepath.Join(testRoot, "mounts.json")),
	}
	for _, item := range allItems() {
		require.NoError(t, fs.MkdirAll(filepath.Join(storageRoot, item), 0o750))
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(storageRoot, item, "shared.txt"), []byte("shared-"+item), 0o600,
		))
	}
	require.NoError(t, manager.apply(kidA(), allItems()))
	t.Cleanup(func() {
		for _, item := range allItems() {
			_ = manager.m.Unmount(filepath.Join(storageRoot, item))
		}
	})
	for _, item := range allItems() {
		require.NoError(t, afero.WriteFile(
			fs, filepath.Join(storageRoot, item, "personal.txt"), []byte("personal-"+item), 0o600,
		))
	}

	plan, cleanup, err := manager.prepareBackup(settings, BackupDefinitions(settings))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	assert.Empty(t, plan.Warnings)
	for _, item := range allItems() {
		// #nosec G304 -- storageRoot is an isolated tmpfs created by the smoke-test runner.
		personal, readErr := afero.ReadFile(fs, filepath.Join(storageRoot, item, "personal.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "personal-"+item, string(personal))
		var alias string
		for _, definition := range plan.Definitions {
			if definition.Category == item && definition.RestoreRoot == item &&
				len(definition.SourceTrustedRoots) == 1 {
				alias = definition.SourceRoot
				break
			}
		}
		require.NotEmpty(t, alias)
		// #nosec G304 -- alias is returned by the backup plan under the isolated test root.
		shared, readErr := afero.ReadFile(fs, filepath.Join(alias, "shared.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "shared-"+item, string(shared))
		_, statErr := fs.Stat(filepath.Join(alias, "personal.txt"))
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
	require.NoError(t, cleanup())
	entries, err := afero.ReadDir(fs, tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
