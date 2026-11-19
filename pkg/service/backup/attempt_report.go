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

package backup

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// Statuses and reasons Core reports to the Online service for a scheduled
// remote backup that produced no snapshot. Reasons are short lowercase codes,
// never error text.
const (
	AttemptStatusFailed  = "failed"
	AttemptStatusSkipped = "skipped"

	AttemptReasonBusy              = "busy"
	AttemptReasonOffline           = "offline"
	AttemptReasonNotAvailable      = "not_available"
	AttemptReasonInterrupted       = "interrupted"
	AttemptReasonQuotaExceeded     = "quota_exceeded"
	AttemptReasonRateLimited       = "rate_limited"
	AttemptReasonFilesChanged      = "files_changed"
	AttemptReasonRequiresNewerCore = "requires_newer_core"
	AttemptReasonBackupTooLarge    = "backup_too_large"
	AttemptReasonPayloadTooLarge   = "payload_too_large"
	AttemptReasonMissingObjects    = "missing_objects"
	AttemptReasonServerError       = "server_error"
	AttemptReasonNetworkError      = "network_error"
	AttemptReasonError             = "error"
)

const (
	// The service refuses a report dated further ahead of its clock than
	// this, or older than attemptReportMaxAge, and refuses the heartbeat
	// carrying it with it. Core drops such a report instead of sending it.
	attemptReportMaxFuture = 5 * time.Minute
	attemptReportMaxAge    = 400 * 24 * time.Hour
)

var attemptReasonPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// attemptReport is a backup attempt that produced no snapshot: either one
// waiting to be sent in a heartbeat, or the last one the service recorded.
type attemptReport struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Reason string    `json:"reason"`
}

func (r *attemptReport) equal(other *attemptReport) bool {
	if r == nil || other == nil {
		return r == other
	}
	return r.At.Equal(other.At) && r.Status == other.Status && r.Reason == other.Reason
}

func (r *attemptReport) sendable(now time.Time) bool {
	return !r.At.After(now.Add(attemptReportMaxFuture)) && !r.At.Before(now.Add(-attemptReportMaxAge))
}

// protectionStatus is the service's answer to whether this device is
// protected, as last reported by GET /v1/device/me.
//
//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type protectionStatus struct {
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	State          string     `json:"state"`
	RecoveryPoints int        `json:"recovery_points"`
}

// RemoteFailureAttempt maps a failed scheduled remote backup run to the
// report sent to the service. ok is false when the outcome is not reported:
// a run stopped by shutdown or an unlinked device.
func RemoteFailureAttempt(err error) (status, reason string, ok bool) {
	var busy *BusyError
	var apiErr *APIError
	var urlErr *url.Error
	var netErr net.Error
	switch {
	case err == nil, errors.Is(err, context.Canceled), errors.Is(err, errRemoteUnlinked):
		return "", "", false
	case errors.As(err, &busy):
		return AttemptStatusSkipped, AttemptReasonBusy, true
	case errors.Is(err, errRemoteNotAvailable):
		reason = AttemptReasonNotAvailable
	case errors.Is(err, errRemoteQuotaExceeded):
		reason = AttemptReasonQuotaExceeded
	case errors.Is(err, errRemoteRateLimited):
		reason = AttemptReasonRateLimited
	case errors.Is(err, errRemoteIntegrityRetry):
		reason = AttemptReasonFilesChanged
	case errors.Is(err, errRemoteNewerSchema):
		reason = AttemptReasonRequiresNewerCore
	case errors.Is(err, errRemoteBackupTooLarge):
		reason = AttemptReasonBackupTooLarge
	case errors.Is(err, errRemotePayloadTooLarge):
		reason = AttemptReasonPayloadTooLarge
	case errors.Is(err, errRemoteMissingObjects):
		reason = AttemptReasonMissingObjects
	case errors.As(err, &apiErr) && apiErr.Status >= http.StatusInternalServerError:
		reason = AttemptReasonServerError
	case errors.As(err, &urlErr), errors.As(err, &netErr):
		reason = AttemptReasonNetworkError
	default:
		reason = AttemptReasonError
	}
	return AttemptStatusFailed, reason, true
}

