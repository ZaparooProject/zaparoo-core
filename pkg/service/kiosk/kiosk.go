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

// Package kiosk owns the device's kiosk lock: whether the device is locked,
// the settings that shape the lock, and what a profile may launch while it
// holds. Every surface that enforces the lock asks this service; none of
// them keeps its own copy of the answer.
package kiosk

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// State is the lock's position.
type State string

const (
	// StateOff is kiosk mode turned off: nothing in this package applies.
	StateOff State = "off"
	// StateLocked is kiosk mode on and enforcing.
	StateLocked State = "locked"
	// StateUnlocked is kiosk mode on with enforcement lifted until a
	// deadline, for an admin to change something.
	StateUnlocked State = "unlocked"
)

// Preset names. A preset only fills in defaults; every setting it chooses
// can be changed afterwards.
const (
	PresetFamily = "family"
	PresetPublic = "public"
)

// DownloadsNone refuses every media download a card asks for while locked.
// It is the only mode today; the setting is a name so that others can be
// added without changing what is stored.
const DownloadsNone = "none"

const (
	persistVersion = 1

	defaultUnlockDuration = 5 * time.Minute
	minUnlockDuration     = 30 * time.Second
	maxUnlockDuration     = time.Hour
	defaultIdleTimeout    = 10 * time.Minute
	minIdleTimeout        = 30 * time.Second

	// approvalTTL is how long a one-time approval waits for its launch.
	approvalTTL = 5 * time.Minute

	// profileRoleAdmin is the stored role of an admin profile. The profiles
	// service owns the value; it is repeated here because that service
	// depends on device state, which holds this one.
	profileRoleAdmin = "admin"

	recoveryTokenFile  = "kiosk-recovery-token"
	recoveryTokenBytes = 32
)

var (
	// ErrNotEnabled is returned by a transition that needs kiosk mode on.
	ErrNotEnabled = errors.New("kiosk mode is not enabled")
	// ErrAlreadyEnabled is returned by Enable while kiosk mode is on.
	ErrAlreadyEnabled = errors.New("kiosk mode is already enabled")
	// ErrNoAdminProfile refuses a lock nobody could open: unlocking needs an
	// admin profile's PIN, and an admin profile always has one.
	ErrNoAdminProfile = errors.New("kiosk mode needs an admin profile")
	// ErrNoAdminClient refuses a lock with no on-device unlock and no paired
	// admin client to unlock it from.
	ErrNoAdminClient = errors.New("kiosk mode without on-device unlock needs a paired admin client")
	// ErrNothingAllowed reports that a profile could launch nothing. Enable
	// goes ahead when the caller confirms that is intended.
	ErrNothingAllowed = errors.New("a profile is limited to allowed content but nothing is allowed")
	// ErrInvalidSettings reports settings Enable or UpdateSettings refuse.
	ErrInvalidSettings = errors.New("invalid kiosk settings")
)

// Settings shape the lock. Durations are whole seconds, as they are stored
// and sent.
type Settings struct {
	// Preset records which preset the settings started from, for display.
	Preset string `json:"preset"`
	// DefaultContentMode is the content mode of a profile with none of its
	// own.
	DefaultContentMode string `json:"defaultContentMode"`
	// Downloads names which media downloads a card may ask for while locked.
	Downloads string `json:"downloads"`
	// IdleTimeout is how long the device may sit untouched before the
	// running media is stopped, when IdleReset is on.
	IdleTimeout int `json:"idleTimeout"`
	// UnlockDuration is how long a temporary unlock lasts.
	UnlockDuration int `json:"unlockDuration"`
	// DeviceUnlock lets an admin PIN entered on the device unlock it. Off,
	// only a paired admin client can.
	DeviceUnlock bool `json:"deviceUnlock"`
	// ProfileSwitching lets the active profile be changed between non-admin
	// profiles while locked.
	ProfileSwitching bool `json:"profileSwitching"`
	// ExtensionCard keeps the playtime extension card working while locked.
	ExtensionCard bool `json:"extensionCard"`
	// IdleReset stops the running media after IdleTimeout without input.
	IdleReset bool `json:"idleReset"`
}

