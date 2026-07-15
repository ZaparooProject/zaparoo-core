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

package playtime

// TODO: Time Zone Manipulation (time crimes)
//    A user could change the system clock backward (e.g., set time to yesterday)
//    to bypass daily limits or extend session time.
//    Fix: If online, verify time against NTP server. If offline, detect large
//    backward time jumps using monotonic uptime and invalidate suspicious sessions.

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultSessionResetTimeout is the default idle time before a session resets.
	// After this period of inactivity, the next game launch starts a fresh session.
	DefaultSessionResetTimeout = 20 * time.Minute

	// MinimumViableSession is the minimum time a session should run before being stoppable.
	// If remaining time < this value, the launch is blocked entirely rather than starting
	// a game that will be immediately killed.
	MinimumViableSession = 1 * time.Minute
)

// SessionState represents the current state of a playtime session.
type SessionState int

const (
	// StateReset indicates no active session, cumulative time is zero.
	StateReset SessionState = iota
	// StateActive indicates a game is currently running and time is being tracked.
	StateActive
	// StateCooldown indicates no game is running, but session may continue if another
	// game launches within the session reset timeout period.
	StateCooldown
)

// String returns the string representation of the session state.
func (s SessionState) String() string {
	return [...]string{"reset", "active", "cooldown"}[s]
}

// pinnedLimits captures the limits context in force when a game launched.
// Everything about a running game belongs to the profile that launched it:
// if the profile deactivates mid-game (switch to the shared profile), the
// pinned values keep governing until the media stops, so clearing a
// profile cannot be used to escape its limits.
type pinnedLimits struct {
	profileID string
	daily     time.Duration
	session   time.Duration
	enabled   bool
}

// LimitsManager enforces time limits and warnings for gameplay sessions.
type LimitsManager struct {
	sessionStart          time.Time
	sessionStartMono      time.Time
	lastStopTime          time.Time
	platform              platforms.Platform
	clock                 clockwork.Clock
	ctx                   context.Context
	cooldownTimer         clockwork.Timer
	done                  chan struct{}
	warningsGiven         map[time.Duration]bool
	db                    *database.Database
	notificationsSend     chan<- models.Notification
	cfg                   *config.Instance
	limits                LimitsProvider
	sessionLimits         *pinnedLimits // launch-time limits for the running game; nil between sessions
	player                audio.Player
	cancel                context.CancelFunc
	sessionCancel         context.CancelFunc // cancels checkLoop for the current game session; nil between sessions
	lastProfileID         string             // last-seen active profile ID, for identity-change detection
	state                 SessionState
	sessionCumulativeTime time.Duration
	subscriptionID        int
	wg                    sync.WaitGroup
	mu                    syncutil.Mutex
	enabledMu             syncutil.Mutex
	sessionStartReliable  bool
	enabled               bool
	stopping              bool
}

// NewLimitsManager creates a new LimitsManager instance.
func NewLimitsManager(
	db *database.Database,
	platform platforms.Platform,
	cfg *config.Instance,
	clock clockwork.Clock,
	player audio.Player,
) *LimitsManager {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	close(done)

	return &LimitsManager{
		state:         StateReset,
		db:            db,
		platform:      platform,
		cfg:           cfg,
		limits:        globalProvider{cfg: cfg},
		clock:         clock,
		player:        player,
		ctx:           ctx,
		cancel:        cancel,
		done:          done,
		warningsGiven: make(map[time.Duration]bool),
		enabled:       false, // Start disabled, caller must enable
	}
}

// SetLimitsProvider replaces the source of limit values, e.g. with the
// profile-aware resolver. Must be called before Start.
func (tm *LimitsManager) SetLimitsProvider(limits LimitsProvider) {
	tm.limits = limits
}

// Broker is the interface for subscribing to notifications.
type Broker interface {
	Subscribe(bufferSize int, methods ...string) (<-chan models.Notification, int)
	Unsubscribe(id int)
}

// Start begins monitoring for time limit enforcement.
// It subscribes to the broker to listen for media.started and media.stopped events.
func (tm *LimitsManager) Start(broker Broker, notificationsSend chan<- models.Notification) {
	done := make(chan struct{})

	tm.mu.Lock()
	tm.notificationsSend = notificationsSend
	tm.done = done
	tm.stopping = false
	// Seed identity-change detection with the boot-restored profile: its
	// profiles.active notification fired before this subscription existed.
	tm.lastProfileID = tm.limits.ActiveProfileID()
	tm.mu.Unlock()

	// Subscribe to broker for media lifecycle and profile-switch events only.
	// The method filter prevents indexing-storm traffic from filling the buffer
	// and dropping these rare events. profiles.active MUST be in this filter or
	// profile switches never reset the session.
	notifChan, subID := broker.Subscribe(32,
		models.NotificationStarted, models.NotificationStopped, models.NotificationProfilesActive)
	tm.subscriptionID = subID

	go func() {
		defer close(done)
		tm.handleNotifications(notifChan, broker)
	}()
}

