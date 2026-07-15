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

// Package profiles implements device profiles: named buckets of preferences
// and limits. One profile is active per device at a time; the un-profiled
// state is the implicit "shared profile" — the device as it behaves when
// nobody is signed in, with global-config limits and unattributed history.
//
// A profile's switch ID is a bearer credential: presenting it (by scanning
// the card it is written on, or by knowing its value) authorizes switching
// to that profile with no PIN on every path. Switch IDs are therefore only
// exposed over the API to privileged clients. The optional per-profile PIN
// protects the remaining path: switching by profile ID picked from the
// visible profile list. Leaving a profile is always free — PINs gate entry
// only.
package profiles

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	ProfileRoleAdmin  = "admin"
	ProfileRoleMember = "member"

	// pinAttemptWindow and pinAttemptLimit bound PIN guesses per profile:
	// at most pinAttemptLimit failures within pinAttemptWindow.
	pinAttemptWindow = time.Minute
	pinAttemptLimit  = 5

	// switchIDRetries bounds regeneration attempts on the (vanishingly
	// unlikely) unique-constraint collision of a generated switch ID.
	switchIDRetries = 5
)

var (
	// ErrPINRequired is returned when switching to a PIN-protected profile
	// without supplying a PIN.
	ErrPINRequired = errors.New("profile requires a PIN")
	// ErrPINIncorrect is returned when the supplied PIN does not match.
	ErrPINIncorrect = errors.New("incorrect PIN")
	// ErrPINRateLimited is returned when too many failed PIN attempts have
	// been made against a profile.
	ErrPINRateLimited = errors.New("too many PIN attempts, try again later")
	// ErrNotFound is returned when a profile does not exist.
	ErrNotFound = userdb.ErrProfileNotFound
	// ErrAdminPINRequired is returned when an administrator profile would
	// have no PIN protecting management authorization.
	ErrAdminPINRequired = errors.New("admin profiles require a PIN")
	// ErrLastAdmin is returned when deleting or demoting the final admin.
	ErrLastAdmin = userdb.ErrLastProfileAdmin
	// ErrInvalidRole is returned for unknown profile roles.
	ErrInvalidRole = errors.New("invalid profile role")
)

// Service owns the device's profile lifecycle: CRUD, the active-profile
// state, and PIN-checked switching. All activation paths (API, ZapScript
// card scans, boot restore) go through here.
type Service struct {
	db          *database.Database
	st          *state.State
	dataSwap    *DataSwapCoordinator
	now         func() time.Time
	pinAttempts map[string][]time.Time
	mu          syncutil.Mutex
	// manageMu serializes CRUD/bootstrap decisions around profile roles.
	manageMu syncutil.Mutex
	// activateMu serializes activate/deactivate so the persisted device
	// state and the in-memory snapshot cannot diverge under concurrency.
	activateMu syncutil.Mutex
}

// NewService creates a profiles service backed by the user database and
// service state.
func NewService(db *database.Database, st *state.State) *Service {
	return &Service{
		db:          db,
		st:          st,
		now:         time.Now,
		pinAttempts: make(map[string][]time.Time),
	}
}

// SetDataSwap attaches the data swap coordinator. Optional: without it,
// profile switches change limits and attribution only.
func (s *Service) SetDataSwap(c *DataSwapCoordinator) {
	s.dataSwap = c
}

// ReconcileData re-applies the active profile's data state, e.g. after the
// swap_data setting changes. No-op when data swapping is not wired.
func (s *Service) ReconcileData() {
	if s.dataSwap != nil {
		s.dataSwap.Reconcile()
	}
}

// List returns all profiles.
func (s *Service) List() ([]database.Profile, error) {
	profiles, err := s.db.UserDB.ListProfiles()
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}
	return profiles, nil
}

// Get returns a profile by its profile ID.
func (s *Service) Get(profileID string) (*database.Profile, error) {
	p, err := s.db.UserDB.GetProfile(profileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}
	return p, nil
}

