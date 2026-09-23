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
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

// Play-history sync uploads MediaHistory sessions to the Zaparoo API over
// the device token, cursored on the server-side watermark (the newest
// core_updated_at it has stored). The server upserts by session UUID, so
// bulk first import, steady state, retries, and retroactive timestamp
// healing all share this one idempotent path. See the API repo's
// docs/plans/zaparoo-open-api.md.
const (
	playSyncBatchSize = 500
	// playSyncMaxBatches bounds one sync pass as a runaway backstop; a full
	// year of heavy play is far below this many sessions. Anything left
	// syncs on the next pass.
	playSyncMaxBatches = 200
)

var errPlaySyncDisabled = errors.New("play history sync is disabled")

// IsPlaySyncDisabledError reports an expected opt-out or mid-sync disable.
// Background schedulers use this to keep intentional inactivity quiet while
// surfacing real upload failures at warning level.
func IsPlaySyncDisabledError(err error) bool {
	return errors.Is(err, errPlaySyncDisabled)
}

// PlaySyncInfo summarizes one play-history sync pass.
type PlaySyncInfo struct {
	Uploaded int
	Batches  int
	// Refused counts sessions the account would not accept. They are worked
	// around rather than retried forever, and reported so they can be
	// repaired.
	Refused int
	// NewlyRefused counts the refusals this process had not seen before. Only
	// these are worth telling the user about again.
	NewlyRefused int
}

// RefusedSessions remembers the play sessions the account has refused, for as
// long as the process runs.
//
// A refused session is never marked synced, so without this every pass sent it
// again, was refused again, spent up to nine extra requests finding it again,
// and raised the same inbox warning again. It is deliberately not persisted: a
// restart retries each one once, which is how a session repaired locally or a
// rule relaxed on the server gets through. A session edited since it was
// refused has a new UpdatedAt, so it is retried straight away.
//
// The scheduler builds a fresh Manager for every pass, so this lives outside
// it and is handed in.
type RefusedSessions struct {
	rows map[refusedSessionKey]struct{}
	mu   syncutil.Mutex
}

type refusedSessionKey struct {
	dbid      int64
	updatedAt int64
}

// NewRefusedSessions returns an empty set.
func NewRefusedSessions() *RefusedSessions {
	return &RefusedSessions{rows: make(map[refusedSessionKey]struct{})}
}

func (r *RefusedSessions) has(dbid int64, updatedAt time.Time) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.rows[refusedSessionKey{dbid: dbid, updatedAt: updatedAt.UnixNano()}]
	return ok
}

// add records a refusal and reports whether it had not been seen before. A nil
// set remembers nothing, so every refusal is new.
func (r *RefusedSessions) add(dbid int64, updatedAt time.Time) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := refusedSessionKey{dbid: dbid, updatedAt: updatedAt.UnixNano()}
	if _, ok := r.rows[key]; ok {
		return false
	}
	r.rows[key] = struct{}{}
	return true
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remotePlaySessionItem struct {
	ProfileID     *string                 `json:"profile_id,omitempty"`
	EndedAt       *time.Time              `json:"ended_at,omitempty"`
	MediaIdentity *database.MediaIdentity `json:"media_identity,omitempty"`
	SessionUUID   string                  `json:"session_uuid"`
	SystemID      string                  `json:"system_id"`
	SystemName    string                  `json:"system_name"`
	LauncherID    string                  `json:"launcher_id"`
	MediaPath     string                  `json:"media_path"`
	MediaName     string                  `json:"media_name"`
	ClockSource   string                  `json:"clock_source"`
	Tags          []string                `json:"tags,omitempty"`
	StartedAt     time.Time               `json:"started_at"`
	CoreUpdatedAt time.Time               `json:"core_updated_at"`
	PlayTimeSecs  int                     `json:"play_time_secs"`
	ClockReliable bool                    `json:"clock_reliable"`
	IsDeleted     bool                    `json:"is_deleted"`
}

type remotePlaySessionRequest struct {
	Sessions []remotePlaySessionItem `json:"sessions"`
}

type remotePlaySessionResponse struct {
	Watermark *time.Time `json:"watermark"`
	Accepted  int64      `json:"accepted"`
}

type remotePlayWatermarkResponse struct {
	Watermark *time.Time `json:"watermark"`
}

// mediaHistoryToRemote converts a local MediaHistory row to its wire shape.
func mediaHistoryToRemote(entry *database.MediaHistoryEntry) remotePlaySessionItem {
	return remotePlaySessionItem{
		SessionUUID:   entry.ID,
		ProfileID:     entry.ProfileID,
		MediaIdentity: entry.MediaIdentity,
		SystemID:      entry.SystemID,
		SystemName:    entry.SystemName,
		LauncherID:    entry.LauncherID,
		MediaPath:     entry.MediaPath,
		MediaName:     entry.MediaName,
		StartedAt:     entry.StartTime.UTC(),
		EndedAt:       entry.EndTime,
		PlayTimeSecs:  entry.PlayTime,
		ClockSource:   entry.ClockSource,
		ClockReliable: entry.ClockReliable,
		Tags:          entry.Tags,
		IsDeleted:     entry.IsDeleted,
		CoreUpdatedAt: entry.UpdatedAt.UTC(),
	}
}