// Stop shuts down the LimitsManager.
func (tm *LimitsManager) Stop() {
	tm.mu.Lock()
	tm.stopping = true
	done := tm.done
	tm.mu.Unlock()

	tm.cancel()

	if done != nil {
		<-done
	}

	tm.wg.Wait()
}

// SetEnabled records the runtime enabled state and, when disabling, resets
// the session completely (clears cooldown and cumulative time). Whether
// limits are actually enforced is decided by the LimitsProvider on every
// check (global config, possibly overridden by the active profile) — this
// flag exists for its session-reset side effect when the user toggles
// limits off, and is kept in sync with global config by the settings
// handler.
func (tm *LimitsManager) SetEnabled(enabled bool) {
	tm.enabledMu.Lock()
	tm.enabled = enabled
	tm.enabledMu.Unlock()

	// If disabling, reset the session completely (clear cooldown state)
	if !enabled {
		tm.mu.Lock()
		// Cancel cooldown timer if running
		if tm.cooldownTimer != nil {
			tm.cooldownTimer.Stop()
			tm.cooldownTimer = nil
			log.Debug().Msg("playtime: cancelled cooldown timer (limits disabled)")
		}
		// Cancel the active session's check loop if one is running
		if tm.sessionCancel != nil {
			tm.sessionCancel()
			tm.sessionCancel = nil
		}

		if tm.state != StateReset {
			log.Info().Msg("playtime: limits disabled, resetting session state")
			tm.transitionTo(StateReset)
			tm.sessionStart = time.Time{}
			tm.sessionStartMono = time.Time{}
			tm.sessionCumulativeTime = 0
			tm.lastStopTime = time.Time{}
			tm.sessionStartReliable = false
			tm.warningsGiven = make(map[time.Duration]bool)
			tm.sessionLimits = nil
		}
		tm.mu.Unlock()
	}
}

// onProfileChanged handles a profiles.active notification. The session is
// reset only when the profile *identity* changes to a different person:
//
//   - Re-activating the already-active profile (rescanning your own card)
//     or editing the active profile (which refreshes the snapshot and
//     re-broadcasts) does nothing — otherwise a rescan every 50 minutes
//     would defeat a 1h session limit.
//   - Deactivating (switching to the shared profile) never resets: a
//     running game keeps its launch-pinned limits until it stops, and
//     cooldown/cumulative time survives so scanning a **profile.clear
//     card cannot be used to escape a session limit.
//   - Switching to a different profile resets the session: a different
//     person is playing. If a game is running, the session is re-pinned
//     to the new profile and tracking restarts from now.
func (tm *LimitsManager) onProfileChanged() {
	current := tm.limits.ActiveProfileID()

	tm.mu.Lock()
	if current == tm.lastProfileID {
		tm.mu.Unlock()
		return
	}
	tm.lastProfileID = current

	if current == "" {
		tm.mu.Unlock()
		log.Info().Msg("playtime: profile deactivated, session state retained")
		return
	}

	if tm.state == StateActive {
		// The running game now belongs to the new profile.
		tm.sessionLimits = tm.snapshotLimits()
	}
	tm.mu.Unlock()

	tm.ResetSession()
}

// snapshotLimits captures the provider's current values. Callers may hold
// tm.mu: the provider reads service state under its own lock.
func (tm *LimitsManager) snapshotLimits() *pinnedLimits {
	return &pinnedLimits{
		profileID: tm.limits.ActiveProfileID(),
		enabled:   tm.limits.PlaytimeLimitsEnabled(),
		daily:     tm.limits.DailyLimit(),
		session:   tm.limits.SessionLimit(),
	}
}

// pinned returns the launch-time limits when they should govern instead of
// the live provider: a game session exists, it was launched (or re-pinned)
// under a profile, and the device has since deactivated to the shared
// profile. Returns nil when live values apply. Must be called without
// tm.mu held.
func (tm *LimitsManager) pinned() *pinnedLimits {
	tm.mu.Lock()
	p := tm.sessionLimits
	tm.mu.Unlock()
	if p == nil || p.profileID == "" {
		return nil
	}
	if tm.limits.ActiveProfileID() != "" {
		return nil
	}
	return p
}

// effectiveEnabled reports whether limits are enforced for the current
// session, honoring launch-time pinning. Must be called without tm.mu held.
func (tm *LimitsManager) effectiveEnabled() bool {
	if p := tm.pinned(); p != nil {
		return p.enabled
	}
	return tm.limits.PlaytimeLimitsEnabled()
}

// effectiveDailyLimit returns the daily limit for the current session,
// honoring launch-time pinning. Must be called without tm.mu held.
func (tm *LimitsManager) effectiveDailyLimit() time.Duration {
	if p := tm.pinned(); p != nil {
		return p.daily
	}
	return tm.limits.DailyLimit()
}

// effectiveSessionLimit returns the session limit for the current session,
// honoring launch-time pinning. Must be called without tm.mu held.
func (tm *LimitsManager) effectiveSessionLimit() time.Duration {
	if p := tm.pinned(); p != nil {
		return p.session
	}
	return tm.limits.SessionLimit()
}