// PresetSettings returns the defaults a preset stands for.
func PresetSettings(preset string) (Settings, bool) {
	switch preset {
	case PresetFamily:
		return Settings{
			Preset:             PresetFamily,
			DefaultContentMode: database.ContentModeAllowed,
			Downloads:          DownloadsNone,
			IdleTimeout:        int(defaultIdleTimeout / time.Second),
			UnlockDuration:     int(defaultUnlockDuration / time.Second),
			DeviceUnlock:       true,
			ProfileSwitching:   true,
			ExtensionCard:      true,
		}, true
	case PresetPublic:
		return Settings{
			Preset:             PresetPublic,
			DefaultContentMode: database.ContentModeAll,
			Downloads:          DownloadsNone,
			IdleTimeout:        int(defaultIdleTimeout / time.Second),
			UnlockDuration:     int(defaultUnlockDuration / time.Second),
			IdleReset:          true,
		}, true
	default:
		return Settings{}, false
	}
}

func (s *Settings) validate() error {
	if _, ok := PresetSettings(s.Preset); !ok {
		return fmt.Errorf("%w: unknown preset %q", ErrInvalidSettings, s.Preset)
	}
	if !database.ValidContentMode(s.DefaultContentMode) {
		return fmt.Errorf("%w: unknown content mode %q", ErrInvalidSettings, s.DefaultContentMode)
	}
	if s.Downloads != DownloadsNone {
		return fmt.Errorf("%w: unknown downloads mode %q", ErrInvalidSettings, s.Downloads)
	}
	unlock := time.Duration(s.UnlockDuration) * time.Second
	if unlock < minUnlockDuration || unlock > maxUnlockDuration {
		return fmt.Errorf("%w: unlock duration must be between %s and %s",
			ErrInvalidSettings, minUnlockDuration, maxUnlockDuration)
	}
	if s.IdleReset && time.Duration(s.IdleTimeout)*time.Second < minIdleTimeout {
		return fmt.Errorf("%w: idle timeout must be at least %s", ErrInvalidSettings, minIdleTimeout)
	}
	return nil
}

// Status is a snapshot of the lock.
type Status struct {
	// UnlockUntil is when a temporary unlock ends; zero unless State is
	// StateUnlocked.
	UnlockUntil time.Time
	State       State
	Settings    Settings
}

// Locked reports whether the snapshot is of an enforcing lock.
func (s *Status) Locked() bool { return s.State == StateLocked }

// Active reports whether kiosk mode is on, enforcing or not.
func (s *Status) Active() bool { return s.State != StateOff }

type persisted struct {
	Settings Settings `json:"settings"`
	Version  int      `json:"version"`
	Enabled  bool     `json:"enabled"`
}

type approvalKey struct {
	systemID string
	path     string
}

func newApprovalKey(systemID, path string) approvalKey {
	return approvalKey{systemID: strings.ToLower(systemID), path: pathutil.CanonicalMediaPath(path)}
}

// Service is the kiosk lock. A nil *Service is a device with no kiosk mode:
// every query reports off, so callers that were never given one need no
// guard of their own.
type Service struct {
	clock         clockwork.Clock
	db            *database.Database
	unlockTimer   clockwork.Timer
	approvals     map[approvalKey]time.Time
	unlockUntil   time.Time
	state         State
	recoveryToken string
	listeners     []func(Status)
	settings      Settings
	mu            syncutil.RWMutex
	// transitionMu serializes whole transitions, listeners included, so two
	// of them cannot interleave their notifications.
	transitionMu syncutil.Mutex
}

// NewService loads the stored lock. A device that was locked or temporarily
// unlocked when it stopped comes back locked: no restart, crash or power
// loss opens it.
func NewService(db *database.Database, clock clockwork.Clock) *Service {
	s := &Service{
		db:        db,
		clock:     clock,
		state:     StateOff,
		approvals: make(map[approvalKey]time.Time),
	}
	s.load()
	return s
}

func (s *Service) load() {
	raw, found, err := s.db.UserDB.GetDeviceState(database.DeviceStateKeyKiosk)
	if err != nil {
		// The stored lock cannot be read, so nothing says the device is
		// unlocked either. It stays off only because there are no settings
		// to enforce; the error is loud so the cause is found.
		log.Error().Err(err).Msg("kiosk: failed to read stored lock state")
		return
	}
	if !found {
		return
	}
	var stored persisted
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		log.Error().Err(err).Msg("kiosk: stored lock state is unreadable")
		return
	}
	if stored.Version != persistVersion || !stored.Enabled {
		return
	}
	if err := stored.Settings.validate(); err != nil {
		log.Error().Err(err).Msg("kiosk: stored settings are invalid, locking with the preset defaults")
		preset, ok := PresetSettings(stored.Settings.Preset)
		if !ok {
			preset, _ = PresetSettings(PresetFamily)
		}
		stored.Settings = preset
	}
	s.settings = stored.Settings
	s.state = StateLocked
}

