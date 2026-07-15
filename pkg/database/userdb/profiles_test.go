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

package userdb

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProfile(profileID, switchID string) *database.Profile {
	return &database.Profile{
		ProfileID: profileID,
		Name:      "Test Profile",
		SwitchID:  switchID,
		CreatedAt: 1700000000,
		UpdatedAt: 1700000000,
	}
}

func TestProfiles_CRUD_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	p := newTestProfile("profile-uuid-1", "corn-arm-truck")
	limitsEnabled := true
	daily := "2h30m"
	p.LimitsEnabled = &limitsEnabled
	p.DailyLimit = &daily
	p.PINHash = "fake-pin-hash"

	require.NoError(t, db.CreateProfile(p))
	assert.Positive(t, p.DBID)
	assert.Equal(t, "admin", p.Role, "first profile is atomically assigned admin")

	got, err := db.GetProfile("profile-uuid-1")
	require.NoError(t, err)
	assert.Equal(t, "Test Profile", got.Name)
	assert.Equal(t, "corn-arm-truck", got.SwitchID)
	assert.Equal(t, "fake-pin-hash", got.PINHash)
	require.NotNil(t, got.LimitsEnabled)
	assert.True(t, *got.LimitsEnabled)
	require.NotNil(t, got.DailyLimit)
	assert.Equal(t, "2h30m", *got.DailyLimit)
	assert.Nil(t, got.SessionLimit)

	bySwitch, err := db.GetProfileBySwitchID("corn-arm-truck")
	require.NoError(t, err)
	assert.Equal(t, "profile-uuid-1", bySwitch.ProfileID)

	list, err := db.ListProfiles()
	require.NoError(t, err)
	require.Len(t, list, 1)

	got.Name = "Renamed"
	got.PINHash = ""
	got.LimitsEnabled = nil
	got.DailyLimit = nil
	got.UpdatedAt = 1700000100
	require.NoError(t, db.UpdateProfile(got))

	updated, err := db.GetProfile("profile-uuid-1")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Empty(t, updated.PINHash)
	assert.Nil(t, updated.LimitsEnabled)
	assert.Nil(t, updated.DailyLimit)

	backupAdmin := newTestProfile("profile-uuid-2", "blue-fox-lamp")
	backupAdmin.Role = "admin"
	require.NoError(t, db.CreateProfile(backupAdmin))
	require.NoError(t, db.DeleteProfile("profile-uuid-1"))
	_, err = db.GetProfile("profile-uuid-1")
	require.ErrorIs(t, err, ErrProfileNotFound)
}

func TestProfiles_ActivateTracksLastUsed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	profile := newTestProfile("p1", "switch-one")
	require.NoError(t, db.CreateProfile(profile))
	got, err := db.GetProfile(profile.ProfileID)
	require.NoError(t, err)
	assert.Nil(t, got.LastUsedAt)

	const usedAt int64 = 1784079000
	require.NoError(t, db.ActivateProfile(profile.ProfileID, usedAt))
	got, err = db.GetProfile(profile.ProfileID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	assert.Equal(t, usedAt, *got.LastUsedAt)

	activeID, found, err := db.GetDeviceState(database.DeviceStateKeyActiveProfile)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, profile.ProfileID, activeID)

	// Ordinary profile edits must not overwrite usage tracking with stale data.
	profile.Name = "Renamed"
	require.NoError(t, db.UpdateProfile(profile))
	got, err = db.GetProfile(profile.ProfileID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	assert.Equal(t, usedAt, *got.LastUsedAt)
}

func TestProfiles_NotFoundErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	_, err := db.GetProfile("missing")
	require.ErrorIs(t, err, ErrProfileNotFound)

	_, err = db.GetProfileBySwitchID("missing-switch")
	require.ErrorIs(t, err, ErrProfileNotFound)

	err = db.UpdateProfile(newTestProfile("missing", "a-b-c"))
	require.ErrorIs(t, err, ErrProfileNotFound)

	err = db.ActivateProfile("missing", 1700000000)
	require.ErrorIs(t, err, ErrProfileNotFound)

	err = db.DeleteProfile("missing")
	require.ErrorIs(t, err, ErrProfileNotFound)
}

func TestProfiles_LastAdminProtected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	admin := newTestProfile("admin", "admin-switch")
	require.NoError(t, db.CreateProfile(admin))
	require.Equal(t, "admin", admin.Role)

	err := db.DeleteProfile(admin.ProfileID)
	require.ErrorIs(t, err, ErrLastProfileAdmin)

	admin.Role = "member"
	err = db.UpdateProfile(admin)
	require.ErrorIs(t, err, ErrLastProfileAdmin)
}