// effectiveProfileID returns the profile whose history scopes daily usage
// accounting, honoring launch-time pinning. Must be called without tm.mu
// held.
func (tm *LimitsManager) effectiveProfileID() string {
	if p := tm.pinned(); p != nil {
		return p.profileID
	}
	return tm.limits.ActiveProfileID()
}

// ResetSession starts a fresh limit session, called when the active
// profile identity changes: a different person is playing, so accumulated
// session time belongs to the previous profile. Daily usage is unaffected
// — it is recalculated from the (profile-attributed) history on every
// check.
//
// If a game is running, tracking restarts from now under the new profile's
// limits rather than stopping: the running game's already-played time was
// the previous profile's.
func (tm *LimitsManager) ResetSession() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.cooldownTimer != nil {
		tm.cooldownTimer.Stop()
		tm.cooldownTimer = nil
		log.Debug().Msg("playtime: cancelled cooldown timer (profile switched)")
	}

	switch tm.state {
	case StateActive:
		log.Info().Msg("playtime: profile switched mid-game, restarting session tracking")
		now := tm.clock.Now()
		tm.sessionStart = now
		tm.sessionStartMono = time.Now()
		tm.sessionStartReliable = helpers.IsClockReliable(now)
		tm.sessionCumulativeTime = 0
		tm.warningsGiven = make(map[time.Duration]bool)
	case StateCooldown:
		log.Info().Msg("playtime: profile switched, resetting session state")
		tm.transitionTo(StateReset)
		tm.sessionStart = time.Time{}
		tm.sessionStartMono = time.Time{}
		tm.sessionCumulativeTime = 0
		tm.lastStopTime = time.Time{}
		tm.sessionStartReliable = false
		tm.warningsGiven = make(map[time.Duration]bool)
	case StateReset:
		tm.sessionCumulativeTime = 0
	}
}

// IsEnabled returns whether limits are currently enforced.
func (tm *LimitsManager) IsEnabled() bool {
	tm.enabledMu.Lock()
	defer tm.enabledMu.Unlock()
	return tm.enabled
}

// isSessionActive returns true if a session is currently being tracked.
func (tm *LimitsManager) isSessionActive() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return !tm.sessionStart.IsZero()
}

// transitionTo transitions the session state machine to a new state and logs the transition.
// Caller must hold tm.mu lock.
func (tm *LimitsManager) transitionTo(newState SessionState) {
	oldState := tm.state
	tm.state = newState

	if oldState != newState {
		log.Info().
			Str("from", oldState.String()).
			Str("to", newState.String()).
			Msg("playtime: state transition")
	}
}

// handleNotifications processes notification events from the broker.
func (tm *LimitsManager) handleNotifications(notifChan <-chan models.Notification, broker Broker) {
	log.Debug().Msg("playtime: notification handler started")
	defer func() {
		broker.Unsubscribe(tm.subscriptionID)
		log.Debug().Msg("playtime: notification handler stopped")
	}()

	for {
		select {
		case notif, ok := <-notifChan:
			if !ok {
				// Channel closed
				return
			}

			// Handle media lifecycle events
			switch notif.Method {
			case models.NotificationStarted:
				tm.OnMediaStarted()
			case models.NotificationStopped:
				tm.OnMediaStopped()
			case models.NotificationProfilesActive:
				tm.onProfileChanged()
			}

		case <-tm.ctx.Done():
			return
		}
	}
}

// OnMediaStarted handles media.started events and begins time tracking.
func (tm *LimitsManager) OnMediaStarted() {
	if !tm.limits.PlaytimeLimitsEnabled() {
		return
	}

	tm.mu.Lock()
	if tm.stopping {
		tm.mu.Unlock()
		return
	}

	now := tm.clock.Now()

	// Cancel cooldown timer if it exists
	if tm.cooldownTimer != nil {
		tm.cooldownTimer.Stop()
		tm.cooldownTimer = nil
		log.Debug().Msg("playtime: cancelled cooldown timer (new game starting)")
	}

	// Handle game start based on current state
	switch tm.state {
	case StateReset:
		// Starting fresh session
		tm.sessionCumulativeTime = 0
		log.Info().Msg("playtime: starting new session")

	case StateCooldown:
		// Resuming session after game switch (within timeout)
		log.Info().
			Dur("cumulative", tm.sessionCumulativeTime).
			Msg("playtime: resuming session after game switch")

	case StateActive:
		// Already active - this shouldn't normally happen, but handle gracefully
		log.Warn().Msg("playtime: game started while already active")
	}

	// Transition to active state
	tm.transitionTo(StateActive)

	tm.sessionStart = now
	tm.sessionStartMono = time.Now() // Monotonic clock for accurate duration
	tm.sessionStartReliable = helpers.IsClockReliable(now)
	tm.warningsGiven = make(map[time.Duration]bool)

	// Pin the launch-time limits context: everything about this game
	// belongs to the profile that launched it, even if the profile
	// deactivates mid-game.
	tm.sessionLimits = tm.snapshotLimits()

	// Cancel any stale check loop (defensive; shouldn't fire in normal state transitions).
	if tm.sessionCancel != nil {
		tm.sessionCancel()
	}
	// Per-session context so the check loop stops when this game stops, not when the
	// manager shuts down. This prevents goroutine leaks across multiple game launches.
	sessionCtx, sessionCancel := context.WithCancel(tm.ctx)
	tm.sessionCancel = sessionCancel

	tm.wg.Add(1)
	tm.mu.Unlock()

	if !tm.sessionStartReliable {
		log.Warn().
			Int("year", now.Year()).
			Msg("playtime: session started with unreliable clock - daily limits disabled for this session")
	}

	// Start the check loop (exits when the session context is cancelled).
	go func() {
		defer tm.wg.Done()
		tm.checkLoop(sessionCtx)
	}()
}

