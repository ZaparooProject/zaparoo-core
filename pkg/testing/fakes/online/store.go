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

package online

import (
	"encoding/json"
	"time"
)

// Account is the fake account every linked device belongs to.
type Account struct {
	// Premium gives the account cloud backup. Without it /v1/device/me
	// lists no features and the backup routes answer 403 not_available.
	Premium bool `json:"premium"`
	// Limited shrinks the backup storage quota to LimitedQuotaBytes. A pack
	// that would go over it is refused with quota_exceeded.
	Limited bool `json:"limited"`
}

// QuotaBytes is the account's backup storage quota.
func (a Account) QuotaBytes() int64 {
	if a.Limited {
		return LimitedQuotaBytes
	}
	return StandardQuotaBytes
}

// Link request statuses.
const (
	LinkPending  = "pending"
	LinkApproved = "approved"
	LinkDenied   = "denied"
	LinkClaimed  = "claimed"
	LinkExpired  = "expired"
)

// LinkRequest is one device link request.
type LinkRequest struct {
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	ID         string    `json:"id"`
	UserCode   string    `json:"userCode"`
	DeviceCode string    `json:"deviceCode"`
	ClaimToken string    `json:"claimToken,omitempty"`
	Status     string    `json:"status"`
	Platform   string    `json:"platform,omitempty"`
	OS         string    `json:"os,omitempty"`
	Arch       string    `json:"arch,omitempty"`
	DeviceHint string    `json:"deviceHint,omitempty"`
	DeviceID   string    `json:"deviceId,omitempty"`
}

// Device is one device linked to the account.
type Device struct {
	LinkedAt          time.Time       `json:"linkedAt"`
	LastHeartbeatAt   *time.Time      `json:"lastHeartbeatAt,omitempty"`
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Platform          string          `json:"platform"`
	OS                string          `json:"os,omitempty"`
	Arch              string          `json:"arch,omitempty"`
	CoreVersion       string          `json:"coreVersion,omitempty"`
	Capabilities      json.RawMessage `json:"capabilities,omitempty"`
	LastBackupAttempt json.RawMessage `json:"lastBackupAttempt,omitempty"`
	Linked            bool            `json:"linked"`
}

// Remote operation statuses. The last three are the results Core reports.
const (
	OpQueued    = "queued"
	OpDelivered = "delivered"
	OpAccepted  = "accepted"
	OpSucceeded = "succeeded"
	OpFailed    = "failed"
	OpBusy      = "busy"
)

// Operation is one queued remote operation and, once Core reports it, its
// result.
type Operation struct {
	EnqueuedAt  time.Time       `json:"enqueuedAt"`
	DeadlineAt  time.Time       `json:"deadlineAt"`
	DeliveredAt *time.Time      `json:"deliveredAt,omitempty"`
	AcceptedAt  *time.Time      `json:"acceptedAt,omitempty"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
	Kind        string          `json:"kind"`
	CommandID   string          `json:"commandId"`
	OperationID string          `json:"operationId"`
	DeviceID    string          `json:"deviceId,omitempty"`
	Status      string          `json:"status"`
	ErrorCode   string          `json:"errorCode,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Params      json.RawMessage `json:"params"`
}

// ManifestEntry is one file in a backup manifest.
type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Backup is one committed backup snapshot.
type Backup struct {
	CreatedAt        time.Time                  `json:"createdAt"`
	RestoredAt       *time.Time                 `json:"restoredAt,omitempty"`
	CoreVersion      *string                    `json:"coreVersion,omitempty"`
	Categories       map[string][]ManifestEntry `json:"categories"`
	ID               string                     `json:"id"`
	DeviceID         string                     `json:"deviceId"`
	Platform         string                     `json:"platform"`
	BackupType       string                     `json:"backupType"`
	ManifestHash     string                     `json:"manifestHash"`
	ManifestRootHash string                     `json:"manifestRootHash"`
	SizeBytes        int64                      `json:"sizeBytes"`
}