// RecordScheduledAttempt queues a report of a scheduled remote backup that
// failed or was skipped, replacing any earlier one: the service keeps only
// the newest. Nothing is queued against an unreliable clock, whose
// timestamp the service would refuse.
func (m *Manager) RecordScheduledAttempt(status, reason string, at time.Time) {
	if status != AttemptStatusFailed && status != AttemptStatusSkipped {
		log.Warn().Str("status", status).Msg("ignoring unknown backup attempt status")
		return
	}
	if !attemptReasonPattern.MatchString(reason) {
		log.Warn().Str("reason", reason).Msg("ignoring invalid backup attempt reason")
		return
	}
	if !helpers.IsClockReliable(at) {
		return
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	st := m.readStatusLocked()
	st.PendingAttempt = &attemptReport{At: at.UTC().Truncate(time.Second), Status: status, Reason: reason}
	if err := m.writeStatusLocked(&st); err != nil {
		log.Warn().Err(err).Msg("failed to persist backup attempt report")
	}
}

// HasPendingAttempt reports whether a backup attempt report is waiting to be
// sent with the next heartbeat.
func (m *Manager) HasPendingAttempt() bool {
	return m.readStatus().PendingAttempt != nil
}

// clearPendingAttempt removes the pending report. With a non-nil sent, the
// report is removed only if it is still the one that was sent, so a newer
// report queued while a heartbeat was in flight is kept.
func (m *Manager) clearPendingAttempt(sent *attemptReport) {
	statusMu.Lock()
	defer statusMu.Unlock()
	st := m.readStatusLocked()
	if st.PendingAttempt == nil || (sent != nil && !st.PendingAttempt.equal(sent)) {
		return
	}
	st.PendingAttempt = nil
	if err := m.writeStatusLocked(&st); err != nil {
		log.Warn().Err(err).Msg("failed to clear backup attempt report")
	}
}

// pendingAttemptForSend returns the report to attach to a heartbeat, or nil.
// A report the service would refuse for its date is dropped here.
func (m *Manager) pendingAttemptForSend(now time.Time) *attemptReport {
	report := m.readStatus().PendingAttempt
	if report == nil {
		return nil
	}
	if !report.sendable(now) {
		log.Debug().Time("at", report.At).Msg("dropping backup attempt report outside the accepted date range")
		m.clearPendingAttempt(report)
		return nil
	}
	return report
}

// heartbeatWithAttempt sends a heartbeat carrying any pending backup attempt report
// and clears the report once the service accepts it. A report the service
// refuses would block every later heartbeat, so it is dropped and the
// heartbeat resent once without it.
func (m *Manager) heartbeatWithAttempt(ctx context.Context, client *remoteClient) error {
	capabilities := HeartbeatCapabilities(m.cfg)
	report := m.pendingAttemptForSend(time.Now())
	err := client.heartbeat(ctx, capabilities, report)
	if err == nil {
		if report != nil {
			m.clearPendingAttempt(report)
		}
		return nil
	}
	var apiErr *APIError
	if report == nil || !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return err
	}
	log.Warn().Str("status", report.Status).Str("reason", report.Reason).
		Msg("online service refused the backup attempt report; dropping it")
	m.clearPendingAttempt(report)
	return client.heartbeat(ctx, capabilities, nil)
}

func protectionToModel(p *protectionStatus) *models.BackupProtection {
	if p == nil {
		return nil
	}
	out := &models.BackupProtection{State: p.State, RecoveryPoints: p.RecoveryPoints}
	if p.LastSuccessAt != nil {
		out.LastSuccessAt = optionalString(formatTime(*p.LastSuccessAt))
	}
	if p.LastVerifiedAt != nil {
		out.LastVerifiedAt = optionalString(formatTime(*p.LastVerifiedAt))
	}
	return out
}

// attemptSuperseded reports whether a snapshot committed after the attempt
// makes it history: the service keeps the attempt, but it no longer says
// anything about whether the device is protected.
func attemptSuperseded(a *attemptReport, p *protectionStatus) bool {
	return a != nil && p != nil && p.LastSuccessAt != nil && p.LastSuccessAt.After(a.At)
}

func attemptToModel(a *attemptReport) *models.BackupAttempt {
	if a == nil {
		return nil
	}
	return &models.BackupAttempt{At: formatTime(a.At), Status: a.Status, Reason: a.Reason}
}