// OnMediaStopped handles media.stopped events and stops time tracking.
func (tm *LimitsManager) OnMediaStopped() {
	tm.mu.Lock()

	if tm.stopping || tm.state != StateActive {
		// Only stop if we're actually active
		tm.mu.Unlock()
		return
	}

	// Cancel the per-session check loop now that the game has stopped.
	if tm.sessionCancel != nil {
		tm.sessionCancel()
		tm.sessionCancel = nil
	}

	// Calculate how long this game was played using monotonic clock (accurate, handles sleep)
	now := tm.clock.Now()
	nowMono := time.Now()
	gameDuration := nowMono.Sub(tm.sessionStartMono)
	tm.sessionCumulativeTime += gameDuration
	tm.lastStopTime = now

	log.Info().
		Dur("game_duration", gameDuration).
		Dur("session_cumulative", tm.sessionCumulativeTime).
		Msg("playtime: game stopped")

	// Transition to COOLDOWN state (session not reset yet, waiting for timeout)
	tm.transitionTo(StateCooldown)

	tm.sessionStart = time.Time{}     // Mark no active game
	tm.sessionStartMono = time.Time{} // Clear monotonic start
	tm.sessionStartReliable = false
	tm.warningsGiven = make(map[time.Duration]bool)
	tm.sessionLimits = nil // launch-time pinning ends with the game

	// Start cooldown timer if timeout is configured (read live from config).
	if sessionResetTimeout := tm.cfg.SessionResetTimeout(); sessionResetTimeout > 0 {
		tm.cooldownTimer = tm.clock.NewTimer(sessionResetTimeout)
		tm.wg.Add(1)
		tm.mu.Unlock()

		go func() {
			defer tm.wg.Done()
			tm.cooldownTimerLoop()
		}()
		return
	}

	tm.mu.Unlock()
}

// cooldownTimerLoop waits for the cooldown timer to expire and transitions to reset.
func (tm *LimitsManager) cooldownTimerLoop() {
	tm.mu.Lock()
	timer := tm.cooldownTimer
	tm.mu.Unlock()

	if timer == nil {
		return
	}

	select {
	case <-timer.Chan():
		// Timer expired - transition to reset
		tm.mu.Lock()
		if tm.state == StateCooldown {
			log.Info().
				Dur("timeout", tm.cfg.SessionResetTimeout()).
				Msg("playtime: cooldown timer expired, resetting session")
			tm.transitionTo(StateReset)
			tm.sessionCumulativeTime = 0
			tm.lastStopTime = time.Time{}
			tm.cooldownTimer = nil
		}
		tm.mu.Unlock()

	case <-tm.ctx.Done():
		// Manager stopping
		return
	}
}

// checkLoop runs periodic checks for time limits.
// Checks every 30 seconds to ensure 1-minute warnings are not skipped.
// ctx is the per-session context cancelled when the game stops; it is a child of
// tm.ctx so manager shutdown also terminates the loop.
func (tm *LimitsManager) checkLoop(ctx context.Context) {
	ticker := tm.clock.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Immediate check
	if ctx.Err() != nil {
		return
	}
	tm.checkLimits()

	for {
		select {
		case <-ticker.Chan():
			tm.checkLimits()
		case <-ctx.Done():
			return
		}
	}
}