// PlaySession is one play session a device synced.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type PlaySession struct {
	StartedAt     time.Time       `json:"started_at"`
	CoreUpdatedAt time.Time       `json:"core_updated_at"`
	EndedAt       *time.Time      `json:"ended_at,omitempty"`
	ProfileID     *string         `json:"profile_id,omitempty"`
	MediaIdentity json.RawMessage `json:"media_identity,omitempty"`
	SessionUUID   string          `json:"session_uuid"`
	SystemID      string          `json:"system_id"`
	SystemName    string          `json:"system_name"`
	LauncherID    string          `json:"launcher_id"`
	MediaPath     string          `json:"media_path"`
	MediaName     string          `json:"media_name"`
	ClockSource   string          `json:"clock_source"`
	Tags          []string        `json:"tags,omitempty"`
	PlayTimeSecs  int             `json:"play_time_secs"`
	ClockReliable bool            `json:"clock_reliable"`
	IsDeleted     bool            `json:"is_deleted"`
}

type objectRef struct {
	Pack   string `json:"pack"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
}

type inventory struct {
	CommittedAt time.Time `json:"committedAt"`
	SHA256      string    `json:"sha256"`
	Ordinals    []uint32  `json:"ordinals"`
	Generation  int64     `json:"generation"`
	SizeBytes   int64     `json:"sizeBytes"`
}

// store is everything the fake keeps, and what it saves to the state file.
type store struct {
	Images       map[string][]byte                  `json:"images"`
	Cards        map[string]Card                    `json:"cards"`
	Objects      map[string]objectRef               `json:"objects"`
	PackSizes    map[string]int64                   `json:"packSizes"`
	Ordinals     map[string]uint32                  `json:"ordinals"`
	Inventories  map[string]*inventory              `json:"inventories"`
	StateRows    map[string]*StateRow               `json:"stateRows"`
	Decks        map[string]*Deck                   `json:"decks"`
	PlaySessions map[string]map[string]*PlaySession `json:"playSessions"`
	ImageUploads map[string]*imageUpload            `json:"imageUploads"`
	Devices      map[string]*Device                 `json:"devices"`
	Tokens       map[string]string                  `json:"tokens"`
	LinkRequests []*LinkRequest                     `json:"linkRequests"`
	Backups      []*Backup                          `json:"backups"`
	SavedDecks   []SavedDeck                        `json:"savedDecks"`
	Operations   []*Operation                       `json:"operations"`
	StateRev     int64                              `json:"stateRevision"`
	StateFloor   int64                              `json:"stateFloor"`
	DeckRev      int64                              `json:"deckRevision"`
	DeckFloor    int64                              `json:"deckFloor"`
	Sequence     int64                              `json:"sequence"`
	NextOrdinal  uint32                             `json:"nextOrdinal"`
	Account      Account                            `json:"account"`
}

// firstOrdinal is where the fake starts numbering resolved titles, so an
// ordinal is never mistaken for a list index.
const firstOrdinal = 100

func newStore() *store {
	st := &store{Account: Account{Premium: true}, NextOrdinal: firstOrdinal}
	st.fill()
	return st
}

// fill makes every nil map, as a decoded state file leaves empty ones nil.
func (st *store) fill() {
	if st.Tokens == nil {
		st.Tokens = make(map[string]string)
	}
	if st.Devices == nil {
		st.Devices = make(map[string]*Device)
	}
	if st.Objects == nil {
		st.Objects = make(map[string]objectRef)
	}
	if st.PackSizes == nil {
		st.PackSizes = make(map[string]int64)
	}
	if st.Ordinals == nil {
		st.Ordinals = make(map[string]uint32)
	}
	if st.Inventories == nil {
		st.Inventories = make(map[string]*inventory)
	}
	if st.StateRows == nil {
		st.StateRows = make(map[string]*StateRow)
	}
	if st.Decks == nil {
		st.Decks = make(map[string]*Deck)
	}
	if st.Cards == nil {
		st.Cards = make(map[string]Card)
	}
	if st.ImageUploads == nil {
		st.ImageUploads = make(map[string]*imageUpload)
	}
	if st.Images == nil {
		st.Images = make(map[string][]byte)
	}
	if st.PlaySessions == nil {
		st.PlaySessions = make(map[string]map[string]*PlaySession)
	}
	if st.NextOrdinal < firstOrdinal {
		st.NextOrdinal = firstOrdinal
	}
}
