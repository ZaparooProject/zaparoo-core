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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
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

// PlaySyncInfo summarizes one play-history sync pass.
type PlaySyncInfo struct {
	Uploaded int
	Batches  int
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remotePlaySessionItem struct {
	ProfileID     *string    `json:"profile_id,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	SessionUUID   string     `json:"session_uuid"`
	SystemID      string     `json:"system_id"`
	SystemName    string     `json:"system_name"`
	LauncherID    string     `json:"launcher_id"`
	MediaPath     string     `json:"media_path"`
	MediaName     string     `json:"media_name"`
	ClockSource   string     `json:"clock_source"`
	Tags          []string   `json:"tags,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	CoreUpdatedAt time.Time  `json:"core_updated_at"`
	PlayTimeSecs  int        `json:"play_time_secs"`
	ClockReliable bool       `json:"clock_reliable"`
	IsDeleted     bool       `json:"is_deleted"`
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

// SyncPlayHistory uploads every session updated since the server's
// watermark. The first call after linking is the bulk import of the whole
// local history; afterwards each pass sends only what changed. A pass is
// cheap when nothing changed: one watermark GET and one empty local query.
func (m *Manager) SyncPlayHistory(ctx context.Context) (PlaySyncInfo, error) {
	if !m.cfg.PlaytimeSyncEnabled() {
		return PlaySyncInfo{}, errPlaySyncDisabled
	}
	client, err := m.newRemoteClient()
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

		items := make([]remotePlaySessionItem, 0, len(batch))
		refs := make([]database.MediaHistorySyncRef, 0, len(batch))
		for i := range batch {
			items = append(items, mediaHistoryToRemote(&batch[i]))
			refs = append(refs, database.MediaHistorySyncRef{
				DBID: batch[i].DBID, UpdatedAt: batch[i].UpdatedAt,
			})
		}
		if !m.cfg.PlaytimeSyncEnabled() {
			return info, errPlaySyncDisabled
		}
		resp, uploadErr := client.uploadPlaySessions(ctx, items)
		if uploadErr != nil {
			return info, uploadErr
		}

		if markErr := m.database.UserDB.MarkMediaHistorySynced(refs, time.Now().UTC()); markErr != nil {
			// Batch selection depends on local acknowledgement state. Stop this
			// pass rather than uploading the same unmarked rows repeatedly; the
			// next pass safely retries the idempotent server upsert.
			return info, fmt.Errorf("marking media history rows synced: %w", markErr)
		}

		info.Uploaded += len(batch)
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

	if info.Uploaded > 0 {
		log.Info().
			Int("sessions", info.Uploaded).
			Int("batches", info.Batches).
			Msg("play history sync completed")
	}
	return info, nil
}