// checkLimits evaluates all rules and handles warnings/limits.
func (tm *LimitsManager) checkLimits() {
	// Enforcement is decided by the effective limits: the live provider,
	// or the launch-pinned context after a mid-game deactivation.
	if !tm.effectiveEnabled() {
		return
	}

	tm.mu.Lock()
	if tm.sessionStart.IsZero() {
		tm.mu.Unlock()
		return
	}
	sessionStart := tm.sessionStart
	tm.mu.Unlock()

	// Build rule context (time.Sub automatically uses monotonic clock for accuracy)
	ctx, err := tm.buildRuleContext(sessionStart)
	if err != nil {
		log.Error().Err(err).Msg("playtime: failed to build rule context")
		return
	}

	// Create rules
	rules := tm.createRules()
	if len(rules) == 0 {
		// No limits configured
		return
	}

	// Evaluate all rules and find minimum remaining time
	allowed := true
	var minRemaining time.Duration
	var reason string

	for _, rule := range rules {
		ruleAllowed, remaining, ruleReason := rule.Evaluate(ctx)
		if !ruleAllowed {
			allowed = false
			reason = ruleReason
			break
		}
		if minRemaining == 0 || (remaining > 0 && remaining < minRemaining) {
			minRemaining = remaining
		}
	}

	if !allowed {
		// Time limit reached - stop the game and return to menu
		log.Warn().Str("reason", reason).Msg("playtime: time limit reached, stopping game")
		notifications.PlaytimeLimitReached(tm.notificationsSend, models.PlaytimeLimitReachedParams{
			Reason: reason,
		})
		tm.playWarningSound()

		if err := tm.platform.StopActiveLauncher(platforms.StopForMenu); err != nil {
			log.Error().Err(err).Msg("playtime: failed to stop active launcher")
		}

		tm.OnMediaStopped()
		return
	}

	// Check for warnings
	if minRemaining > 0 {
		tm.handleWarnings(minRemaining)
	}
}

// buildRuleContext creates a RuleContext from current state.
func (tm *LimitsManager) buildRuleContext(
	sessionStart time.Time,
) (RuleContext, error) {
	now := tm.clock.Now()

	tm.mu.Lock()
	cumulativeTime := tm.sessionCumulativeTime
	sessionStartWasReliable := tm.sessionStartReliable
	sessionStartMono := tm.sessionStartMono
	tm.mu.Unlock()

	// Current game duration via wall clock.
	wallElapsed := now.Sub(sessionStart)

	// Detect in-session clock rollback by comparing wall-clock elapsed vs
	// monotonic elapsed since session start. If the wall clock moved backward by
	// more than 5 minutes relative to monotonic, use the monotonic-derived
	// duration instead so the session limit still reflects real time played.
	var currentGameDuration time.Duration
	if !sessionStartMono.IsZero() {
		monoElapsed := time.Since(sessionStartMono)
		if wallElapsed < 0 || monoElapsed > wallElapsed+5*time.Minute {
			log.Warn().
				Dur("wall_elapsed", wallElapsed).
				Dur("mono_elapsed", monoElapsed).
				Msg("playtime: clock rollback detected, using monotonic for session duration")
			currentGameDuration = monoElapsed
		} else {
			currentGameDuration = wallElapsed
		}
	} else {
		currentGameDuration = wallElapsed
		if currentGameDuration < 0 {
			currentGameDuration = 0
		}
	}

	// Total session duration = previous games + current game
	sessionDuration := cumulativeTime + currentGameDuration

	// Check if BOTH clocks are trustworthy for daily limit enforcement
	currentClockReliable := helpers.IsClockReliable(now)
	bothClocksReliable := sessionStartWasReliable && currentClockReliable

	var dailyUsage time.Duration
	if bothClocksReliable && tm.effectiveDailyLimit() > 0 {
		// Both clocks appear valid and a daily limit is configured - calculate daily usage.
		year, month, day := now.Date()
		todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())

		// Calculate how much of the current session counts toward today
		sessionStartToday := sessionStart
		if sessionStart.Before(todayStart) {
			// Session started yesterday - only count time after midnight
			sessionStartToday = todayStart
		}
		sessionDurationToday := now.Sub(sessionStartToday)

		// Lower-bound clamp: prevent negative duration during wall-clock rollback.
		if sessionDurationToday < 0 {
			sessionDurationToday = 0
		}

		// Safety clamp: Session duration today cannot exceed total session duration.
		// This prevents math errors when clock jumps (e.g., 1970 → 2025 mid-session).
		if sessionDurationToday > sessionDuration {
			sessionDurationToday = sessionDuration
		}

		// Calculate today's total usage from MediaHistory
		usage, err := tm.calculateDailyUsage(todayStart, sessionDurationToday)
		if err != nil {
			return RuleContext{}, fmt.Errorf("failed to calculate daily usage: %w", err)
		}
		dailyUsage = usage
	} else if !bothClocksReliable {
		// Clock unreliable - skip daily usage calculation.
		// DailyLimitRule will skip enforcement when ClockReliable is false.
		// This provides graceful degradation: session limits still work.
		dailyUsage = 0

		if !sessionStartWasReliable {
			// Session started with bad clock - daily disabled for entire session
			log.Debug().
				Int("year", now.Year()).
				Msg("playtime: daily limits disabled - session started with unreliable clock")
		} else if !currentClockReliable {
			// Clock became unreliable during session - log once at debug level
			// (checkLimits runs every minute, so we avoid log spam)
			log.Debug().
				Int("year", now.Year()).
				Msg("playtime: system clock appears unreliable, daily limits disabled (session limits still active)")
		}
	}

	return RuleContext{
		CurrentTime:     now,
		SessionDuration: sessionDuration,
		DailyUsageToday: dailyUsage,
		ClockReliable:   bothClocksReliable,
	}, nil
}