func (c *remoteClient) playSessionWatermark(ctx context.Context) (*time.Time, error) {
	var resp remotePlayWatermarkResponse
	if err := c.retryRateLimited(ctx, func() error {
		resp = remotePlayWatermarkResponse{}
		return c.doJSON(ctx, http.MethodGet, "/v1/device/play-sessions/watermark", nil, &resp)
	}); err != nil {
		return nil, err
	}
	return resp.Watermark, nil
}

func (c *remoteClient) uploadPlaySessions(
	ctx context.Context, sessions []remotePlaySessionItem,
) (remotePlaySessionResponse, error) {
	var resp remotePlaySessionResponse
	req := remotePlaySessionRequest{Sessions: sessions}
	if err := c.retryRateLimited(ctx, func() error {
		resp = remotePlaySessionResponse{}
		return c.doJSON(ctx, http.MethodPost, "/v1/device/play-sessions", &req, &resp)
	}); err != nil {
		return remotePlaySessionResponse{}, err
	}
	return resp, nil
}

// maxRefusedPerPass bounds how many individually refused sessions one pass
// will isolate. A handful is malformed local data worth working around; more
// than that is systemic, and hunting each one costs the server a request.
const maxRefusedPerPass = 16

// refusedSessionIndexes reads the session positions a validation refusal
// names. The server reports refused fields keyed as they appear in the
// request, e.g. "Sessions[3].MediaName", so the batch position can be read
// straight out of the key without Core knowing what the rule was.
func refusedSessionIndexes(err error, batchLen int) []int {
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.Status != http.StatusBadRequest {
		return nil
	}
	seen := make(map[int]struct{}, len(apiErr.Fields))
	for field := range apiErr.Fields {
		_, rest, found := strings.Cut(field, "[")
		if !found {
			continue
		}
		digits, _, found := strings.Cut(rest, "]")
		if !found {
			continue
		}
		index, convErr := strconv.Atoi(digits)
		if convErr != nil || index < 0 || index >= batchLen {
			continue
		}
		seen[index] = struct{}{}
	}
	indexes := make([]int, 0, len(seen))
	for index := range seen {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

// isValidationRefusal reports a response that refused the request's contents
// rather than failing for a transport or availability reason. Only those are
// worth isolating: retrying the same rows cannot make them acceptable.
func isValidationRefusal(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Status == http.StatusBadRequest
}

// uploadWithoutRefused uploads sessions, working around any the server refuses
// rather than letting them stop the pass.
//
// A refusal fails the whole request, so one unacceptable row used to stop
// play-history sync for the device permanently: the same batch was retried
// forever and nothing after it ever reached the account. The refused positions
// are read from the response where the server names them, and found by halving
// the batch where it does not. Everything else then uploads, and each refused
// session is reported so it can be repaired rather than silently dropped.
func (c *remoteClient) uploadWithoutRefused(
	ctx context.Context, sessions []remotePlaySessionItem, offset int, refused *[]int,
) (remotePlaySessionResponse, error) {
	resp, err := c.uploadPlaySessions(ctx, sessions)
	if err == nil || !isValidationRefusal(err) {
		return resp, err
	}
	if len(sessions) == 1 {
		if len(*refused) >= maxRefusedPerPass {
			return remotePlaySessionResponse{}, err
		}
		*refused = append(*refused, offset)
		return remotePlaySessionResponse{}, nil
	}

	// Split on the positions the server named when it named any, and down the
	// middle when it did not.
	split := len(sessions) / 2
	if named := refusedSessionIndexes(err, len(sessions)); len(named) > 0 && named[0] > 0 {
		split = named[0]
	}

	head, headErr := c.uploadWithoutRefused(ctx, sessions[:split], offset, refused)
	if headErr != nil {
		return remotePlaySessionResponse{}, headErr
	}
	tail, tailErr := c.uploadWithoutRefused(ctx, sessions[split:], offset+split, refused)
	if tailErr != nil {
		return remotePlaySessionResponse{}, tailErr
	}
	combined := remotePlaySessionResponse{Accepted: head.Accepted + tail.Accepted}
	combined.Watermark = tail.Watermark
	if combined.Watermark == nil {
		combined.Watermark = head.Watermark
	}
	return combined, nil
}

// notifyRefusedSessions tells the user that some play sessions cannot be
// uploaded. Without it the only trace is a log line, and a device can go on
// failing to record history with nothing to notice.
func (m *Manager) notifyRefusedSessions(refused int) {
	if m.inbox == nil || refused == 0 {
		return
	}
	body := fmt.Sprintf(
		"%d play session(s) could not be added to your Zaparoo Online history because the "+
			"account would not accept them. The rest of your history is syncing normally. "+
			"See the log for which sessions were affected.",
		refused,
	)
	if addErr := m.inbox.Add(
		"Some play sessions were not synced",
		inboxservice.WithBody(body),
		inboxservice.WithSeverity(inboxservice.SeverityWarning),
		inboxservice.WithCategory(inboxservice.CategoryPlayHistorySessionsRefused),
	); addErr != nil {
		log.Warn().Err(addErr).Msg("failed to add refused play sessions inbox message")
	}
}

// SyncPlayHistory uploads every session updated since the server's
// watermark. The first call after linking is the bulk import of the whole
// local history; afterwards each pass sends only what changed. A pass is
// cheap when nothing changed: one watermark GET and one empty local query.
func (m *Manager) SyncPlayHistory(ctx context.Context) (PlaySyncInfo, error) {
	if !m.cfg.PlaytimeSyncEnabled() {
		return PlaySyncInfo{}, errPlaySyncDisabled
	}
	client, err := m.newPlaytimeRemoteClient()
	if err != nil {
		return PlaySyncInfo{}, err
	}

	watermark, err := client.playSessionWatermark(ctx)
	if err != nil {
		return PlaySyncInfo{}, err
	}
	// Clear local acknowledgements beyond server state (or all of them for a
	// fresh server-side device). Batch selection then walks every unsynced row
	// from the local beginning, including unreliable-clock rows older than the
	// server watermark.
	if resetErr := m.database.UserDB.ResetMediaHistorySyncAfter(watermark); resetErr != nil {
		return PlaySyncInfo{}, fmt.Errorf("resetting media history sync state: %w", resetErr)
	}
	cursor := time.Time{}
	var cursorDBID int64

	info := PlaySyncInfo{}
	for range playSyncMaxBatches {
		if !m.cfg.PlaytimeSyncEnabled() {
			return info, errPlaySyncDisabled
		}
		batch, batchErr := m.database.UserDB.GetMediaHistorySyncBatch(cursor, cursorDBID, playSyncBatchSize)
		if batchErr != nil {
			return info, fmt.Errorf("reading media history sync batch: %w", batchErr)
		}
		if len(batch) == 0 {
			break
		}

		// Sessions this process has already seen refused are left out rather
		// than sent to be refused again. They stay unsynced, so a restart gives
		// each one more try, and one edited since then is sent again.
		sent := make([]int, 0, len(batch))
		items := make([]remotePlaySessionItem, 0, len(batch))
		refs := make([]database.MediaHistorySyncRef, 0, len(batch))
		for i := range batch {
			if m.refusedSessions.has(batch[i].DBID, batch[i].UpdatedAt) {
				continue
			}
			sent = append(sent, i)
			items = append(items, mediaHistoryToRemote(&batch[i]))
			refs = append(refs, database.MediaHistorySyncRef{
				DBID: batch[i].DBID, UpdatedAt: batch[i].UpdatedAt,
			})
		}
		if !m.cfg.PlaytimeSyncEnabled() {
			return info, errPlaySyncDisabled
		}
		var refused []int
		var resp remotePlaySessionResponse
		if len(items) > 0 {
			var uploadErr error
			resp, uploadErr = client.uploadWithoutRefused(ctx, items, 0, &refused)
			if uploadErr != nil {
				return info, uploadErr
			}
		}
		if len(refused) > 0 {
			refusedSet := make(map[int]struct{}, len(refused))
			for _, index := range refused {
				refusedSet[index] = struct{}{}
				entry := &batch[sent[index]]
				if !m.refusedSessions.add(entry.DBID, entry.UpdatedAt) {
					continue
				}
				info.NewlyRefused++
				log.Warn().
					Int64("dbid", entry.DBID).
					Str("session", entry.ID).
					Str("system", entry.SystemID).
					Str("name", entry.MediaName).
					Str("path", entry.MediaPath).
					Msg("account refused a play session; skipping it and syncing the rest")
			}
			kept := refs[:0:0]
			for i := range refs {
				if _, skip := refusedSet[i]; !skip {
					kept = append(kept, refs[i])
				}
			}
			refs = kept
			info.Refused += len(refused)
		}

		if markErr := m.database.UserDB.MarkMediaHistorySynced(refs, time.Now().UTC()); markErr != nil {
			// Batch selection depends on local acknowledgement state. Stop this
			// pass rather than uploading the same unmarked rows repeatedly; the
			// next pass safely retries the idempotent server upsert.
			return info, fmt.Errorf("marking media history rows synced: %w", markErr)
		}

		info.Uploaded += len(refs)
		info.Batches++
		last := &batch[len(batch)-1]
		cursor = last.UpdatedAt
		cursorDBID = last.DBID
		log.Debug().
			Int("batch", len(batch)).
			Int64("accepted", resp.Accepted).
			Time("cursor", cursor).
			Msg("play history batch synced")

		if len(batch) < playSyncBatchSize {
			break
		}
	}

	m.notifyRefusedSessions(info.NewlyRefused)
	if info.Uploaded > 0 || info.Refused > 0 {
		log.Info().
			Int("sessions", info.Uploaded).
			Int("batches", info.Batches).
			Int("refused", info.Refused).
			Msg("play history sync completed")
	}
	return info, nil
}
