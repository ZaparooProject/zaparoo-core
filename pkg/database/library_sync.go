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

package database

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// LibraryMediaRow is one present media row with the fields its identity
// observation is built from. Tags are loaded separately so rows outside the
// synced media types never pay for them.
type LibraryMediaRow struct {
	SystemID  string
	Name      string
	Slug      string
	MediaDBID int64
}

// LibraryOrdinal is the cached answer to resolving one identity fingerprint
// with Zaparoo Online: the inventory ordinal, or a rejection code with
// Ordinal 0. SeenGeneration is the last index generation whose inventory
// walk met the fingerprint, so answers for files long gone can be pruned.
type LibraryOrdinal struct {
	ResolvedAt     time.Time
	Fingerprint    string
	Code           string
	SeenGeneration int64
	Ordinal        uint32
}

// LibraryInventoryState records the inventory this device last committed,
// so a pass can skip rebuilding a bitmap for an index generation it already
// uploaded.
type LibraryInventoryState struct {
	CommittedAt time.Time `json:"committedAt"`
	ConfirmedAt time.Time `json:"confirmedAt"`
	// Endpoint is the Library sync base URL the ordinal cache and the
	// committed inventory belong to.
	Endpoint string `json:"endpoint"`
	// Credential tags the device link the inventory was committed under.
	Credential string `json:"credential"`
	SHA256     string `json:"sha256"`
	// TooLarge is set when the inventory for Generation exceeded what the
	// account accepts, so it is not built again until the index changes.
	TooLarge   bool  `json:"tooLarge,omitempty"`
	Generation int64 `json:"generation"`
	ItemCount  int   `json:"itemCount"`
}

// BuildMediaIdentity returns the identity observation for one indexed media
// row, the same observation LookupMediaIdentity builds from a path lookup.
func BuildMediaIdentity(
	mediaType slugs.MediaType,
	canonicalSystemID string,
	displayName string,
	coreSlug string,
	tagInfos []TagInfo,
) (MediaIdentity, error) {
	return newMediaIdentity(mediaType, canonicalSystemID, displayName, coreSlug, tagInfos)
}

// Personal state values of the Library sync contract.
const (
	LibraryIntentNone       = "none"
	LibraryIntentPlayLater  = "play_later"
	LibraryReactionNone     = "none"
	LibraryReactionLiked    = "liked"
	LibraryReactionDisliked = "disliked"
)

// LibraryStateSyncRow is the last personal state row this device agreed
// with the account for one game, or the row it holds no copy of yet.
type LibraryStateSyncRow struct {
	IdentityKey   string
	MediaType     string
	SystemID      string
	CoreSlug      string
	Title         string
	Intent        string
	Reaction      string
	RejectedCode  string
	RejectedHash  string
	VariantTags   []string
	PreferredTags []string
	Revision      int64
	UpdatedAt     int64
	Favorite      bool
	Deleted       bool
	Unmatched     bool
}

// DeckSyncRow is the last copy of an owned deck this device agreed with the
// account. Snapshot is the agreed deck content as JSON; Revision 0 means the
// deck was never created on the account.
type DeckSyncRow struct {
	DeckID       string
	Snapshot     string
	RejectedCode string
	RejectedHash string
	Revision     int64
	UpdatedAt    int64
	Conflicts    int
	Locked       bool
}