func (s *Service) persistLocked() error {
	stored := persisted{Version: persistVersion, Enabled: s.state != StateOff, Settings: s.settings}
	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode kiosk state: %w", err)
	}
	if err := s.db.UserDB.SetDeviceState(database.DeviceStateKeyKiosk, string(raw)); err != nil {
		return fmt.Errorf("store kiosk state: %w", err)
	}
	return nil
}

// DB returns the database the lock is stored in.
func (s *Service) DB() *database.Database {
	return s.db
}

// Status returns the current lock snapshot.
func (s *Service) Status() Status {
	if s == nil {
		return Status{State: StateOff}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	return Status{State: s.state, Settings: s.settings, UnlockUntil: s.unlockUntil}
}

// Locked reports whether the lock is enforcing right now.
func (s *Service) Locked() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == StateLocked
}

// Active reports whether kiosk mode is on, locked or temporarily unlocked.
func (s *Service) Active() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state != StateOff
}

// OnChange registers fn to run after every transition, with the new
// snapshot. Listeners run in registration order on the goroutine that made
// the transition, so they must not call back into a transition.
func (s *Service) OnChange(fn func(Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// EnableOptions qualify an Enable call.
type EnableOptions struct {
	// ConfirmEmpty accepts a lock under which some profile can launch
	// nothing.
	ConfirmEmpty bool
}

// Enable turns kiosk mode on, locked.
func (s *Service) Enable(settings *Settings, opts EnableOptions) error {
	if err := settings.validate(); err != nil {
		return err
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	if s.Active() {
		return ErrAlreadyEnabled
	}
	if err := s.checkUnlockable(settings); err != nil {
		return err
	}
	if !opts.ConfirmEmpty {
		if err := s.checkSomethingAllowed(settings); err != nil {
			return err
		}
	}
	return s.transition(func() error {
		s.settings = *settings
		s.state = StateLocked
		s.unlockUntil = time.Time{}
		return s.persistLocked()
	})
}

// checkUnlockable refuses settings under which nobody could open the lock.
func (s *Service) checkUnlockable(settings *Settings) error {
	profiles, err := s.db.UserDB.ListProfiles()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}
	hasAdmin := false
	for i := range profiles {
		if profiles[i].Role == profileRoleAdmin {
			hasAdmin = true
			break
		}
	}
	if !hasAdmin {
		return ErrNoAdminProfile
	}
	if settings.DeviceUnlock {
		return nil
	}
	clients, err := s.db.UserDB.ListClients()
	if err != nil {
		return fmt.Errorf("list clients: %w", err)
	}
	for i := range clients {
		if clients[i].Role == string(permissions.RoleAdmin) {
			return nil
		}
	}
	return ErrNoAdminClient
}

// checkSomethingAllowed reports a profile a locked device could be left on
// that has an allowed-only rule and no allow entry.
func (s *Service) checkSomethingAllowed(settings *Settings) error {
	profiles, err := s.db.UserDB.ListProfiles()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}
	ids := []string{""}
	for i := range profiles {
		if profiles[i].Role != profileRoleAdmin {
			ids = append(ids, profiles[i].ProfileID)
		}
	}
	for _, id := range ids {
		rule, err := s.db.UserDB.GetContentRule(id)
		if err != nil {
			return fmt.Errorf("read content rule: %w", err)
		}
		mode := rule.Mode
		if mode == "" {
			mode = settings.DefaultContentMode
		}
		if mode != database.ContentModeAllowed {
			continue
		}
		allows := false
		for i := range rule.Entries {
			if rule.Entries[i].Effect == database.ContentEffectAllow {
				allows = true
				break
			}
		}
		if !allows {
			return ErrNothingAllowed
		}
	}
	return nil
}

// Disable turns kiosk mode off.
func (s *Service) Disable() error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if !s.Active() {
		return ErrNotEnabled
	}
	return s.transition(func() error {
		s.state = StateOff
		s.unlockUntil = time.Time{}
		if err := s.persistLocked(); err != nil {
			// The relock timer of a temporary unlock is still running. It
			// must be: the state rolls back to unlocked, and an unlock with
			// no timer behind it would never end.
			return err
		}
		s.stopTimerLocked()
		s.approvals = make(map[approvalKey]time.Time)
		return nil
	})
}

