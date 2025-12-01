// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

// KNOWN EDGE CASES / TODO:
//
// 1. System Sleep/Hibernate:
//    If a game is running when the system sleeps (laptop lid close, hibernate),
//    using wall-clock time (Now - StartTime) will count sleep hours as playtime.
//    Fix: Detect OS sleep/wake events and pause timers, OR ensure monotonic clock
//    excludes suspension time (most OS wall clocks include it).
//
// 2. Time Zone Manipulation:
//    A user could change the system clock backward (e.g., set time to yesterday)
//    to bypass daily limits or extend session time.
//    Fix: If online, verify time against NTP server. If offline, detect large
//    backward time jumps using monotonic uptime and invalidate suspicious sessions.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
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

// LimitsManager enforces time limits and warnings for gameplay sessions.
type LimitsManager struct {
	sessionStart          time.Time
	sessionStartMono      time.Time
	lastStopTime          time.Time
	platform              platforms.Platform
	clock                 clockwork.Clock
	ctx                   context.Context
	cooldownTimer         clockwork.Timer
	warningsGiven         map[time.Duration]bool
	db                    *database.Database
	notificationsSend     chan<- models.Notification
	cfg                   *config.Instance
	cancel                context.CancelFunc
	state                 SessionState
	sessionResetTimeout   time.Duration
	sessionCumulativeTime time.Duration
	subscriptionID        int
	mu                    syncutil.Mutex
	enabledMu             syncutil.Mutex
	sessionStartReliable  bool
	enabled               bool
}

// NewLimitsManager creates a new LimitsManager instance.
func NewLimitsManager(
	db *database.Database,
	platform platforms.Platform,
	cfg *config.Instance,
	clock clockwork.Clock,
) *LimitsManager {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Get session reset timeout from config
	// 0 means disabled (session never resets)
	// Non-zero positive duration means reset after that idle time
	sessionResetTimeout := cfg.SessionResetTimeout()

	return &LimitsManager{
		state:               StateReset,
		db:                  db,
		platform:            platform,
		cfg:                 cfg,
		clock:               clock,
		ctx:                 ctx,
		cancel:              cancel,
		warningsGiven:       make(map[time.Duration]bool),
		sessionResetTimeout: sessionResetTimeout,
		enabled:             false, // Start disabled, caller must enable
	}
}

// Broker is the interface for subscribing to notifications.
type Broker interface {
	Subscribe(bufferSize int) (<-chan models.Notification, int)
	Unsubscribe(id int)
}

// Start begins monitoring for time limit enforcement.
// It subscribes to the broker to listen for media.started and media.stopped events.
func (tm *LimitsManager) Start(broker Broker, notificationsSend chan<- models.Notification) {
	tm.mu.Lock()
	tm.notificationsSend = notificationsSend
	tm.mu.Unlock()

	// Subscribe to broker for media.started and media.stopped events
	notifChan, subID := broker.Subscribe(10)
	tm.subscriptionID = subID

	go tm.handleNotifications(notifChan, broker)
}

// Stop shuts down the LimitsManager.
func (tm *LimitsManager) Stop() {
	tm.cancel()
}

// SetEnabled enables or disables limit enforcement at runtime.
// When disabling, resets the session state completely (clears cooldown and cumulative time).
// When re-enabling, session starts fresh but daily usage from history is still enforced.
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

		if tm.state != StateReset {
			log.Info().Msg("playtime: limits disabled, resetting session state")
			tm.transitionTo(StateReset)
			tm.sessionStart = time.Time{}
			tm.sessionStartMono = time.Time{}
			tm.sessionCumulativeTime = 0
			tm.lastStopTime = time.Time{}
			tm.sessionStartReliable = false
			tm.warningsGiven = make(map[time.Duration]bool)
		}
		tm.mu.Unlock()
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
			}

		case <-tm.ctx.Done():
			return
		}
	}
}

// OnMediaStarted handles media.started events and begins time tracking.
func (tm *LimitsManager) OnMediaStarted() {
	if !tm.cfg.PlaytimeLimitsEnabled() {
		return
	}

	tm.mu.Lock()
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
	tm.mu.Unlock()

	if !tm.sessionStartReliable {
		log.Warn().
			Int("year", now.Year()).
			Msg("playtime: session started with unreliable clock - daily limits disabled for this session")
	}

	// Start the check loop
	go tm.checkLoop()
}