// Create creates a new profile with a generated profile ID and switch ID,
// hashing the PIN if one is given. The first profile is always an explicit
// administrator and therefore must have a PIN; later profiles default member.
func (s *Service) Create(params *models.NewProfileParams) (*database.Profile, error) {
	s.manageMu.Lock()
	defer s.manageMu.Unlock()

	if err := validateLimitDurations(params.DailyLimit, params.SessionLimit); err != nil {
		return nil, err
	}

	list, err := s.List()
	if err != nil {
		return nil, err
	}
	role := params.Role
	if len(list) == 0 {
		role = ProfileRoleAdmin
	} else if role == "" {
		role = ProfileRoleMember
	}
	if role != ProfileRoleAdmin && role != ProfileRoleMember {
		return nil, ErrInvalidRole
	}
	if role == ProfileRoleAdmin && (params.PIN == nil || *params.PIN == "") {
		return nil, ErrAdminPINRequired
	}

	p := &database.Profile{
		ProfileID:     uuid.New().String(),
		Name:          params.Name,
		Role:          role,
		LimitsEnabled: params.LimitsEnabled,
		DailyLimit:    params.DailyLimit,
		SessionLimit:  params.SessionLimit,
		CreatedAt:     s.now().Unix(),
		UpdatedAt:     s.now().Unix(),
	}

	if params.PIN != nil && *params.PIN != "" {
		hash, err := HashPIN(*params.PIN)
		if err != nil {
			return nil, err
		}
		p.PINHash = hash
	}

	if err := s.insertWithSwitchID(p); err != nil {
		return nil, err
	}

	return p, nil
}

// Update applies an update to a profile. If the profile is currently
// active, the in-memory snapshot is refreshed so changed limits apply
// immediately.
func (s *Service) Update(params *models.UpdateProfileParams) (*database.Profile, error) {
	s.manageMu.Lock()
	defer s.manageMu.Unlock()

	if err := validateLimitDurations(params.DailyLimit, params.SessionLimit); err != nil {
		return nil, err
	}

	p, err := s.db.UserDB.GetProfile(params.ProfileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}

	if params.Name != nil {
		p.Name = *params.Name
	}
	if params.Role != nil {
		if *params.Role != ProfileRoleAdmin && *params.Role != ProfileRoleMember {
			return nil, ErrInvalidRole
		}
		p.Role = *params.Role
	}
	switch {
	case params.ClearPIN:
		p.PINHash = ""
	case params.PIN != nil && *params.PIN != "":
		hash, hashErr := HashPIN(*params.PIN)
		if hashErr != nil {
			return nil, hashErr
		}
		p.PINHash = hash
	}
	if p.Role == ProfileRoleAdmin && p.PINHash == "" {
		return nil, ErrAdminPINRequired
	}
	// ClearLimits resets all overrides back to inherit, then any limit
	// fields in the same request are applied on top. This lets a form
	// submit its full desired state in one call: clear-then-set.
	if params.ClearLimits {
		p.LimitsEnabled = nil
		p.DailyLimit = nil
		p.SessionLimit = nil
	}
	if params.LimitsEnabled != nil {
		p.LimitsEnabled = params.LimitsEnabled
	}
	if params.DailyLimit != nil {
		p.DailyLimit = params.DailyLimit
	}
	if params.SessionLimit != nil {
		p.SessionLimit = params.SessionLimit
	}
	p.UpdatedAt = s.now().Unix()

	if params.RegenerateSwitchID {
		if regenErr := s.updateWithNewSwitchID(p); regenErr != nil {
			return nil, regenErr
		}
	} else if updateErr := s.db.UserDB.UpdateProfile(p); updateErr != nil {
		return nil, fmt.Errorf("failed to update profile: %w", updateErr)
	}

	// Refresh the active snapshot if this profile is active so limit
	// changes take effect without a re-switch.
	s.activateMu.Lock()
	if active := s.st.ActiveProfile(); active != nil && active.ProfileID == p.ProfileID {
		s.st.SetActiveProfile(snapshot(p))
	}
	s.activateMu.Unlock()
	return p, nil
}

// Delete removes a profile. If it is the active profile, the device
// deactivates (the persisted active state is cleared transactionally by
// the database layer).
func (s *Service) Delete(profileID string) error {
	s.manageMu.Lock()
	defer s.manageMu.Unlock()
	s.activateMu.Lock()
	defer s.activateMu.Unlock()

	wasActive := false
	if active := s.st.ActiveProfile(); active != nil {
		wasActive = active.ProfileID == profileID
	}
	if err := s.db.UserDB.DeleteProfile(profileID); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	if wasActive {
		s.st.SetActiveProfile(nil)
		if s.dataSwap != nil {
			s.dataSwap.RequestSwitch(platforms.ProfileRef{})
		}
	}
	return nil
}

