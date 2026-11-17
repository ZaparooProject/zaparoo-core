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

package methods

import (
	"errors"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/rs/zerolog/log"
)

var errKioskUnavailable = errors.New("kiosk mode is not available")

// requireKioskAdmin passes a request that holds admin authority right now:
// the device itself or an API key while the lock is not enforcing, or a
// client paired as admin at any time. A capped caller never passes.
func requireKioskAdmin(env *requests.RequestEnv) error {
	if requestGrant(env).EffectiveRole() != permissions.RoleAdmin {
		return models.ClientErrf("%w", ErrForbidden)
	}
	return nil
}

func kioskError(err error) error {
	switch {
	case errors.Is(err, kiosk.ErrNotEnabled),
		errors.Is(err, kiosk.ErrAlreadyEnabled),
		errors.Is(err, kiosk.ErrNoAdminProfile),
		errors.Is(err, kiosk.ErrNoAdminClient),
		errors.Is(err, kiosk.ErrNothingAllowed),
		errors.Is(err, kiosk.ErrInvalidSettings):
		return models.ClientErrf("%w", err)
	default:
		return err
	}
}

func kioskSettingsModel(s *kiosk.Settings) *models.KioskSettings {
	return &models.KioskSettings{
		Preset:             s.Preset,
		DefaultContentMode: s.DefaultContentMode,
		Downloads:          s.Downloads,
		IdleTimeout:        s.IdleTimeout,
		UnlockDuration:     s.UnlockDuration,
		DeviceUnlock:       s.DeviceUnlock,
		ProfileSwitching:   s.ProfileSwitching,
		ExtensionCard:      s.ExtensionCard,
		IdleReset:          s.IdleReset,
	}
}

func applyKioskPatch(s *kiosk.Settings, patch *models.KioskSettingsPatch) {
	if patch == nil {
		return
	}
	if patch.DefaultContentMode != nil {
		s.DefaultContentMode = *patch.DefaultContentMode
	}
	if patch.Downloads != nil {
		s.Downloads = *patch.Downloads
	}
	if patch.IdleTimeout != nil {
		s.IdleTimeout = *patch.IdleTimeout
	}
	if patch.UnlockDuration != nil {
		s.UnlockDuration = *patch.UnlockDuration
	}
	if patch.DeviceUnlock != nil {
		s.DeviceUnlock = *patch.DeviceUnlock
	}
	if patch.ProfileSwitching != nil {
		s.ProfileSwitching = *patch.ProfileSwitching
	}
	if patch.ExtensionCard != nil {
		s.ExtensionCard = *patch.ExtensionCard
	}
	if patch.IdleReset != nil {
		s.IdleReset = *patch.IdleReset
	}
}

// KioskStatusResponse builds the lock as clients see it: the service's
// snapshot plus how far this platform's own interface is closed.
func KioskStatusResponse(pl platforms.Platform, status *kiosk.Status) models.KioskResponse {
	resp := models.KioskResponse{
		State: string(status.State),
		Seal:  models.KioskSeal{Unsealed: []string{}},
	}
	if !status.Active() {
		// Nothing is locked, so nothing is unsealed either.
		resp.Seal.Sealed = false
		return resp
	}
	resp.Settings = kioskSettingsModel(&status.Settings)
	if status.State == kiosk.StateUnlocked && !status.UnlockUntil.IsZero() {
		until := status.UnlockUntil.UTC().Format(time.RFC3339)
		resp.UnlockUntil = &until
	}
	unsealed := []string{}
	if pl != nil {
		unsealed = append(unsealed, platforms.KioskUnsealed(pl)...)
		if status.Settings.IdleReset && !kiosk.IdleResetSupported(pl) {
			unsealed = append(unsealed, platforms.KioskUnsealedIdleReset)
		}
	}
	resp.Seal.Unsealed = unsealed
	resp.Seal.Sealed = len(unsealed) == 0
	return resp
}

// NotifyKioskState publishes the lock's current state.
func NotifyKioskState(ns chan<- models.Notification, pl platforms.Platform, status *kiosk.Status) {
	payload := KioskStatusResponse(pl, status)
	notifications.KioskState(ns, &payload)
}

func kioskResponse(env *requests.RequestEnv) models.KioskResponse {
	status := env.Kiosk.Status()
	return KioskStatusResponse(env.Platform, &status)
}

// HandleKiosk returns the kiosk lock's state, settings and seal status.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKiosk(env requests.RequestEnv) (any, error) {
	return kioskResponse(&env), nil
}

// HandleKioskEnable turns kiosk mode on, locked, from a preset.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskEnable(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	var params models.KioskEnableParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	settings, ok := kiosk.PresetSettings(params.Preset)
	if !ok {
		return nil, models.ClientErrf("unknown preset: %q", params.Preset)
	}
	applyKioskPatch(&settings, params.Settings)

	log.Info().Str("preset", params.Preset).Msg("received kiosk enable request")
	if err := env.Kiosk.Enable(&settings, kiosk.EnableOptions{ConfirmEmpty: params.ConfirmEmpty}); err != nil {
		return nil, kioskError(err)
	}
	return kioskResponse(&env), nil
}