// OnMediaStopped handles media.stopped events and stops time tracking.
func (tm *LimitsManager) OnMediaStopped() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.state != StateActive {
		// Only stop if we're actually active
		return
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

	// Start cooldown timer if timeout is configured
	if tm.sessionResetTimeout > 0 {
		tm.cooldownTimer = tm.clock.NewTimer(tm.sessionResetTimeout)
		go tm.cooldownTimerLoop()
	}
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
				Dur("timeout", tm.sessionResetTimeout).
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
// Checks every 30 seconds to ensure warnings 1-minute warnings are not skipped.
func (tm *LimitsManager) checkLoop() {
	ticker := tm.clock.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Immediate check
	tm.checkLimits()

	for {
		select {
		case <-ticker.Chan():
			tm.checkLimits()
		case <-tm.ctx.Done():
			return
		}
	}
}

// checkLimits evaluates all rules and handles warnings/limits.
func (tm *LimitsManager) checkLimits() {
	// Respect both config and runtime enabled state
	if !tm.cfg.PlaytimeLimitsEnabled() || !tm.IsEnabled() {
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

	// Session duration includes:
	// 1. sessionCumulativeTime: Total time from all previous games in this session
	// 2. Current game duration: Time since this game started
	// Note: Go's time.Sub() automatically uses monotonic clock when both Time values have it,
	// which prevents sleep/hibernate from counting as playtime in production.
	// In tests with fake clocks, this uses wall-clock time (which is fine for deterministic tests).
	currentGameDuration := now.Sub(sessionStart)

	tm.mu.Lock()
	cumulativeTime := tm.sessionCumulativeTime
	sessionStartWasReliable := tm.sessionStartReliable
	tm.mu.Unlock()

	// Total session duration = previous games + current game
	sessionDuration := cumulativeTime + currentGameDuration

	// Check if BOTH clocks are trustworthy for daily limit enforcement
	currentClockReliable := helpers.IsClockReliable(now)
	bothClocksReliable := sessionStartWasReliable && currentClockReliable

	var dailyUsage time.Duration
	if bothClocksReliable {
		// Both clocks appear valid - calculate daily usage normally
		year, month, day := now.Date()
		todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())

		// Calculate how much of the current session counts toward today
		sessionStartToday := sessionStart
		if sessionStart.Before(todayStart) {
			// Session started yesterday - only count time after midnight
			sessionStartToday = todayStart
		}
		sessionDurationToday := now.Sub(sessionStartToday)

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
	} else {
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

// calculateDailyUsage queries the database for today's total play time.
func (tm *LimitsManager) calculateDailyUsage(
	todayStart time.Time,
	currentSessionDuration time.Duration,
) (time.Duration, error) {
	// Query media history for today
	// Note: GetMediaHistory uses pagination, so we need to fetch all entries
	var totalUsage time.Duration
	lastID := 0
	limit := 100

	for {
		entries, err := tm.db.UserDB.GetMediaHistory(lastID, limit)
		if err != nil {
			return 0, fmt.Errorf("failed to query media history: %w", err)
		}

		if len(entries) == 0 {
			break
		}

		for i := range entries {
			entry := &entries[i]

			// If entry ended before today, skip it
			if entry.EndTime != nil && entry.EndTime.Before(todayStart) {
				// We've gone past today's entries (ordered DESC by DBID)
				goto done
			}

			// If entry started before today but ended today (or is still running),
			// only count the portion that falls within today
			if entry.StartTime.Before(todayStart) {
				if entry.EndTime != nil {
					// Completed session that spans midnight
					playTimeToday := entry.EndTime.Sub(todayStart)
					if playTimeToday > 0 {
						totalUsage += playTimeToday
					}
				}
				// Note: Current session is handled separately via currentSessionDuration
			} else {
				// Entry started today
				if entry.EndTime == nil {
					// Active session (still running) - skip it.
					// We calculate current session precisely via currentSessionDuration parameter.
					// Including it here would double-count the current session.
					continue
				}
				// Completed session - count full PlayTime
				totalUsage += time.Duration(entry.PlayTime) * time.Second
			}

			lastID = int(entry.DBID)
		}

		if len(entries) < limit {
			// No more entries
			break
		}
	}

done:
	// Add current session duration (already clamped to today in buildRuleContext)
	totalUsage += currentSessionDuration

	return totalUsage, nil
}

// createRules builds the list of active rules based on configuration.
func (tm *LimitsManager) createRules() []Rule {
	rules := make([]Rule, 0, 2)

	if limit := tm.cfg.SessionLimit(); limit > 0 {
		rules = append(rules, &SessionLimitRule{Limit: limit})
	}

	if limit := tm.cfg.DailyLimit(); limit > 0 {
		rules = append(rules, &DailyLimitRule{Limit: limit})
	}

	return rules
}

// handleWarnings checks if warnings should be emitted based on remaining time.
func (tm *LimitsManager) handleWarnings(remaining time.Duration) {
	intervals := tm.cfg.WarningIntervals()

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

// playWarningSound plays an audio warning.
func (tm *LimitsManager) playWarningSound() {
	path, enabled := tm.cfg.LimitSoundPath(helpers.DataDir(tm.platform))
	helpers.PlayConfiguredSound(path, enabled, assets.LimitSound, "limit")
}

// StatusInfo contains current playtime session and limit status.
type StatusInfo struct {
	SessionStarted        time.Time
	State                 string
	SessionDuration       time.Duration
	SessionCumulativeTime time.Duration
	SessionRemaining      time.Duration
	CooldownRemaining     time.Duration
	DailyUsageToday       time.Duration
	DailyRemaining        time.Duration
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
	resetTimeout := tm.sessionResetTimeout
	tm.mu.Unlock()

	now := tm.clock.Now()

	// State: Reset (no session exists)
	if currentState == StateReset {
		return &StatusInfo{
			State:         StateReset.String(),
			SessionActive: false,
		}
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
		var sessionRemaining, dailyRemaining time.Duration
		sessionLimit := tm.cfg.SessionLimit()
		dailyLimit := tm.cfg.DailyLimit()

		if sessionLimit > 0 {
			sessionRemaining = sessionLimit - cumulativeTime
			if sessionRemaining < 0 {
				sessionRemaining = 0
			}
		}

		// For daily remaining, we need to calculate today's total usage
		if dailyLimit > 0 {
			year, month, day := now.Date()
			todayStart := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
			usage, err := tm.calculateDailyUsage(todayStart, 0)
			if err == nil {
				dailyRemaining = dailyLimit - usage
				if dailyRemaining < 0 {
					dailyRemaining = 0
				}
			}
		}

		// Note: sessionStart is zero during cooldown (no current game)
		// Don't include SessionStarted in response - it's not meaningful
		return &StatusInfo{
			State:                 StateCooldown.String(),
			SessionActive:         false,
			SessionStarted:        time.Time{}, // Zero time - will be omitted from API response
			SessionDuration:       cumulativeTime,
			SessionCumulativeTime: cumulativeTime,
			SessionRemaining:      sessionRemaining,
			CooldownRemaining:     cooldownRemaining,
			DailyUsageToday:       0, // Skip during cooldown
			DailyRemaining:        dailyRemaining,
		}
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

	// Calculate session and daily remaining times
	var sessionRemaining, dailyRemaining time.Duration
	sessionLimit := tm.cfg.SessionLimit()
	dailyLimit := tm.cfg.DailyLimit()

	if sessionLimit > 0 {
		sessionRemaining = sessionLimit - ctx.SessionDuration
		if sessionRemaining < 0 {
			sessionRemaining = 0
		}
	}

	if dailyLimit > 0 && ctx.ClockReliable {
		dailyRemaining = dailyLimit - ctx.DailyUsageToday
		if dailyRemaining < 0 {
			dailyRemaining = 0
		}
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
		DailyUsageToday:       ctx.DailyUsageToday,
		DailyRemaining:        dailyRemaining,
	}
}

// CheckBeforeLaunch checks if launching new media would exceed daily or session limits.
// Returns an error if:
// - Daily or session limit is already exceeded
// - Remaining time < MinimumViableSession (prevents launching games that will be immediately killed)
// This implements the preventive check strategy - block launches before they start rather than
// trying to stop games immediately after they launch.
func (tm *LimitsManager) CheckBeforeLaunch() error {
	// Check if limits are enabled (both config and runtime)
	if !tm.cfg.PlaytimeLimitsEnabled() || !tm.IsEnabled() {
		return nil
	}

	dailyLimit := tm.cfg.DailyLimit()
	sessionLimit := tm.cfg.SessionLimit()

	// If no limits configured, allow launch
	if dailyLimit == 0 && sessionLimit == 0 {
		return nil
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
				return fmt.Errorf("failed to check daily usage: %w", err)
			}

			dailyRemaining = dailyLimit - usage

			// Already over daily limit - block immediately
			if dailyRemaining <= 0 {
				log.Warn().
					Dur("usage", usage).
					Dur("limit", dailyLimit).
					Msg("playtime: daily limit already reached, blocking launch")
				return fmt.Errorf("daily playtime limit reached (%s / %s)", usage, dailyLimit)
			}

			// Minimum viable session check for daily limit
			if dailyRemaining < MinimumViableSession {
				log.Warn().
					Dur("remaining", dailyRemaining).
					Dur("minimum", MinimumViableSession).
					Msg("playtime: insufficient daily time remaining for viable session, blocking launch")
				return fmt.Errorf(
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
			return fmt.Errorf("session playtime limit reached (%s / %s)", cumulativeTime, sessionLimit)
		}

		// Minimum viable session check for session limit
		if sessionRemaining < MinimumViableSession {
			log.Warn().
				Dur("remaining", sessionRemaining).
				Dur("minimum", MinimumViableSession).
				Msg("playtime: insufficient session time remaining for viable session, blocking launch")
			return fmt.Errorf(
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

	return nil
}