// Unlock lifts enforcement for d, or for the configured unlock duration
// when d is zero. Unlocking an already unlocked device restarts the clock.
func (s *Service) Unlock(d time.Duration) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if !s.Active() {
		return ErrNotEnabled
	}
	return s.transition(func() error {
		if d <= 0 {
			d = time.Duration(s.settings.UnlockDuration) * time.Second
		}
		if d > maxUnlockDuration {
			d = maxUnlockDuration
		}
		s.stopTimerLocked()
		s.state = StateUnlocked
		s.unlockUntil = s.clock.Now().Add(d)
		s.unlockTimer = s.clock.AfterFunc(d, s.relockOnExpiry)
		// Nothing is stored: the stored state only says kiosk mode is on,
		// and a device that restarts while unlocked comes back locked.
		return nil
	})
}

func (s *Service) relockOnExpiry() {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	s.mu.RLock()
	due := s.state == StateUnlocked && !s.clock.Now().Before(s.unlockUntil)
	s.mu.RUnlock()
	if !due {
		// A later unlock or a manual lock got here first.
		return
	}
	if err := s.transition(s.lockLocked); err != nil {
		log.Error().Err(err).Msg("kiosk: failed to relock after temporary unlock")
	}
}

// Lock ends a temporary unlock early.
func (s *Service) Lock() error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if !s.Active() {
		return ErrNotEnabled
	}
	if s.Locked() {
		return nil
	}
	return s.transition(s.lockLocked)
}

func (s *Service) lockLocked() error {
	s.stopTimerLocked()
	s.state = StateLocked
	s.unlockUntil = time.Time{}
	return nil
}

func (s *Service) stopTimerLocked() {
	if s.unlockTimer != nil {
		s.unlockTimer.Stop()
		s.unlockTimer = nil
	}
}

// UpdateSettings replaces the settings of an enabled lock.
func (s *Service) UpdateSettings(settings *Settings) error {
	replacement := *settings
	return s.PatchSettings(func(current *Settings) { *current = replacement })
}

// PatchSettings changes the settings of an enabled lock. patch is given the
// settings as they are now and edits them in place, all inside one
// transition, so two changes made at once each build on the other's result
// instead of one silently undoing the other.
func (s *Service) PatchSettings(patch func(*Settings)) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if !s.Active() {
		return ErrNotEnabled
	}
	settings := s.Status().Settings
	patch(&settings)
	if err := settings.validate(); err != nil {
		return err
	}
	if err := s.checkUnlockable(&settings); err != nil {
		return err
	}
	return s.transition(func() error {
		s.settings = settings
		return s.persistLocked()
	})
}

// transition applies change under the state lock and then tells the
// listeners. A change that fails leaves the previous state in place.
// transitionMu must be held.
func (s *Service) transition(change func() error) error {
	s.mu.Lock()
	before := s.statusLocked()
	if err := change(); err != nil {
		s.state = before.State
		s.settings = before.Settings
		s.unlockUntil = before.UnlockUntil
		s.mu.Unlock()
		return err
	}
	after := s.statusLocked()
	listeners := append([]func(Status){}, s.listeners...)
	s.mu.Unlock()

	log.Info().Str("from", string(before.State)).Str("to", string(after.State)).Msg("kiosk: state changed")
	for _, fn := range listeners {
		fn(after)
	}
	return nil
}

// AllowOnce lets the next launch of one media path through a locked
// device's content rule. It expires unused after a few minutes.
func (s *Service) AllowOnce(systemID, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approvals[newApprovalKey(systemID, path)] = s.clock.Now().Add(approvalTTL)
}

// approved reports whether a one-time approval covers the launch, and uses
// it up when consume is set.
func (s *Service) approved(systemID, path string, consume bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	for key, expires := range s.approvals {
		if !now.Before(expires) {
			delete(s.approvals, key)
		}
	}
	key := newApprovalKey(systemID, path)
	if _, ok := s.approvals[key]; !ok {
		return false
	}
	if consume {
		delete(s.approvals, key)
	}
	return true
}

