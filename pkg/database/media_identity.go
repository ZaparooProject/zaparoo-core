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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// Media identity snapshots capture the scanner's complete canonical file-tag
// observation on durable userdb rows (MediaHistory, MediaUserData). MediaDB is
// disposable, and re-deriving these fields later would mean parsing filenames
// outside the scanner. Legacy flat tags use a JSON string array; an empty
// string means no snapshot was possible.

const (
	// CurrentMediaIdentityPolicyVersion identifies the immutable role and
	// fingerprint policy emitted by this Core version.
	CurrentMediaIdentityPolicyVersion = 1
	mediaIdentityFingerprintPrefix    = "sha256:"
)

type MediaIdentityTagRole string

const (
	MediaIdentityTagRoleIdentity MediaIdentityTagRole = "identity"
	MediaIdentityTagRoleContext  MediaIdentityTagRole = "context"
)

// mediaIdentityV1IdentityTagTypes is deliberately independent from mutable UI
// display ordering. Changing this set requires a new identity policy version.
var mediaIdentityV1IdentityTagTypes = map[string]struct{}{
	"unfinished": {}, "unlicensed": {}, "region": {}, "video": {},
	"disc": {}, "disctotal": {}, "edition": {}, "rev": {},
	"arcadeboard": {}, "cabinet": {}, "protection": {}, "set": {},
	"input": {}, "dump": {}, "alt": {}, "compatibility": {},
	"builddate": {}, "lang": {}, "distribution": {}, "media": {},
	"addon": {}, "release": {}, "year": {}, "players": {},
	"developer": {}, "publisher": {}, "copyright": {}, "credit": {},
	"track": {},
}

type MediaIdentityTag struct {
	Type  string               `json:"type"`
	Value string               `json:"value"`
	Role  MediaIdentityTagRole `json:"role"`
}

// MediaIdentity is Core's versioned scanner-derived identity observation for
// one indexed media path.
//
//nolint:tagliatelle // Shared play-session contract uses snake_case JSON fields.
type MediaIdentity struct {
	MediaType              string             `json:"media_type"`
	CanonicalSystemID      string             `json:"canonical_system_id"`
	DisplayName            string             `json:"display_name"`
	CoreSlug               string             `json:"core_slug"`
	ObservationFingerprint string             `json:"observation_fingerprint"`
	Tags                   []MediaIdentityTag `json:"tags"`
	PolicyVersion          int                `json:"policy_version"`
}

//nolint:tagliatelle // Canonical fingerprint contract uses snake_case JSON fields.
type mediaIdentityFingerprintInput struct {
	MediaType         string             `json:"media_type"`
	CanonicalSystemID string             `json:"canonical_system_id"`
	CoreSlug          string             `json:"core_slug"`
	Tags              []MediaIdentityTag `json:"tags"`
	PolicyVersion     int                `json:"policy_version"`
}

// EncodeTagStrings serializes legacy flat tags for a userdb TEXT column. Nil
// or empty input encodes to the empty string.
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

// DecodeTagStrings parses a legacy userdb tags column value. Empty or
// malformed input decodes to nil because snapshots are best-effort data.
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

// EncodeMediaIdentity serializes a complete identity snapshot for UserDB.
func EncodeMediaIdentity(identity *MediaIdentity) string {
	if identity == nil {
		return ""
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		log.Warn().Err(err).Msg("failed to encode media identity")
		return ""
	}
	return string(encoded)
}

// DecodeMediaIdentity parses a UserDB identity snapshot. Empty or malformed
// input returns nil so legacy history remains readable.
func DecodeMediaIdentity(raw string) *MediaIdentity {
	if raw == "" {
		return nil
	}
	var identity MediaIdentity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		log.Warn().Err(err).Msg("failed to decode media identity")
		return nil
	}
	return &identity
}

// MediaIdentityTagRoleForPolicy returns a tag's immutable role under a known
// identity policy version.
func MediaIdentityTagRoleForPolicy(policyVersion int, tagType string) (MediaIdentityTagRole, error) {
	if policyVersion != CurrentMediaIdentityPolicyVersion {
		return "", fmt.Errorf("unsupported media identity policy version: %d", policyVersion)
	}
	if _, ok := mediaIdentityV1IdentityTagTypes[tagType]; ok {
		return MediaIdentityTagRoleIdentity, nil
	}
	return MediaIdentityTagRoleContext, nil
}

// LegacyTags returns the complete scanner tag snapshot as sorted type:value
// strings for compatibility fields retained during API migration.
func (identity *MediaIdentity) LegacyTags() []string {
	if identity == nil || len(identity.Tags) == 0 {
		return nil
	}
	tags := make([]string, 0, len(identity.Tags))
	for i := range identity.Tags {
		tags = append(tags, identity.Tags[i].Type+":"+identity.Tags[i].Value)
	}
	return tags
}