// calculateDailyUsage returns the total play time for today.
// completedSeconds is the sum of all closed sessions overlapping today (from SQL).
// When a profile is active, only history attributed to that profile is
// counted; the shared profile (no active profile) counts all history, so
// deactivating never grants a fresh daily allowance.
// currentSessionDuration is the current game's contribution to today (computed by
// the caller and already clamped to the today window).
// Active sessions (EndTime IS NULL) are excluded by the SQL query; callers add
// currentSessionDuration separately to avoid double-counting.
func (tm *LimitsManager) calculateDailyUsage(
	todayStart time.Time,
	currentSessionDuration time.Duration,
) (time.Duration, error) {
	var completedSeconds int64
	var err error
	if profileID := tm.effectiveProfileID(); profileID != "" {
		completedSeconds, err = tm.db.UserDB.SumMediaPlayTimeForDayByProfile(todayStart, profileID)
	} else {
		completedSeconds, err = tm.db.UserDB.SumMediaPlayTimeForDay(todayStart)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to sum daily play time: %w", err)
	}
	return time.Duration(completedSeconds)*time.Second + currentSessionDuration, nil
}

// createRules builds the list of active rules based on configuration.
func (tm *LimitsManager) createRules() []Rule {
	rules := make([]Rule, 0, 2)

	if limit := tm.effectiveSessionLimit(); limit > 0 {
		rules = append(rules, &SessionLimitRule{Limit: limit})
	}

	if limit := tm.effectiveDailyLimit(); limit > 0 {
		rules = append(rules, &DailyLimitRule{Limit: limit})
	}

	return rules
}

// handleWarnings checks if warnings should be emitted based on remaining time.
func (tm *LimitsManager) handleWarnings(remaining time.Duration) {
	intervals := tm.limits.WarningIntervals()

	// Sort intervals in descending order (largest first)
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i] > intervals[j]
	})

	// Check if we should send a warning (under lock)
	tm.mu.Lock()
	var warningInterval time.Duration
	for _, interval := range intervals {
		if remaining > interval || tm.warningsGiven[interval] {
			continue
		}
		tm.warningsGiven[interval] = true
		warningInterval = interval
		break
	}
	tm.mu.Unlock()

	// If we found a warning to send, do it outside the lock
	if warningInterval > 0 {
		log.Info().Dur("remaining", remaining).Msg("playtime: warning threshold reached")

		// Send notification with interval and remaining time in payload
		notifications.PlaytimeLimitWarning(tm.notificationsSend, models.PlaytimeLimitWarningParams{
			Interval:  warningInterval.String(),
			Remaining: remaining.String(),
		})

		// Play warning sound
		tm.playWarningSound()
	}
}

func (tm *LimitsManager) playWarningSound() {
	path, enabled := tm.cfg.LimitSoundPath(helpers.DataDir(tm.platform))
	helpers.PlayConfiguredSound(tm.player, path, enabled, assets.LimitSound, "limit")
}

// StatusInfo contains current playtime session and limit status.
type StatusInfo struct {
	SessionStarted        time.Time
	DailyUsageToday       *time.Duration
	DailyRemaining        *time.Duration
	State                 string
	SessionDuration       time.Duration
	SessionCumulativeTime time.Duration
	SessionRemaining      time.Duration
	CooldownRemaining     time.Duration
	SessionActive         bool
}