// InitRecoveryToken writes a fresh recovery token into dir. The token
// stands in for an admin PIN on the device itself, and reading it takes the
// file access a locked-out owner has and a child at the controls does not.
func (s *Service) InitRecoveryToken(fs afero.Fs, dir string) error {
	raw := make([]byte, recoveryTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate recovery token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := afero.WriteFile(fs, RecoveryTokenPath(dir), []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write recovery token: %w", err)
	}
	s.mu.Lock()
	s.recoveryToken = token
	s.mu.Unlock()
	return nil
}

// RecoveryTokenPath is where InitRecoveryToken writes the token under dir.
func RecoveryTokenPath(dir string) string {
	return filepath.Join(dir, recoveryTokenFile)
}

// CheckRecoveryToken reports whether token is the current recovery token.
func (s *Service) CheckRecoveryToken(token string) bool {
	if s == nil || token == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.recoveryToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.recoveryToken)) == 1
}

// CommandPolicy is what a guarded token may run on a locked device: card
// text, a capped caller's run, a remote operation. It is default-deny,
// so a command added later stays out until it is listed here.
//
// Everything that starts or steers media is in, because that is what a card
// is for. What is out either changes the device (execute, input, write,
// the MiSTer verbs), reaches the network, or opens the lock from the
// outside (profile.clear). Launches still answer to the content rule.
func (s *Service) CommandPolicy() tokens.CommandPolicy {
	settings := s.Status().Settings
	names := []string{
		gozapscript.ZapScriptCmdLaunch,
		gozapscript.ZapScriptCmdLaunchSystem,
		gozapscript.ZapScriptCmdLaunchRandom,
		gozapscript.ZapScriptCmdLaunchSearch,
		gozapscript.ZapScriptCmdLaunchTitle,
		gozapscript.ZapScriptCmdLaunchLast,
		gozapscript.ZapScriptCmdSystem,
		gozapscript.ZapScriptCmdRandom,
		gozapscript.ZapScriptCmdStop,
		gozapscript.ZapScriptCmdPlaylistPlay,
		gozapscript.ZapScriptCmdPlaylistStop,
		gozapscript.ZapScriptCmdPlaylistNext,
		gozapscript.ZapScriptCmdPlaylistPrevious,
		gozapscript.ZapScriptCmdPlaylistGoto,
		gozapscript.ZapScriptCmdPlaylistPause,
		gozapscript.ZapScriptCmdPlaylistLoad,
		gozapscript.ZapScriptCmdPlaylistOpen,
		gozapscript.ZapScriptCmdDelay,
		gozapscript.ZapScriptCmdEcho,
		gozapscript.ZapScriptCmdControl,
	}
	if settings.ProfileSwitching {
		names = append(names, gozapscript.ZapScriptCmdProfile)
	}
	if settings.ExtensionCard {
		names = append(names, gozapscript.ZapScriptCmdPlaytimeExtend)
	}
	return tokens.NewCommandPolicy(names...)
}

// GuardToken marks a token whose script the device's owner did not write
// here, so that it answers to the kiosk command policy for as long as it
// runs. The mark is made whether or not the lock is enforcing: the policy
// is consulted when each command runs, which is what lets a lock that
// engages later reach a script that was already running or queued.
// Everything the token expands into carries the mark with it.
func (s *Service) GuardToken(token *tokens.Token) {
	if s == nil {
		return
	}
	token.AllowedCommands = token.AllowedCommands.WithGuard()
}

// CommandPermitted reports whether a guarded token may run a command right
// now. With the lock not enforcing, every command is permitted.
func (s *Service) CommandPermitted(name string) bool {
	if !s.Locked() {
		return true
	}
	return s.CommandPolicy().Allows(name)
}

// IdleResetSupported reports whether pl can tell when the device is being
// used well enough to stop media for a device nobody is at. Missing any
// class of input would end a game under someone using the input it cannot
// see.
func IdleResetSupported(pl platforms.Platform) bool {
	source := platforms.ResolveInputActivity(pl)
	return source != nil && source.InputCoverage().Has(platforms.InputAll)
}