// HandleKioskUpdate changes the settings of an enabled lock.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskUpdate(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	var params models.KioskUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	settings := env.Kiosk.Status().Settings
	applyKioskPatch(&settings, &params.Settings)
	if err := env.Kiosk.UpdateSettings(&settings); err != nil {
		return nil, kioskError(err)
	}
	return kioskResponse(&env), nil
}

// HandleKioskDisable turns kiosk mode off.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskDisable(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	log.Info().Msg("received kiosk disable request")
	if err := env.Kiosk.Disable(); err != nil {
		return nil, kioskError(err)
	}
	return kioskResponse(&env), nil
}

// HandleKioskUnlock lifts the lock for a while. It is the one way in for a
// capped caller, so it decides for itself who may ask:
//
//   - a client paired as admin, with nothing more to show;
//   - a caller on the device with the recovery token, which proves access
//     to the device's own files;
//   - a caller on the device with an admin profile's PIN, when on-device
//     unlock is on.
//
// A PIN is never accepted from the network. A paired member could otherwise
// work through a four-digit PIN at the rate limiter's pace from a script.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskUnlock(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	var params models.KioskUnlockParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	if err := authorizeKioskUnlock(&env, &params); err != nil {
		return nil, err
	}
	var duration time.Duration
	if params.Duration != nil {
		duration = time.Duration(*params.Duration) * time.Second
	}
	log.Info().Msg("received kiosk unlock request")
	if err := env.Kiosk.Unlock(duration); err != nil {
		return nil, kioskError(err)
	}
	return kioskResponse(&env), nil
}

func authorizeKioskUnlock(env *requests.RequestEnv, params *models.KioskUnlockParams) error {
	if requestGrant(env).EffectiveRole() == permissions.RoleAdmin {
		return nil
	}
	if !env.IsLocal {
		return models.ClientErrf("%w", ErrForbidden)
	}
	if params.RecoveryToken != nil && *params.RecoveryToken != "" {
		if env.Kiosk.CheckRecoveryToken(*params.RecoveryToken) {
			return nil
		}
		return models.ClientErrf("%w", ErrForbidden)
	}
	if !env.Kiosk.Status().Settings.DeviceUnlock {
		return models.ClientErrf("%w", ErrKioskLocked)
	}
	if env.Profiles == nil {
		return errProfilesUnavailable
	}
	if params.ProfileID == nil || *params.ProfileID == "" {
		return models.ClientErrf("profileId is required")
	}
	// The role is checked before the PIN so that a member profile's PIN,
	// which its owner knows, is never even tried.
	profile, err := env.Profiles.Get(*params.ProfileID)
	if err != nil {
		return profileError(err)
	}
	if profile.Role != profiles.ProfileRoleAdmin {
		return models.ClientErrf("%w", ErrForbidden)
	}
	pin := ""
	if params.PIN != nil {
		pin = *params.PIN
	}
	if pin == "" {
		return models.ClientErrf("%w", profiles.ErrPINRequired)
	}
	if _, err := env.Profiles.VerifyByID(*params.ProfileID, pin); err != nil {
		return profileError(err)
	}
	return nil
}

// HandleKioskLock ends a temporary unlock early. Locking only ever narrows
// what the device allows, so any caller the method gate lets through may
// ask for it.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskLock(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	if err := requireAuthenticated(&env); err != nil {
		return nil, err
	}
	if err := env.Kiosk.Lock(); err != nil {
		return nil, kioskError(err)
	}
	return kioskResponse(&env), nil
}

// HandleKioskAllow approves the next launch of one media path that the
// content rule would refuse. The device stays locked.
//
//nolint:gocritic // single-use parameter in API handler
func HandleKioskAllow(env requests.RequestEnv) (any, error) {
	if env.Kiosk == nil {
		return nil, errKioskUnavailable
	}
	var params models.KioskAllowParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	if !env.Kiosk.Active() {
		return nil, kioskError(kiosk.ErrNotEnabled)
	}
	env.Kiosk.AllowOnce(params.SystemID, params.Path)
	return NoContent{}, nil
}

func contentProfileExists(env *requests.RequestEnv, profileID string) error {
	if profileID == "" {
		return nil
	}
	if env.Profiles == nil {
		return errProfilesUnavailable
	}
	if _, err := env.Profiles.Get(profileID); err != nil {
		return profileError(err)
	}
	return nil
}

func contentResponse(env *requests.RequestEnv, profileID string) (any, error) {
	rule, err := env.Database.UserDB.GetContentRule(profileID)
	if err != nil {
		log.Error().Err(err).Msg("error reading content rule")
		return nil, errors.New("error reading content rule")
	}
	resp := models.ProfilesContentResponse{
		ProfileID:     profileID,
		Mode:          rule.Mode,
		EffectiveMode: rule.Mode,
		Entries:       make([]models.ContentRuleEntry, 0, len(rule.Entries)),
	}
	if resp.EffectiveMode == "" {
		resp.EffectiveMode = env.Kiosk.Status().Settings.DefaultContentMode
	}
	for i := range rule.Entries {
		entry := &rule.Entries[i]
		resp.Entries = append(resp.Entries, models.ContentRuleEntry{
			Effect: entry.Effect, Kind: entry.Kind,
			SystemID: entry.SystemID, Path: entry.Path, Value: entry.Value,
		})
	}
	return resp, nil
}

