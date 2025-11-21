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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// MinReliableYear is the earliest year considered valid for the system clock.
	// Zaparoo Core v2 was released in 2024 - any earlier date indicates an unset clock.
	MinReliableYear = 2024
)

// LimitsManager enforces time limits and warnings for gameplay sessions.
type LimitsManager struct {
	sessionStart         time.Time
	platform             platforms.Platform
	clock                clockwork.Clock
	ctx                  context.Context
	db                   *database.Database
	cfg                  *config.Instance
	cancel               context.CancelFunc
	notificationsSend    chan<- models.Notification
	warningsGiven        map[time.Duration]bool
	subscriptionID       int
	mu                   sync.Mutex
	sessionStartReliable bool
	enabledMu            sync.Mutex
	enabled              bool
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
	return &LimitsManager{
		db:            db,
		platform:      platform,
		cfg:           cfg,
		clock:         clock,
		ctx:           ctx,
		cancel:        cancel,
		warningsGiven: make(map[time.Duration]bool),
		enabled:       false, // Start disabled, caller must enable
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
// If disabling mid-session, stops the current session tracking.
func (tm *LimitsManager) SetEnabled(enabled bool) {
	tm.enabledMu.Lock()
	tm.enabled = enabled
	tm.enabledMu.Unlock()

	// If disabling mid-session, clean up the session
	if !enabled && tm.isSessionActive() {
		log.Warn().Msg("playtime: limits disabled mid-session, stopping tracking")
		tm.OnMediaStopped()
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
	tm.sessionStart = now
	tm.sessionStartReliable = isClockReliable(now)
	tm.warningsGiven = make(map[time.Duration]bool)
	tm.mu.Unlock()

	if tm.sessionStartReliable {
		log.Info().Msg("playtime: session started, beginning time monitoring")
	} else {
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

	if tm.sessionStart.IsZero() {
		return
	}

	log.Info().Msg("playtime: session stopped, ending time monitoring")
	tm.sessionStart = time.Time{}
	tm.sessionStartReliable = false
	tm.warningsGiven = make(map[time.Duration]bool)
}

// checkLoop runs periodic checks for time limits.
func (tm *LimitsManager) checkLoop() {
	ticker := tm.clock.NewTicker(1 * time.Minute)
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

	// Build rule context
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
		// Time limit reached - stop the game
		log.Warn().Str("reason", reason).Msg("playtime: time limit reached, stopping game")
		notifications.PlaytimeLimitReached(tm.notificationsSend, models.PlaytimeLimitReachedParams{
			Reason: reason,
		})
		tm.playWarningSound()

		if err := tm.platform.StopActiveLauncher(platforms.StopForPreemption); err != nil {
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

// isClockReliable checks if the system clock appears to be set correctly.
// Returns false if the clock is clearly wrong (e.g., year < 2024).
// This handles MiSTer's lack of RTC chip - clock often resets to epoch on boot.
func isClockReliable(t time.Time) bool {
	return t.Year() >= MinReliableYear
}

// buildRuleContext creates a RuleContext from current state.
func (tm *LimitsManager) buildRuleContext(sessionStart time.Time) (RuleContext, error) {
	now := tm.clock.Now()

	// Session duration is always valid - uses monotonic clock via time.Sub()
	// This is immune to NTP jumps, clock resets, and manual time changes.
	sessionDuration := now.Sub(sessionStart)

	// Check if BOTH clocks are trustworthy for daily limit enforcement
	tm.mu.Lock()
	sessionStartWasReliable := tm.sessionStartReliable
	tm.mu.Unlock()

	currentClockReliable := isClockReliable(now)
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
	if !enabled {
		return
	}

	if path == "" {
		// Use embedded default sound
		if err := audio.PlayWAVBytes(assets.LimitSound); err != nil {
			log.Warn().Err(err).Msg("playtime: error playing limit sound")
		}
	} else {
		// Use custom sound file
		if err := audio.PlayFile(path); err != nil {
			log.Warn().Str("path", path).Err(err).Msg("playtime: error playing custom limit sound")
		}
	}
}

// StatusInfo contains current playtime session and limit status.
type StatusInfo struct {
	SessionStarted   time.Time
	WarningsGiven    []time.Duration
	SessionDuration  time.Duration
	SessionRemaining time.Duration
	DailyUsageToday  time.Duration
	DailyRemaining   time.Duration
	SessionActive    bool
	ClockReliable    bool
}

// GetStatus returns the current playtime session and limit status.
// Returns nil if no session is active.
func (tm *LimitsManager) GetStatus() *StatusInfo {
	// Snapshot session state under lock, then release to avoid deadlock in buildRuleContext
	tm.mu.Lock()
	sessionStart := tm.sessionStart
	if sessionStart.IsZero() {
		tm.mu.Unlock()
		return &StatusInfo{
			SessionActive: false,
			ClockReliable: isClockReliable(tm.clock.Now()),
		}
	}
	tm.mu.Unlock()

	// Build rule context (performs DB I/O and acquires its own locks)
	ctx, err := tm.buildRuleContext(sessionStart)
	if err != nil {
		log.Error().Err(err).Msg("playtime: failed to build rule context for status")
		return &StatusInfo{
			SessionActive:   true,
			SessionStarted:  sessionStart,
			SessionDuration: tm.clock.Now().Sub(sessionStart),
			ClockReliable:   false,
		}
	}

	// Calculate session and daily remaining times separately
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

	// Re-acquire lock to safely read warningsGiven map
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Verify session didn't stop while we were unlocked
	if tm.sessionStart.IsZero() || !tm.sessionStart.Equal(sessionStart) {
		return &StatusInfo{
			SessionActive: false,
			ClockReliable: ctx.ClockReliable,
		}
	}

	// Convert warningsGiven map to slice and sort for consistent ordering
	warnings := make([]time.Duration, 0, len(tm.warningsGiven))
	for interval := range tm.warningsGiven {
		warnings = append(warnings, interval)
	}
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i] > warnings[j] // Descending order (largest first)
	})

	return &StatusInfo{
		SessionActive:    true,
		SessionStarted:   sessionStart,
		SessionDuration:  ctx.SessionDuration,
		SessionRemaining: sessionRemaining,
		DailyUsageToday:  ctx.DailyUsageToday,
		DailyRemaining:   dailyRemaining,
		ClockReliable:    ctx.ClockReliable,
		WarningsGiven:    warnings,
	}
}