// ActivateByID switches the device to a profile, enforcing its PIN if one
// is set. This is the API path; card scans use ActivateBySwitchID.
func (s *Service) ActivateByID(profileID, pin string) (*models.ActiveProfile, error) {
	p, err := s.db.UserDB.GetProfile(profileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}
	if err := s.checkPIN(p, pin); err != nil {
		return nil, err
	}
	return s.activate(p)
}

// ActivateBySwitchID switches to a profile selected by switch ID without a
// PIN check. Switch IDs are bearer credentials: possessing the card or
// knowing its content is the authorization, on every path (scan, run API,
// profiles.switch). They are only readable via the API by privileged
// clients.
func (s *Service) ActivateBySwitchID(switchID string) (*models.ActiveProfile, error) {
	p, err := s.db.UserDB.GetProfileBySwitchID(switchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile by switch ID: %w", err)
	}
	return s.activate(p)
}

// VerifyByID checks a profile's PIN without switching to it. It shares the
// PIN rate limiter with activation, so it cannot be used to brute-force a
// PIN any faster than switching attempts could. Clients use this to gate
// their own ad-hoc UI items behind a profile credential; success grants
// nothing server-side.
func (s *Service) VerifyByID(profileID, pin string) (*database.Profile, error) {
	p, err := s.db.UserDB.GetProfile(profileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}
	if err := s.checkPIN(p, pin); err != nil {
		return nil, err
	}
	return p, nil
}

// VerifyBySwitchID resolves a switch ID without switching. The switch ID
// is a bearer credential, so resolving it IS the verification — no PIN.
// Success grants nothing server-side.
func (s *Service) VerifyBySwitchID(switchID string) (*database.Profile, error) {
	p, err := s.db.UserDB.GetProfileBySwitchID(switchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile by switch ID: %w", err)
	}
	return p, nil
}

// Deactivate clears the active profile. Leaving a profile is always free
// (PINs gate entry only); restricting what a profile-less device can do is
// handled by the require-profile launch setting.
func (s *Service) Deactivate() error {
	s.activateMu.Lock()
	defer s.activateMu.Unlock()

	if err := s.db.UserDB.DeleteDeviceState(database.DeviceStateKeyActiveProfile); err != nil {
		return fmt.Errorf("failed to clear active profile state: %w", err)
	}
	s.st.SetActiveProfile(nil)
	if s.dataSwap != nil {
		s.dataSwap.RequestSwitch(platforms.ProfileRef{})
	}
	return nil
}

// Active returns the active profile snapshot, or nil when none is active.
func (s *Service) Active() *models.ActiveProfile {
	return s.st.ActiveProfile()
}

// RestoreOnBoot restores the persisted active profile into service state.
// A dangling reference to a deleted profile is cleaned up silently.
func (s *Service) RestoreOnBoot() error {
	profileID, found, err := s.db.UserDB.GetDeviceState(database.DeviceStateKeyActiveProfile)
	if err != nil {
		return fmt.Errorf("failed to read active profile state: %w", err)
	}
	if !found {
		return nil
	}

	p, err := s.db.UserDB.GetProfile(profileID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			log.Warn().Str("profileId", profileID).
				Msg("persisted active profile no longer exists, clearing")
			if delErr := s.db.UserDB.DeleteDeviceState(database.DeviceStateKeyActiveProfile); delErr != nil {
				return fmt.Errorf("failed to clear dangling active profile state: %w", delErr)
			}
			return nil
		}
		return fmt.Errorf("failed to get persisted active profile: %w", err)
	}

	s.st.SetActiveProfile(snapshot(p))
	log.Info().Str("profileId", p.ProfileID).Str("name", p.Name).
		Msg("restored active profile")
	return nil
}

func (s *Service) activate(p *database.Profile) (*models.ActiveProfile, error) {
	// Serialize activations so two concurrent switches cannot interleave
	// the persisted device state with the in-memory snapshot.
	s.activateMu.Lock()
	defer s.activateMu.Unlock()

	lastUsedAt := s.now().Unix()
	if err := s.db.UserDB.ActivateProfile(p.ProfileID, lastUsedAt); err != nil {
		return nil, fmt.Errorf("failed to persist active profile: %w", err)
	}
	p.LastUsedAt = &lastUsedAt
	snap := snapshot(p)
	s.st.SetActiveProfile(snap)
	log.Info().Str("profileId", p.ProfileID).Str("name", p.Name).
		Msg("switched active profile")
	if s.dataSwap != nil {
		s.dataSwap.RequestSwitch(platforms.ProfileRef{ID: p.ProfileID, Name: p.Name})
	}
	return snap, nil
}