// HandleProfilesContent returns what a profile may launch while the device
// is locked.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesContent(env requests.RequestEnv) (any, error) {
	var params models.ProfilesContentParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	if err := contentProfileExists(&env, params.ProfileID); err != nil {
		return nil, err
	}
	return contentResponse(&env, params.ProfileID)
}

// HandleProfilesContentUpdate replaces a profile's whole content rule.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesContentUpdate(env requests.RequestEnv) (any, error) {
	var params models.ProfilesContentUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := requireKioskAdmin(&env); err != nil {
		return nil, err
	}
	if err := contentProfileExists(&env, params.ProfileID); err != nil {
		return nil, err
	}

	rule := database.ContentRule{
		Mode:    params.Mode,
		Entries: make([]database.ContentRuleEntry, 0, len(params.Entries)),
	}
	for i := range params.Entries {
		entry, err := contentEntry(&params.Entries[i])
		if err != nil {
			return nil, err
		}
		rule.Entries = append(rule.Entries, entry)
	}
	if err := env.Database.UserDB.ReplaceContentRule(params.ProfileID, &rule); err != nil {
		log.Error().Err(err).Msg("error storing content rule")
		return nil, errors.New("error storing content rule")
	}
	return contentResponse(&env, params.ProfileID)
}

// contentEntry checks that an entry carries what its kind needs and nothing
// that would silently never match.
func contentEntry(in *models.ContentRuleEntry) (database.ContentRuleEntry, error) {
	entry := database.ContentRuleEntry{Effect: in.Effect, Kind: in.Kind}
	systemID := strings.TrimSpace(in.SystemID)
	if in.Kind == database.ContentKindSystem || in.Kind == database.ContentKindMedia {
		system, err := systemdefs.LookupSystem(systemID)
		if err != nil {
			return entry, models.ClientErrf("unknown system: %q", in.SystemID)
		}
		entry.SystemID = system.ID
	}
	switch in.Kind {
	case database.ContentKindSystem:
	case database.ContentKindMedia:
		if strings.TrimSpace(in.Path) == "" {
			return entry, models.ClientErrf("a media entry needs a path")
		}
		entry.Path = in.Path
	case database.ContentKindDeck:
		if strings.TrimSpace(in.Value) == "" {
			return entry, models.ClientErrf("a deck entry needs a deck ID in value")
		}
		entry.Value = strings.TrimSpace(in.Value)
	case database.ContentKindTags:
		parsed, err := filters.ParseTagFilters(strings.Split(in.Value, ","))
		if err != nil || len(parsed) == 0 {
			return entry, models.ClientErrf("a tags entry needs a tag filter in value")
		}
		entry.Value = in.Value
	default:
		return entry, models.ClientErrf("unknown entry kind: %q", in.Kind)
	}
	return entry, nil
}

// kioskScope returns the system scope a capped caller's browsing is held
// to, and whether the caller is capped at all. The scope follows the active
// profile, the way launches do.
func kioskScope(env *requests.RequestEnv) (kiosk.SystemScope, bool) {
	if !requestGrant(env).Capped() {
		return kiosk.SystemScope{}, false
	}
	profileID := ""
	if env.State != nil {
		if active := env.State.ActiveProfile(); active != nil {
			profileID = active.ProfileID
		}
	}
	return env.Kiosk.SystemScope(profileID), true
}

// kioskNarrowSystems holds the systems a capped caller asked to browse or
// search to the ones its profile may see. Asking for nothing means every
// system, so a limited scope always comes back as an explicit list. A
// request left with no system at all is refused, since an empty list would
// read as every system further down.
func kioskNarrowSystems(env *requests.RequestEnv, requested *[]string) (*[]string, error) {
	scope, capped := kioskScope(env)
	if !capped || !scope.Limited() {
		return requested, nil
	}
	var candidates []string
	switch {
	case requested != nil && len(*requested) > 0:
		candidates = *requested
	case scope.Allowed != nil:
		candidates = scope.Allowed
	default:
		all := systemdefs.AllSystems()
		candidates = make([]string, 0, len(all))
		for i := range all {
			candidates = append(candidates, all[i].ID)
		}
	}
	narrowed := make([]string, 0, len(candidates))
	for _, id := range candidates {
		canonical := id
		if system, err := systemdefs.LookupSystem(id); err == nil {
			canonical = system.ID
		}
		if scope.Permits(canonical) {
			narrowed = append(narrowed, id)
		}
	}
	if len(narrowed) == 0 {
		return nil, models.ClientErrf("%w", ErrKioskLocked)
	}
	return &narrowed, nil
}
