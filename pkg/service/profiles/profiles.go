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
// and limits with no credentials. One profile is active per
// device at a time. Switching via the API enforces an optional per-profile
// PIN; switching by scanning a profile's physical card bypasses the PIN
// (possession of the card is the authorization).
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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
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
)

// Service owns the device's profile lifecycle: CRUD, the active-profile
// state, and PIN-checked switching. All activation paths (API, ZapScript
// card scans, boot restore) go through here.
type Service struct {
	db          *database.Database
	st          *state.State
	now         func() time.Time
	pinAttempts map[string][]time.Time
	mu          syncutil.Mutex
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
// hashing the PIN if one is given. Limit duration strings must already be
// validated by the caller (API layer) or empty.
func (s *Service) Create(params *models.NewProfileParams) (*database.Profile, error) {
	if err := validateLimitDurations(params.DailyLimit, params.SessionLimit); err != nil {
		return nil, err
	}

	p := &database.Profile{
		ProfileID:     uuid.New().String(),
		Name:          params.Name,
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
	if params.ClearLimits {
		p.LimitsEnabled = nil
		p.DailyLimit = nil
		p.SessionLimit = nil
	} else {
		if params.LimitsEnabled != nil {
			p.LimitsEnabled = params.LimitsEnabled
		}
		if params.DailyLimit != nil {
			p.DailyLimit = params.DailyLimit
		}
		if params.SessionLimit != nil {
			p.SessionLimit = params.SessionLimit
		}
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
	if active := s.st.ActiveProfile(); active != nil && active.ProfileID == p.ProfileID {
		s.st.SetActiveProfile(snapshot(p))
	}

	return p, nil
}

// Delete removes a profile. If it is the active profile, the device
// deactivates (the persisted active state is cleared transactionally by
// the database layer).
func (s *Service) Delete(profileID string) error {
	if err := s.db.UserDB.DeleteProfile(profileID); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	if active := s.st.ActiveProfile(); active != nil && active.ProfileID == profileID {
		s.st.SetActiveProfile(nil)
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

// ActivateBySwitchIDChecked switches to a profile selected by switch ID,
// enforcing its PIN. Used when a switch ID arrives over the API rather
// than from a physical scan.
func (s *Service) ActivateBySwitchIDChecked(switchID, pin string) (*models.ActiveProfile, error) {
	p, err := s.db.UserDB.GetProfileBySwitchID(switchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile by switch ID: %w", err)
	}
	if err := s.checkPIN(p, pin); err != nil {
		return nil, err
	}
	return s.activate(p)
}

// ActivateBySwitchID switches to a profile selected by switch ID without a
// PIN check. This is the physical card-scan path: possession of the card
// is the authorization.
func (s *Service) ActivateBySwitchID(switchID string) (*models.ActiveProfile, error) {
	p, err := s.db.UserDB.GetProfileBySwitchID(switchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile by switch ID: %w", err)
	}
	return s.activate(p)
}

// Deactivate clears the active profile. Leaving a profile is always free
// (PINs gate entry only); restricting what a profile-less device can do is
// handled by the require-profile launch setting.
func (s *Service) Deactivate() error {
	if err := s.db.UserDB.DeleteDeviceState(database.DeviceStateKeyActiveProfile); err != nil {
		return fmt.Errorf("failed to clear active profile state: %w", err)
	}
	s.st.SetActiveProfile(nil)
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
	if err := s.db.UserDB.SetDeviceState(database.DeviceStateKeyActiveProfile, p.ProfileID); err != nil {
		return nil, fmt.Errorf("failed to persist active profile: %w", err)
	}
	snap := snapshot(p)
	s.st.SetActiveProfile(snap)
	log.Info().Str("profileId", p.ProfileID).Str("name", p.Name).
		Msg("switched active profile")
	return snap, nil
}

// checkPIN enforces a profile's PIN with per-profile rate limiting. A
// profile without a PIN always passes.
func (s *Service) checkPIN(p *database.Profile, pin string) error {
	if p.PINHash == "" {
		return nil
	}
	if pin == "" {
		return ErrPINRequired
	}

	s.mu.Lock()
	now := s.now()
	attempts := s.pinAttempts[p.ProfileID]
	recent := attempts[:0]
	for _, at := range attempts {
		if now.Sub(at) < pinAttemptWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) >= pinAttemptLimit {
		s.pinAttempts[p.ProfileID] = recent
		s.mu.Unlock()
		return ErrPINRateLimited
	}
	s.mu.Unlock()

	if !VerifyPIN(pin, p.PINHash) {
		s.mu.Lock()
		s.pinAttempts[p.ProfileID] = append(recent, now)
		s.mu.Unlock()
		return ErrPINIncorrect
	}

	s.mu.Lock()
	delete(s.pinAttempts, p.ProfileID)
	s.mu.Unlock()
	return nil
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
		HasPIN:        p.PINHash != "",
		LimitsEnabled: p.LimitsEnabled,
		DailyLimit:    p.DailyLimit,
		SessionLimit:  p.SessionLimit,
	}
}

// validateLimitDurations rejects unparseable limit duration strings. An
// empty string is allowed (it means "clear to inherit" on update).
func validateLimitDurations(durations ...*string) error {
	for _, d := range durations {
		if d == nil || *d == "" {
			continue
		}
		if _, err := time.ParseDuration(*d); err != nil {
			return fmt.Errorf("invalid limit duration %q: %w", *d, err)
		}
	}
	return nil
}
