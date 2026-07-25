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
	"context"
	"encoding/json"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// Media identity snapshots: the scanner's parsed filename tags (typed
// "type:value" pairs like "region:us") are captured onto durable userdb rows
// (MediaHistory, MediaUserData) at record time, because MediaDB is
// disposable and re-deriving tags later means re-parsing filenames outside
// the scanner. Tags are stored as a JSON string array; the empty string
// means no snapshot was possible.

// EncodeTagStrings serializes tags for a userdb TEXT column. Nil or empty
// input encodes to the empty string.
func EncodeTagStrings(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		// A []string cannot fail to marshal; guard anyway.
		log.Warn().Err(err).Msg("failed to encode media tags")
		return ""
	}
	return string(encoded)
}

// DecodeTagStrings parses a userdb tags column value. Empty or malformed
// input decodes to nil (a snapshot is best-effort data, never a hard error).
func DecodeTagStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		log.Warn().Err(err).Msg("failed to decode media tags")
		return nil
	}
	return tags
}

// MediaIdentity is the scanner-derived identity snapshot for a media path.
type MediaIdentity struct {
	Name string
	Tags []string
}

// LookupMediaIdentity resolves a media path to its scanner identity: the
// indexed display name and the disambiguating "type:value" tags. The bool
// reports a complete successful lookup, including a valid zero-tag result.
// A media item launched outside the index has no snapshot; lookup failures
// log at debug and return false so callers preserve any earlier snapshot.
func LookupMediaIdentity(
	ctx context.Context, mediaDB MediaDBI, systemID, path string,
) (MediaIdentity, bool) {
	if mediaDB == nil || path == "" {
		return MediaIdentity{}, false
	}
	var systems []systemdefs.System
	if system, err := systemdefs.GetSystem(systemID); err == nil {
		systems = []systemdefs.System{*system}
	}
	results, err := mediaDB.SearchMediaPathExact(ctx, systems, path)
	if err != nil || len(results) == 0 {
		log.Debug().Err(err).Str("path", path).Msg("no media identity for path")
		return MediaIdentity{}, false
	}
	identity := MediaIdentity{Name: results[0].Name}
	tagInfos, err := mediaDB.GetMediaTagsByMediaDBID(ctx, results[0].MediaID)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("failed to load media tags for identity")
		return MediaIdentity{}, false
	}
	tags := make([]string, 0, len(tagInfos))
	for i := range tagInfos {
		if tagInfos[i].Type == "" || tagInfos[i].Tag == "" ||
			mediatags.IsUserOwnedType(mediatags.TagType(tagInfos[i].Type)) {
			continue
		}
		tags = append(tags, tagInfos[i].Type+":"+tagInfos[i].Tag)
	}
	identity.Tags = tags
	return identity, true
}