func TestProfiles_SwitchIDUnique(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, db.CreateProfile(newTestProfile("p1", "same-switch-id")))
	err := db.CreateProfile(newTestProfile("p2", "same-switch-id"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE")
}

func TestProfiles_DeleteClearsActiveState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	require.NoError(t, db.CreateProfile(newTestProfile("p1", "switch-one")))
	p2 := newTestProfile("p2", "switch-two")
	p2.Role = "admin"
	require.NoError(t, db.CreateProfile(p2))
	require.NoError(t, db.SetDeviceState(database.DeviceStateKeyActiveProfile, "p1"))

	// Deleting a non-active profile keeps the active state.
	require.NoError(t, db.DeleteProfile("p2"))
	value, found, err := db.GetDeviceState(database.DeviceStateKeyActiveProfile)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "p1", value)

	// Keep a second administrator before deleting the active admin.
	p3 := newTestProfile("p3", "switch-three")
	p3.Role = "admin"
	require.NoError(t, db.CreateProfile(p3))

	// Deleting the active profile clears it.
	require.NoError(t, db.DeleteProfile("p1"))
	_, found, err = db.GetDeviceState(database.DeviceStateKeyActiveProfile)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestDeviceState_SetGetDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	_, found, err := db.GetDeviceState("some_key")
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, db.SetDeviceState("some_key", "value1"))
	value, found, err := db.GetDeviceState("some_key")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value1", value)

	// Upsert overwrites.
	require.NoError(t, db.SetDeviceState("some_key", "value2"))
	value, found, err = db.GetDeviceState("some_key")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value2", value)

	require.NoError(t, db.DeleteDeviceState("some_key"))
	_, found, err = db.GetDeviceState("some_key")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestMediaHistory_ProfileAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	now := time.Now()
	year, month, day := now.Date()
	dayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	startTime := dayStart.Add(1 * time.Hour)
	profileID := "profile-uuid-1"

	attributedEnd := startTime.Add(120 * time.Second)
	attributed := &database.MediaHistoryEntry{
		StartTime:  startTime,
		EndTime:    &attributedEnd,
		SystemID:   "SNES",
		SystemName: "Super Nintendo",
		MediaPath:  "snes/game.sfc",
		MediaName:  "Game",
		LauncherID: "test",
		PlayTime:   120,
		CreatedAt:  startTime,
		UpdatedAt:  startTime,
		ProfileID:  &profileID,
	}
	dbid, err := db.AddMediaHistory(attributed)
	require.NoError(t, err)
	require.NoError(t, db.CloseMediaHistory(dbid, attributedEnd, 120))

	unattributedEnd := startTime.Add(60 * time.Second)
	unattributed := &database.MediaHistoryEntry{
		StartTime:  startTime,
		EndTime:    &unattributedEnd,
		SystemID:   "NES",
		SystemName: "Nintendo",
		MediaPath:  "nes/other.nes",
		MediaName:  "Other",
		LauncherID: "test",
		PlayTime:   60,
		CreatedAt:  startTime,
		UpdatedAt:  startTime,
	}
	dbid, err = db.AddMediaHistory(unattributed)
	require.NoError(t, err)
	require.NoError(t, db.CloseMediaHistory(dbid, unattributedEnd, 60))

	// Rows carry their attribution.
	all, err := db.GetMediaHistory(nil, 0, 10)
	require.NoError(t, err)
	require.Len(t, all, 2)
	for i := range all {
		if all[i].SystemID == "SNES" {
			require.NotNil(t, all[i].ProfileID)
			assert.Equal(t, profileID, *all[i].ProfileID)
		} else {
			assert.Nil(t, all[i].ProfileID)
		}
	}

	// Profile-scoped daily sum counts only the attributed session.
	scoped, err := db.SumMediaPlayTimeForDayByProfile(dayStart, profileID)
	require.NoError(t, err)
	assert.Equal(t, int64(120), scoped)

	// Unscoped daily sum counts everything (shared-profile / device-level
	// accounting).
	total, err := db.SumMediaPlayTimeForDay(dayStart)
	require.NoError(t, err)
	assert.Equal(t, int64(180), total)

	// Unknown profile matches nothing.
	none, err := db.SumMediaPlayTimeForDayByProfile(dayStart, "unknown")
	require.NoError(t, err)
	assert.Equal(t, int64(0), none)
}