// MediaIdentityFingerprintPayload returns the canonical cross-service bytes
// hashed for an observation fingerprint. Display name and raw path are
// intentionally absent.
func MediaIdentityFingerprintPayload(identity *MediaIdentity) ([]byte, error) {
	if identity == nil || identity.PolicyVersion <= 0 {
		return nil, errors.New("media identity policy version is required")
	}
	if identity.MediaType == "" || identity.CanonicalSystemID == "" || identity.CoreSlug == "" {
		return nil, errors.New("media identity type, canonical system, and Core slug are required")
	}

	normalizedTags, err := normalizeMediaIdentityTags(identity.PolicyVersion, identity.Tags)
	if err != nil {
		return nil, err
	}
	payload := mediaIdentityFingerprintInput{
		MediaType:         identity.MediaType,
		CanonicalSystemID: identity.CanonicalSystemID,
		CoreSlug:          identity.CoreSlug,
		Tags:              normalizedTags,
		PolicyVersion:     identity.PolicyVersion,
	}
	// Plain JSON without Go's HTML escaping (& < > as \u00XX): the canonical
	// payload is a cross-service contract and other languages' encoders do
	// not HTML-escape by default. Changing this encoding after release would
	// require a new identity policy version.
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, fmt.Errorf("marshal media identity fingerprint payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// ComputeMediaIdentityFingerprint returns a deterministic SHA-256 fingerprint
// for a normalized scanner observation.
func ComputeMediaIdentityFingerprint(identity *MediaIdentity) (string, error) {
	payload, err := MediaIdentityFingerprintPayload(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return mediaIdentityFingerprintPrefix + hex.EncodeToString(digest[:]), nil
}

func normalizeMediaIdentityTags(
	policyVersion int, input []MediaIdentityTag,
) ([]MediaIdentityTag, error) {
	tags := make([]MediaIdentityTag, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for i := range input {
		if input[i].Type == "" || input[i].Value == "" {
			continue
		}
		role, err := MediaIdentityTagRoleForPolicy(policyVersion, input[i].Type)
		if err != nil {
			return nil, err
		}
		key := input[i].Type + "\x00" + input[i].Value
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, MediaIdentityTag{Type: input[i].Type, Value: input[i].Value, Role: role})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Type != tags[j].Type {
			return tags[i].Type < tags[j].Type
		}
		if tags[i].Value != tags[j].Value {
			return tags[i].Value < tags[j].Value
		}
		return tags[i].Role < tags[j].Role
	})
	if tags == nil {
		return []MediaIdentityTag{}, nil
	}
	return tags, nil
}

func newMediaIdentity(
	mediaType slugs.MediaType,
	canonicalSystemID string,
	displayName string,
	coreSlug string,
	tagInfos []TagInfo,
) (MediaIdentity, error) {
	tags := make([]MediaIdentityTag, 0, len(tagInfos))
	for i := range tagInfos {
		tagType := mediatags.TagType(tagInfos[i].Type)
		if tagInfos[i].Type == "" || tagInfos[i].Tag == "" || !mediatags.IsScannerOwnedType(tagType) {
			continue
		}
		tags = append(tags, MediaIdentityTag{Type: tagInfos[i].Type, Value: tagInfos[i].Tag})
	}
	normalizedTags, err := normalizeMediaIdentityTags(CurrentMediaIdentityPolicyVersion, tags)
	if err != nil {
		return MediaIdentity{}, err
	}
	identity := MediaIdentity{
		MediaType:         string(mediaType),
		CanonicalSystemID: canonicalSystemID,
		DisplayName:       displayName,
		CoreSlug:          coreSlug,
		Tags:              normalizedTags,
		PolicyVersion:     CurrentMediaIdentityPolicyVersion,
	}
	identity.ObservationFingerprint, err = ComputeMediaIdentityFingerprint(&identity)
	if err != nil {
		return MediaIdentity{}, err
	}
	return identity, nil
}

// LookupMediaIdentity resolves an exact indexed media path to Core's complete
// scanner identity observation. found=false covers both a successful no-row
// result and rows that can never resolve (unknown system, unbuildable
// identity); an error is returned only for transient query failures, which
// remain distinguishable for bounded history enrichment retries.
func LookupMediaIdentity(
	ctx context.Context, mediaDB MediaDBI, systemID, path string,
) (MediaIdentity, bool, error) {
	if mediaDB == nil || path == "" {
		return MediaIdentity{}, false, nil
	}
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		log.Debug().Err(err).Str("systemID", systemID).Msg("unknown system for media identity")
		return MediaIdentity{}, false, nil
	}
	results, err := mediaDB.SearchMediaPathExact(ctx, []systemdefs.System{*system}, path)
	if err != nil {
		return MediaIdentity{}, false, fmt.Errorf("search exact media identity path: %w", err)
	}
	if len(results) == 0 {
		return MediaIdentity{}, false, nil
	}

	// Failures below that retrying can never fix (an unknown indexed system,
	// an observation that cannot satisfy the fingerprint contract, e.g. a
	// title whose name slugifies to nothing) report found=false instead of an
	// error: the row is unresolvable and callers must skip it, not treat it
	// as a transient failure that would wedge a backfill sweep on one row.
	indexedSystem, err := systemdefs.GetSystem(results[0].SystemID)
	if err != nil {
		log.Warn().Err(err).Str("systemID", results[0].SystemID).Str("path", path).
			Msg("indexed media identity system is unknown; treating as unresolvable")
		return MediaIdentity{}, false, nil
	}
	tagInfos, err := mediaDB.GetMediaTagsByMediaDBID(ctx, results[0].MediaID)
	if err != nil {
		return MediaIdentity{}, false, fmt.Errorf("load media identity tags: %w", err)
	}
	identity, err := newMediaIdentity(
		indexedSystem.GetMediaType(), results[0].SystemID, results[0].Name, results[0].Slug, tagInfos,
	)
	if err != nil {
		log.Warn().Err(err).Str("path", path).
			Msg("media identity cannot be constructed; treating as unresolvable")
		return MediaIdentity{}, false, nil
	}
	return identity, true, nil
}
