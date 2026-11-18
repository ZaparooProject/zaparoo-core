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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heartbeatRecorder is a test server that records the backup attempt report
// carried by each heartbeat and answers with the configured status.
type heartbeatRecorder struct {
	me       remoteDeviceMeResponse
	reports  []map[string]any
	statuses []int
	present  []bool
	mu       syncutil.Mutex
	calls    int
}

func (h *heartbeatRecorder) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
			var body map[string]any
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			h.mu.Lock()
			report, ok := body["last_backup_attempt"].(map[string]any)
			h.present = append(h.present, ok)
			h.reports = append(h.reports, report)
			status := http.StatusNoContent
			if h.calls < len(h.statuses) {
				status = h.statuses[h.calls]
			}
			h.calls++
			h.mu.Unlock()
			if status == http.StatusBadRequest {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"Request validation failed"}}`))
				return
			}
			w.WriteHeader(status)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
			h.mu.Lock()
			me := h.me
			h.mu.Unlock()
			writeJSON(t, w, me)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newAttemptTestManager(t *testing.T, rec *heartbeatRecorder) *Manager {
	t.Helper()
	env := newBackupTestEnv(t, platformids.Mister)
	server := httptest.NewServer(rec.handler(t))
	t.Cleanup(server.Close)
	require.NoError(t, env.Manager.cfg.SetOnlineBaseURL(server.URL))
	configureRemoteTestAuth(t, env.Manager, server.URL)
	return env.Manager
}

func TestRemoteFailureAttempt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err        error
		name       string
		wantStatus string
		wantReason string
		wantOK     bool
	}{
		{name: "nil", err: nil},
		{name: "canceled", err: fmt.Errorf("run: %w", context.Canceled)},
		{name: "unlinked", err: errRemoteUnlinked},
		{
			name: "busy", err: &BusyError{},
			wantStatus: AttemptStatusSkipped, wantReason: AttemptReasonBusy, wantOK: true,
		},
		{name: "not available", err: errRemoteNotAvailable, wantReason: AttemptReasonNotAvailable},
		{name: "quota", err: &remoteQuotaError{}, wantReason: AttemptReasonQuotaExceeded},
		{name: "rate limited", err: &remoteRateLimitedError{}, wantReason: AttemptReasonRateLimited},
		{name: "integrity", err: errRemoteIntegrityRetry, wantReason: AttemptReasonFilesChanged},
		{name: "newer schema", err: errRemoteNewerSchema, wantReason: AttemptReasonRequiresNewerCore},
		{
			name: "too many files", err: &APIError{Status: 413, sentinel: errRemoteBackupTooLarge},
			wantReason: AttemptReasonBackupTooLarge,
		},
		{name: "payload", err: errRemotePayloadTooLarge, wantReason: AttemptReasonPayloadTooLarge},
		{name: "missing objects", err: errRemoteMissingObjects, wantReason: AttemptReasonMissingObjects},
		{name: "server error", err: &APIError{Status: 503}, wantReason: AttemptReasonServerError},
		{
			name: "network", err: fmt.Errorf("contacting: %w", &url.Error{Op: "Post", Err: errors.New("refused")}),
			wantReason: AttemptReasonNetworkError,
		},
		{name: "net error", err: &net.DNSError{Err: "no such host"}, wantReason: AttemptReasonNetworkError},
		{name: "client error", err: &APIError{Status: 409}, wantReason: AttemptReasonError},
		{name: "other", err: errors.New("disk read failed"), wantReason: AttemptReasonError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, reason, ok := RemoteFailureAttempt(tt.err)
			if tt.wantReason != "" && tt.wantStatus == "" {
				tt.wantStatus, tt.wantOK = AttemptStatusFailed, true
			}
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantReason, reason)
			if ok {
				assert.Regexp(t, attemptReasonPattern, reason)
			}
		})
	}
}

func TestRecordScheduledAttemptValidates(t *testing.T) {
	m := newAttemptTestManager(t, &heartbeatRecorder{})
	now := time.Now()

	m.RecordScheduledAttempt("succeeded", AttemptReasonBusy, now)
	m.RecordScheduledAttempt(AttemptStatusFailed, "Disk Full: /media", now)
	m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonError, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	assert.False(t, m.HasPendingAttempt())

	m.RecordScheduledAttempt(AttemptStatusSkipped, AttemptReasonBusy, now)
	m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonQuotaExceeded, now.Add(time.Second))
	pending := m.readStatus().PendingAttempt
	require.NotNil(t, pending)
	assert.Equal(t, AttemptStatusFailed, pending.Status)
	assert.Equal(t, AttemptReasonQuotaExceeded, pending.Reason)
}

func TestSendHeartbeatCarriesAndClearsPendingAttempt(t *testing.T) {
	rec := &heartbeatRecorder{me: remoteDeviceMeResponse{ID: "d", BackupActive: true}}
	m := newAttemptTestManager(t, rec)
	at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonQuotaExceeded, at)

	require.NoError(t, m.SendHeartbeat(context.Background()))
	require.Len(t, rec.reports, 1)
	require.True(t, rec.present[0])
	assert.Equal(t, AttemptStatusFailed, rec.reports[0]["status"])
	assert.Equal(t, AttemptReasonQuotaExceeded, rec.reports[0]["reason"])
	assert.Equal(t, at.Format(time.RFC3339), rec.reports[0]["at"])
	assert.False(t, m.HasPendingAttempt())

	require.NoError(t, m.SendCapabilityHeartbeat(context.Background()))
	require.Len(t, rec.present, 2)
	assert.False(t, rec.present[1], "no report key without a pending report")
}

func TestSendHeartbeatKeepsReportFailedToSend(t *testing.T) {
	rec := &heartbeatRecorder{statuses: []int{http.StatusInternalServerError}}
	m := newAttemptTestManager(t, rec)
	m.RecordScheduledAttempt(AttemptStatusSkipped, AttemptReasonOffline, time.Now())

	require.Error(t, m.SendCapabilityHeartbeat(context.Background()))
	assert.True(t, m.HasPendingAttempt())
	assert.Equal(t, 1, rec.calls)
}

func TestSendHeartbeatDropsRefusedReportAndResends(t *testing.T) {
	rec := &heartbeatRecorder{statuses: []int{http.StatusBadRequest}}
	m := newAttemptTestManager(t, rec)
	m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonError, time.Now())

	require.NoError(t, m.SendCapabilityHeartbeat(context.Background()))
	require.Equal(t, 2, rec.calls)
	assert.True(t, rec.present[0])
	assert.False(t, rec.present[1], "resend must not carry the refused report")
	assert.False(t, m.HasPendingAttempt())
}

func TestSendHeartbeatRefusedWithoutReportIsNotRetried(t *testing.T) {
	rec := &heartbeatRecorder{statuses: []int{http.StatusBadRequest}}
	m := newAttemptTestManager(t, rec)

	require.Error(t, m.SendCapabilityHeartbeat(context.Background()))
	assert.Equal(t, 1, rec.calls)
}

func TestSendHeartbeatDropsOutOfRangeReport(t *testing.T) {
	for name, at := range map[string]time.Time{
		"future": time.Now().Add(time.Hour),
		"old":    time.Now().Add(-401 * 24 * time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			rec := &heartbeatRecorder{}
			m := newAttemptTestManager(t, rec)
			m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonError, at)
			require.True(t, m.HasPendingAttempt())

			require.NoError(t, m.SendCapabilityHeartbeat(context.Background()))
			require.Equal(t, 1, rec.calls)
			assert.False(t, rec.present[0])
			assert.False(t, m.HasPendingAttempt())
		})
	}
}

func TestClearPendingAttemptKeepsNewerReport(t *testing.T) {
	m := newAttemptTestManager(t, &heartbeatRecorder{})
	now := time.Now()
	m.RecordScheduledAttempt(AttemptStatusSkipped, AttemptReasonBusy, now)
	sent := m.readStatus().PendingAttempt
	require.NotNil(t, sent)

	m.RecordScheduledAttempt(AttemptStatusFailed, AttemptReasonServerError, now.Add(2*time.Second))
	m.clearPendingAttempt(sent)
	pending := m.readStatus().PendingAttempt
	require.NotNil(t, pending)
	assert.Equal(t, AttemptReasonServerError, pending.Reason)

	m.clearPendingAttempt(nil)
	assert.False(t, m.HasPendingAttempt())
}

func TestDeviceMeProtectionPersistedAndCleared(t *testing.T) {
	success := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	attemptAt := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	rec := &heartbeatRecorder{me: remoteDeviceMeResponse{
		ID: "d", BackupActive: true,
		BackupProtection: &protectionStatus{State: "failed", LastSuccessAt: &success, RecoveryPoints: 4},
		LastBackupAttempt: &attemptReport{
			At: attemptAt, Status: AttemptStatusFailed, Reason: AttemptReasonQuotaExceeded,
		},
	}}
	m := newAttemptTestManager(t, rec)

	_, err := m.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	remote := m.Status().Remote
	require.NotNil(t, remote.Protection)
	assert.Equal(t, "failed", remote.Protection.State)
	assert.Equal(t, 4, remote.Protection.RecoveryPoints)
	require.NotNil(t, remote.Protection.LastSuccessAt)
	assert.Equal(t, formatTime(success), *remote.Protection.LastSuccessAt)
	assert.Nil(t, remote.Protection.LastVerifiedAt)
	require.NotNil(t, remote.LastAttempt)
	assert.Equal(t, formatTime(attemptAt), remote.LastAttempt.At)
	assert.Equal(t, AttemptReasonQuotaExceeded, remote.LastAttempt.Reason)

	// A snapshot committed after the attempt supersedes it.
	later := attemptAt.Add(time.Hour)
	rec.mu.Lock()
	rec.me.BackupProtection = &protectionStatus{State: "protected", LastSuccessAt: &later, RecoveryPoints: 5}
	rec.mu.Unlock()
	_, err = m.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	remote = m.Status().Remote
	require.NotNil(t, remote.Protection)
	assert.Equal(t, "protected", remote.Protection.State)
	assert.Nil(t, remote.LastAttempt)

	// An older service that sends neither field clears them.
	rec.mu.Lock()
	rec.me = remoteDeviceMeResponse{ID: "d", BackupActive: true}
	rec.mu.Unlock()
	_, err = m.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	remote = m.Status().Remote
	assert.Nil(t, remote.Protection)
	assert.Nil(t, remote.LastAttempt)
}

func TestUnlinkClearsAttemptAndProtection(t *testing.T) {
	rec := &heartbeatRecorder{me: remoteDeviceMeResponse{
		ID: "d", BackupActive: true, BackupProtection: &protectionStatus{State: "protected"},
	}}
	m := newAttemptTestManager(t, rec)
	_, err := m.RefreshRemoteAvailability(context.Background())
	require.NoError(t, err)
	m.RecordScheduledAttempt(AttemptStatusSkipped, AttemptReasonBusy, time.Now())

	m.MarkRemoteUnlinked()
	st := m.readStatus()
	assert.Nil(t, st.PendingAttempt)
	assert.Nil(t, st.RemoteProtection)
	assert.Nil(t, st.RemoteLastAttempt)
}

func TestRecoverInterruptedRunsReportsOnlyScheduledRuns(t *testing.T) {
	for _, runType := range []string{RemoteBackupTypeScheduled, RemoteBackupTypeManual} {
		t.Run(runType, func(t *testing.T) {
			m := newAttemptTestManager(t, &heartbeatRecorder{})
			require.NoError(t, m.writeRemoteStatus(&statusEntry{
				LastRunAt: formatTime(time.Now()), LastStatus: StatusRunning, RunType: runType,
			}))

			m.RecoverInterruptedRuns()
			st := m.readStatus()
			assert.Equal(t, StatusFailed, st.Remote.LastStatus)
			assert.Empty(t, st.Remote.RunType)
			if runType == RemoteBackupTypeManual {
				assert.Nil(t, st.PendingAttempt)
				return
			}
			require.NotNil(t, st.PendingAttempt)
			assert.Equal(t, AttemptStatusFailed, st.PendingAttempt.Status)
			assert.Equal(t, AttemptReasonInterrupted, st.PendingAttempt.Reason)
		})
	}
}

func TestRunRemoteSuccessClearsReportAndRefreshesProtection(t *testing.T) {
	env := newBackupTestEnv(t, platformids.Mister)
	var requests atomic.Int32
	inner := newRemoteBackupSuccessServer(t, &requests)
	defer inner.Close()
	var meCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups" {
			// A report queued while the run was uploading, after the run's
			// own heartbeat went out.
			env.Manager.RecordScheduledAttempt(AttemptStatusSkipped, AttemptReasonBusy, time.Now())
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/device/me" {
			// The first check runs before the upload; the one after it sees
			// the protection the new snapshot produced.
			state := "failed"
			if meCalls.Add(1) > 1 {
				state = "protected"
			}
			writeJSON(t, w, remoteDeviceMeResponse{
				ID: "d", BackupActive: true, BackupProtection: &protectionStatus{State: state, RecoveryPoints: 1},
			})
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	configureRemoteTestAuth(t, env.Manager, server.URL)

	_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeManual)
	require.NoError(t, err)
	assert.False(t, env.Manager.HasPendingAttempt())
	assert.Equal(t, int32(2), meCalls.Load())
	remote := env.Manager.Status().Remote
	require.NotNil(t, remote.Protection)
	assert.Equal(t, "protected", remote.Protection.State)
}