// GetStatus returns the current playtime session and limit status.
// Always returns a StatusInfo struct with current state information.
func (tm *LimitsManager) GetStatus() *StatusInfo {
	// Snapshot session state under lock
	tm.mu.Lock()
	sessionStart := tm.sessionStart
	currentState := tm.state
	cumulativeTime := tm.sessionCumulativeTime
	lastStop := tm.lastStopTime
	tm.mu.Unlock()

	resetTimeout := tm.cfg.SessionResetTimeout()

	now := tm.clock.Now()

	// State: Reset (no session exists)
	if currentState == StateReset {
		status := &StatusInfo{
			State:         StateReset.String(),
			SessionActive: false,
		}

		// Calculate daily usage/remaining even during reset - this data is valid
		// regardless of session state (the user has used time today and has
		// time remaining in their daily allowance)
		dailyLimit := tm.effectiveDailyLimit()
		if dailyLimit > 0 && helpers.IsClockReliable(now) {
			year, month, day := now.Date()
			todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
			usage, err := tm.calculateDailyUsage(todayStart, 0)
			if err == nil {
				status.DailyUsageToday = &usage
				dailyRemaining := dailyLimit - usage
				if dailyRemaining < 0 {
					dailyRemaining = 0
				}
				status.DailyRemaining = &dailyRemaining
			}
		}

		return status
	}

	// State: Cooldown (session exists but no game running)
	if currentState == StateCooldown {
		// Calculate cooldown remaining (timer will handle the actual transition)
		var cooldownRemaining time.Duration
		if resetTimeout > 0 && !lastStop.IsZero() {
			elapsed := now.Sub(lastStop)
			cooldownRemaining = resetTimeout - elapsed
			if cooldownRemaining < 0 {
				cooldownRemaining = 0
			}
		}

		// Calculate remaining times based on cumulative time
		var sessionRemaining time.Duration
		sessionLimit := tm.effectiveSessionLimit()
		dailyLimit := tm.effectiveDailyLimit()

		if sessionLimit > 0 {
			sessionRemaining = sessionLimit - cumulativeTime
			if sessionRemaining < 0 {
				sessionRemaining = 0
			}
		}

		// Build status with session info
		status := &StatusInfo{
			State:                 StateCooldown.String(),
			SessionActive:         false,
			SessionStarted:        time.Time{}, // Zero time - will be omitted from API response
			SessionDuration:       cumulativeTime,
			SessionCumulativeTime: cumulativeTime,
			SessionRemaining:      sessionRemaining,
			CooldownRemaining:     cooldownRemaining,
		}

		// For daily usage/remaining, we need to calculate today's total usage
		if dailyLimit > 0 && helpers.IsClockReliable(now) {
			year, month, day := now.Date()
			todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
			usage, err := tm.calculateDailyUsage(todayStart, 0)
			if err == nil {
				status.DailyUsageToday = &usage
				dailyRemaining := dailyLimit - usage
				if dailyRemaining < 0 {
					dailyRemaining = 0
				}
				status.DailyRemaining = &dailyRemaining
			}
		}

		return status
	}

	// State: Active (game is running)
	// Build rule context (performs DB I/O and acquires its own locks)
	ctx, err := tm.buildRuleContext(sessionStart)
	if err != nil {
		log.Error().Err(err).Msg("playtime: failed to build rule context for status")
		return &StatusInfo{
			State:                 StateActive.String(),
			SessionActive:         true,
			SessionStarted:        sessionStart,
			SessionDuration:       now.Sub(sessionStart),
			SessionCumulativeTime: cumulativeTime,
		}
	}

	// Calculate session remaining time
	var sessionRemaining time.Duration
	sessionLimit := tm.effectiveSessionLimit()
	dailyLimit := tm.effectiveDailyLimit()

	if sessionLimit > 0 {
		sessionRemaining = sessionLimit - ctx.SessionDuration
		if sessionRemaining < 0 {
			sessionRemaining = 0
		}
	}

	// Calculate daily usage/remaining (only if limits enabled and clock reliable)
	var dailyUsageToday, dailyRemaining *time.Duration
	if dailyLimit > 0 && ctx.ClockReliable {
		dailyUsageToday = &ctx.DailyUsageToday
		remaining := dailyLimit - ctx.DailyUsageToday
		if remaining < 0 {
			remaining = 0
		}
		dailyRemaining = &remaining
	}

	// Re-acquire lock to verify session didn't stop while we were unlocked
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.sessionStart.IsZero() || !tm.sessionStart.Equal(sessionStart) {
		// Session stopped while we were calculating - return cooldown/reset state
		return &StatusInfo{
			State:         tm.state.String(),
			SessionActive: false,
		}
	}

	return &StatusInfo{
		State:                 StateActive.String(),
		SessionActive:         true,
		SessionStarted:        sessionStart,
		SessionDuration:       ctx.SessionDuration,
		SessionCumulativeTime: cumulativeTime,
		SessionRemaining:      sessionRemaining,
		CooldownRemaining:     0, // Not in cooldown
		DailyUsageToday:       dailyUsageToday,
		DailyRemaining:        dailyRemaining,
	}
}