// checkPIN enforces a profile's PIN with per-profile rate limiting. A
// profile without a PIN always passes. The expensive VerifyPIN runs
// outside the mutex; attempt accounting re-reads state under a single lock
// acquisition afterwards so concurrent failures cannot lose records.
func (s *Service) checkPIN(p *database.Profile, pin string) error {
	if p.PINHash == "" {
		return nil
	}
	if pin == "" {
		return ErrPINRequired
	}

	s.mu.Lock()
	now := s.now()
	recent := recentAttempts(s.pinAttempts[p.ProfileID], now)
	s.pinAttempts[p.ProfileID] = recent
	if len(recent) >= pinAttemptLimit {
		s.mu.Unlock()
		return ErrPINRateLimited
	}
	s.mu.Unlock()

	if !VerifyPIN(pin, p.PINHash) {
		s.mu.Lock()
		now = s.now()
		s.pinAttempts[p.ProfileID] = append(recentAttempts(s.pinAttempts[p.ProfileID], now), now)
		s.mu.Unlock()
		return ErrPINIncorrect
	}

	s.mu.Lock()
	delete(s.pinAttempts, p.ProfileID)
	s.mu.Unlock()
	return nil
}

// recentAttempts filters attempt timestamps down to those still inside the
// rate-limit window, returning a fresh slice so callers never alias the
// old backing array.
func recentAttempts(attempts []time.Time, now time.Time) []time.Time {
	recent := make([]time.Time, 0, len(attempts))
	for _, at := range attempts {
		if now.Sub(at) < pinAttemptWindow {
			recent = append(recent, at)
		}
	}
	return recent
}

// insertWithSwitchID inserts a profile, generating a fresh switch ID and
// retrying on the unlikely unique-constraint collision.
func (s *Service) insertWithSwitchID(p *database.Profile) error {
	for range switchIDRetries {
		switchID, err := GenerateSwitchID()
		if err != nil {
			return err
		}
		p.SwitchID = switchID
		err = s.db.UserDB.CreateProfile(p)
		if err == nil {
			return nil
		}
		if !isSwitchIDConflict(err) {
			return fmt.Errorf("failed to create profile: %w", err)
		}
	}
	return errors.New("failed to generate a unique switch ID")
}

// updateWithNewSwitchID updates a profile with a regenerated switch ID,
// retrying on collision.
func (s *Service) updateWithNewSwitchID(p *database.Profile) error {
	for range switchIDRetries {
		switchID, err := GenerateSwitchID()
		if err != nil {
			return err
		}
		p.SwitchID = switchID
		err = s.db.UserDB.UpdateProfile(p)
		if err == nil {
			return nil
		}
		if !isSwitchIDConflict(err) {
			return fmt.Errorf("failed to update profile: %w", err)
		}
	}
	return errors.New("failed to generate a unique switch ID")
}

// isSwitchIDConflict detects a unique-constraint violation on SwitchID.
// SQLite reports these as "UNIQUE constraint failed: Profiles.SwitchID".
func isSwitchIDConflict(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), "UNIQUE") &&
		strings.Contains(err.Error(), "SwitchID")
}

// snapshot builds the in-memory active-profile snapshot from a profile
// row. It carries resolved limit overrides so the playtime hot path never
// touches the database.
func snapshot(p *database.Profile) *models.ActiveProfile {
	return &models.ActiveProfile{
		ProfileID:     p.ProfileID,
		Name:          p.Name,
		Role:          p.Role,
		HasPIN:        p.PINHash != "",
		LimitsEnabled: p.LimitsEnabled,
		DailyLimit:    p.DailyLimit,
		SessionLimit:  p.SessionLimit,
	}
}

// validateLimitDurations rejects unparseable and negative limit duration
// strings. An empty string is allowed (it means "clear to inherit" on
// update); "0" means explicitly unlimited. Negative values are rejected
// rather than silently behaving as "no limit", which would invert the
// intent of whoever typed them.
func validateLimitDurations(durations ...*string) error {
	for _, d := range durations {
		if d == nil || *d == "" {
			continue
		}
		parsed, err := time.ParseDuration(*d)
		if err != nil {
			return fmt.Errorf("invalid limit duration %q: %w", *d, err)
		}
		if parsed < 0 {
			return fmt.Errorf("invalid limit duration %q: must not be negative", *d)
		}
	}
	return nil
}