// CheckBeforeLaunch checks if launching new media would exceed daily or session limits.
// Returns a reason string (models.PlaytimeLimitReasonDaily or models.PlaytimeLimitReasonSession)
// and a non-nil error when the launch should be blocked:
// - Daily or session limit is already exceeded
// - Remaining time < MinimumViableSession (prevents launching a game that will be immediately killed)
// On success, reason is "" and error is nil.
func (tm *LimitsManager) CheckBeforeLaunch() (string, error) {
	// Whether limits are enforced is decided by the LimitsProvider (global
	// config, possibly overridden by the active profile).
	if !tm.limits.PlaytimeLimitsEnabled() {
		return "", nil
	}

	dailyLimit := tm.limits.DailyLimit()
	sessionLimit := tm.limits.SessionLimit()

	// If no limits configured, allow launch
	if dailyLimit == 0 && sessionLimit == 0 {
		return "", nil
	}

	now := tm.clock.Now()

	// Check daily limit (requires reliable clock)
	var dailyRemaining time.Duration
	if dailyLimit > 0 {
		if !helpers.IsClockReliable(now) {
			log.Warn().
				Int("year", now.Year()).
				Msg("playtime: clock unreliable, skipping daily limit check")
		} else {
			// Calculate today's usage
			year, month, day := now.Date()
			todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())

			usage, err := tm.calculateDailyUsage(todayStart, 0)
			if err != nil {
				return "", fmt.Errorf("failed to check daily usage: %w", err)
			}

			dailyRemaining = dailyLimit - usage

			// Already over daily limit - block immediately
			if dailyRemaining <= 0 {
				log.Warn().
					Dur("usage", usage).
					Dur("limit", dailyLimit).
					Msg("playtime: daily limit already reached, blocking launch")
				return models.PlaytimeLimitReasonDaily,
					fmt.Errorf("daily playtime limit reached (%s / %s)", usage, dailyLimit)
			}

			// Minimum viable session check for daily limit
			if dailyRemaining < MinimumViableSession {
				log.Warn().
					Dur("remaining", dailyRemaining).
					Dur("minimum", MinimumViableSession).
					Msg("playtime: insufficient daily time remaining for viable session, blocking launch")
				return models.PlaytimeLimitReasonDaily, fmt.Errorf(
					"insufficient daily time remaining (%s left, need %s min)",
					dailyRemaining,
					MinimumViableSession,
				)
			}
		}
	}

	// Check session limit (doesn't require clock reliability)
	var sessionRemaining time.Duration
	if sessionLimit > 0 {
		tm.mu.Lock()
		cumulativeTime := tm.sessionCumulativeTime
		tm.mu.Unlock()

		sessionRemaining = sessionLimit - cumulativeTime

		// Already over session limit - block immediately
		if sessionRemaining <= 0 {
			log.Warn().
				Dur("cumulative", cumulativeTime).
				Dur("limit", sessionLimit).
				Msg("playtime: session limit already reached, blocking launch")
			return models.PlaytimeLimitReasonSession,
				fmt.Errorf("session playtime limit reached (%s / %s)", cumulativeTime, sessionLimit)
		}

		// Minimum viable session check for session limit
		if sessionRemaining < MinimumViableSession {
			log.Warn().
				Dur("remaining", sessionRemaining).
				Dur("minimum", MinimumViableSession).
				Msg("playtime: insufficient session time remaining for viable session, blocking launch")
			return models.PlaytimeLimitReasonSession, fmt.Errorf(
				"insufficient session time remaining (%s left, need %s min)",
				sessionRemaining,
				MinimumViableSession,
			)
		}
	}

	log.Debug().
		Dur("daily_remaining", dailyRemaining).
		Dur("session_remaining", sessionRemaining).
		Msg("playtime: pre-launch check passed")

	return "", nil
}

// RestoreSessionFromHistory reconstructs session state from recent MediaHistory entries.
// Must be called after CloseHangingMediaHistory so any crashed sessions are closed first.
// If the most recent session ended within the cooldown window, cumulative session time and
// cooldown state are restored so session limits survive service restarts.
func (tm *LimitsManager) RestoreSessionFromHistory(now time.Time) {
	sessionResetTimeout := tm.cfg.SessionResetTimeout()
	if sessionResetTimeout == 0 {
		return // No cooldown window; session resets on any stop.
	}

	entries, err := tm.db.UserDB.GetMediaHistory(nil, 0, 100)
	if err != nil {
		log.Warn().Err(err).Msg("playtime: failed to query history for session restore")
		return
	}
	if len(entries) == 0 {
		return
	}

	// Most recent entry must be closed (CloseHangingMediaHistory should have handled any open rows).
	mostRecent := entries[0]
	if mostRecent.EndTime == nil {
		return
	}

	// If the last session ended outside the cooldown window, there is nothing to restore.
	sinceLast := now.Sub(*mostRecent.EndTime)
	if sinceLast < 0 {
		sinceLast = 0 // Clock skew safety.
	}
	if sinceLast >= sessionResetTimeout {
		return
	}

	// Walk entries newest-to-oldest, accumulating PlayTime while consecutive
	// inter-game gaps are within the cooldown window.
	var cumulative time.Duration
	prevStart := mostRecent.StartTime
	for i := range entries {
		entry := &entries[i]
		if entry.EndTime == nil {
			break // Unexpected open row; stop here.
		}
		if i > 0 {
			// Gap between the previous (newer) entry's start and this entry's end.
			gap := prevStart.Sub(*entry.EndTime)
			if gap < 0 {
				gap = 0 // Overlap safety.
			}
			if gap >= sessionResetTimeout {
				break // Gap too large; older entries belong to a prior session.
			}
		}
		cumulative += time.Duration(entry.PlayTime) * time.Second
		prevStart = entry.StartTime
	}

	if cumulative == 0 {
		return
	}

	remaining := sessionResetTimeout - sinceLast

	tm.mu.Lock()
	tm.sessionCumulativeTime = cumulative
	tm.lastStopTime = *mostRecent.EndTime
	tm.transitionTo(StateCooldown)
	tm.cooldownTimer = tm.clock.NewTimer(remaining)
	tm.wg.Add(1)
	tm.mu.Unlock()

	go func() {
		defer tm.wg.Done()
		tm.cooldownTimerLoop()
	}()

	log.Info().
		Dur("cumulative", cumulative).
		Dur("remaining_cooldown", remaining).
		Msg("playtime: restored session state from history")
}
