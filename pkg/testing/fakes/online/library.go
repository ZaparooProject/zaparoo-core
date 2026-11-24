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
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	maxResolveItems = 500
	maxPullLimit    = 500

	headerLibraryGeneration = "X-Zaparoo-Library-Generation"
	headerLibraryItemCount  = "X-Zaparoo-Library-Item-Count"
	headerLibrarySchema     = "X-Zaparoo-Library-Schema-Version"

	stateNone = "none"
)

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type resolveItem struct {
	MediaIdentity database.MediaIdentity `json:"media_identity"`
}

type resolveRequest struct {
	Items []resolveItem `json:"items"`
}

type resolveResult struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Index   int    `json:"index"`
	Ordinal uint32 `json:"ordinal,omitempty"`
}

type resolveResponse struct {
	Items    []resolveResult `json:"items"`
	Resolved int             `json:"resolved"`
	Rejected int             `json:"rejected"`
}

// handleResolve gives every title a stable ordinal on the account, keyed by
// its fingerprint, and rejects an identity whose fingerprint does not match
// its fields with invalid_fingerprint.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request, _ string) {
	var body resolveRequest
	if err := decodeJSON(r, &body); err != nil || len(body.Items) == 0 || len(body.Items) > maxResolveItems {
		writeError(w, http.StatusBadRequest, "validation_error", "items must hold 1 to 500 identities")
		return
	}
	resp := resolveResponse{Items: make([]resolveResult, 0, len(body.Items))}
	s.mu.Lock()
	for i := range body.Items {
		identity := body.Items[i].MediaIdentity
		fingerprint, err := database.ComputeMediaIdentityFingerprint(&identity)
		if err != nil || fingerprint != identity.ObservationFingerprint {
			resp.Items = append(resp.Items, resolveResult{Index: i, Status: "rejected", Code: "invalid_fingerprint"})
			resp.Rejected++
			continue
		}
		ordinal, ok := s.state.Ordinals[fingerprint]
		if !ok {
			s.state.NextOrdinal++
			ordinal = s.state.NextOrdinal
			s.state.Ordinals[fingerprint] = ordinal
		}
		resp.Items = append(resp.Items, resolveResult{Index: i, Status: "resolved", Ordinal: ordinal})
		resp.Resolved++
	}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type inventoryResponse struct {
	CommittedAt     time.Time      `json:"committed_at"`
	SystemCounts    map[string]int `json:"system_counts"`
	ContentSHA256   string         `json:"content_sha256"`
	IndexGeneration int64          `json:"index_generation"`
	SchemaVersion   int            `json:"schema_version"`
	ItemCount       int            `json:"item_count"`
	SizeBytes       int64          `json:"size_bytes"`
	Deduplicated    bool           `json:"deduplicated"`
	Unchanged       bool           `json:"unchanged"`
	Superseded      bool           `json:"superseded"`
}

func inventoryBody(inv *inventory, superseded bool) inventoryResponse {
	return inventoryResponse{
		CommittedAt: inv.CommittedAt, SystemCounts: map[string]int{}, ContentSHA256: inv.SHA256,
		IndexGeneration: inv.Generation, SchemaVersion: 1, ItemCount: len(inv.Ordinals),
		SizeBytes: inv.SizeBytes, Superseded: superseded,
	}
}

func (s *Server) handleInventoryGet(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	inv := s.state.Inventories[deviceID]
	s.mu.Unlock()
	if inv == nil {
		writeError(w, http.StatusNotFound, "not_found", "the device has no inventory")
		return
	}
	fakectl.WriteJSON(w, http.StatusOK, inventoryBody(inv, false))
}

func (s *Server) handleInventoryDelete(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	delete(s.state.Inventories, deviceID)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleInventoryPut stores the device's inventory: a roaring bitmap of the
// ordinals it holds, named by its sha256. An upload from an older index
// generation than the one held is answered as superseded and not stored.
func (s *Server) handleInventoryPut(w http.ResponseWriter, r *http.Request, deviceID string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil || len(body) > maxJSONBody {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the inventory")
		return
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if r.PathValue("sha256") != digest {
		writeError(w, http.StatusBadRequest, "integrity_mismatch", "inventory body does not match its hash")
		return
	}
	generation, genErr := strconv.ParseInt(r.Header.Get(headerLibraryGeneration), 10, 64)
	count, countErr := strconv.ParseUint(r.Header.Get(headerLibraryItemCount), 10, 32)
	if genErr != nil || countErr != nil || r.Header.Get(headerLibrarySchema) != "1" ||
		r.Header.Get("Content-Type") != "application/octet-stream" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing or invalid inventory headers")
		return
	}
	bitmap := roaring.New()
	if unmarshalErr := bitmap.UnmarshalBinary(body); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitmap", "")
		return
	}
	if bitmap.GetCardinality() != count {
		writeError(w, http.StatusBadRequest, "item_count_mismatch", "")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issued := make(map[uint32]struct{}, len(s.state.Ordinals))
	for _, ordinal := range s.state.Ordinals {
		issued[ordinal] = struct{}{}
	}
	ordinals := bitmap.ToArray()
	for _, ordinal := range ordinals {
		if _, ok := issued[ordinal]; !ok {
			writeError(w, http.StatusBadRequest, "unknown_ordinal", "")
			return
		}
	}
	if held := s.state.Inventories[deviceID]; held != nil && generation < held.Generation {
		fakectl.WriteJSON(w, http.StatusOK, inventoryBody(held, true))
		return
	}
	inv := &inventory{
		CommittedAt: now(), SHA256: digest, Ordinals: ordinals, Generation: generation,
		SizeBytes: int64(len(body)),
	}
	s.state.Inventories[deviceID] = inv
	fakectl.WriteJSON(w, http.StatusOK, inventoryBody(inv, false))
}

// StateRow is one personal state row (favourite, intent, reaction) on the
// account.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type StateRow struct {
	MediaType     string   `json:"media_type"`
	SystemID      string   `json:"system_id"`
	CoreSlug      string   `json:"core_slug"`
	Title         string   `json:"title"`
	Intent        string   `json:"intent"`
	Reaction      string   `json:"reaction"`
	VariantTags   []string `json:"variant_tags"`
	PreferredTags []string `json:"preferred_tags"`
	Revision      int64    `json:"revision"`
	Favorite      bool     `json:"favorite"`
	Deleted       bool     `json:"deleted"`
}

func (row *StateRow) isDefault() bool {
	return !row.Favorite && row.Intent == stateNone && row.Reaction == stateNone
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type statePushItem struct {
	Favorite      *bool     `json:"favorite"`
	PreferredTags *[]string `json:"preferred_tags"`
	MediaType     string    `json:"media_type"`
	SystemID      string    `json:"system_id"`
	CoreSlug      string    `json:"core_slug"`
	Title         string    `json:"title"`
	Intent        string    `json:"intent"`
	Reaction      string    `json:"reaction"`
	Tags          []string  `json:"tags"`
	BaseRevision  int64     `json:"base_revision"`
}

type stateResult struct {
	State  *StateRow `json:"state"`
	Status string    `json:"status"`
	Code   string    `json:"code,omitempty"`
	Index  int       `json:"index"`
}

type pushResponse[T any] struct {
	Items     []T `json:"items"`
	Applied   int `json:"applied"`
	Conflicts int `json:"conflicts"`
	Rejected  int `json:"rejected"`
}

func (p *pushResponse[T]) count(status string) {
	switch status {
	case "applied":
		p.Applied++
	case "conflict":
		p.Conflicts++
	case "rejected":
		p.Rejected++
	}
}

// stateKey is a row's identity: media type, system, slug and its sorted
// variant tags.
func stateKey(mediaType, systemID, slug string, tagList []string) (key string, variants []string) {
	variants = make([]string, 0, 1)
	for _, tv := range tagList {
		tagType, value, _ := strings.Cut(tv, ":")
		if tags.IsGameVariantTag(tagType, value) {
			variants = append(variants, tv)
		}
	}
	sort.Strings(variants)
	return mediaType + "|" + systemID + "|" + slug + "|" + strings.Join(variants, "+"), variants
}

// handleStatePush applies state rows against their base revision. A stale
// base is a conflict carrying the current row; a row returned to its
// defaults becomes a tombstone.
func (s *Server) handleStatePush(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		Items []statePushItem `json:"items"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Items) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "items is required")
		return
	}
	for i := range body.Items {
		if body.Items[i].Tags == nil {
			writeErrorFields(w, http.StatusBadRequest, "validation_error", "tags is required",
				map[string]string{"items[" + strconv.Itoa(i) + "].tags": "required"})
			return
		}
	}
	resp := pushResponse[stateResult]{Items: make([]stateResult, 0, len(body.Items))}
	add := func(i int, status, code string, row *StateRow) {
		resp.Items = append(resp.Items, stateResult{Index: i, Status: status, Code: code, State: row})
		resp.count(status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range body.Items {
		item := &body.Items[i]
		key, variants := stateKey(item.MediaType, item.SystemID, item.CoreSlug, item.Tags)
		row := s.state.StateRows[key]
		switch {
		case row == nil && item.BaseRevision > 0:
			add(i, "conflict", "", nil)
			continue
		case row != nil && row.Revision != item.BaseRevision:
			current := *row
			add(i, "conflict", "", &current)
			continue
		}
		next := StateRow{
			MediaType: item.MediaType, SystemID: item.SystemID, CoreSlug: item.CoreSlug,
			VariantTags: variants, Intent: stateNone, Reaction: stateNone, PreferredTags: []string{},
		}
		if row != nil && !row.Deleted {
			next = *row
		}
		applyStatePush(&next, item)
		if next.Favorite && next.Reaction == "disliked" {
			add(i, "rejected", "invalid_state", nil)
			continue
		}
		if row == nil && next.isDefault() {
			add(i, "applied", "", nil)
			continue
		}
		s.state.StateRev++
		next.Revision = s.state.StateRev
		next.Deleted = next.isDefault()
		stored := next
		s.state.StateRows[key] = &stored
		result := stored
		add(i, "applied", "", &result)
	}
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

func applyStatePush(next *StateRow, item *statePushItem) {
	if item.Title != "" {
		next.Title = item.Title
	}
	if item.Favorite != nil {
		next.Favorite = *item.Favorite
	}
	if item.Intent != "" {
		next.Intent = item.Intent
	}
	if item.Reaction != "" {
		next.Reaction = item.Reaction
	}
	if item.PreferredTags != nil {
		next.PreferredTags = *item.PreferredTags
	}
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type pullResponse[T any] struct {
	Items     []T   `json:"items"`
	NextSince int64 `json:"next_since"`
	HasMore   bool  `json:"has_more"`
	Reset     bool  `json:"reset"`
}

// window is the part of a pull a request asks for.
type window struct {
	since int64
	limit int
	reset bool
}

// pullWindow reads since and limit from a pull, and moves a cursor older
// than floor back to zero with reset set. ok is false for a bad limit.
func pullWindow(r *http.Request, floor int64) (win window, ok bool) {
	win.since, _ = strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	win.limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if win.limit <= 0 || win.limit > maxPullLimit {
		return window{}, false
	}
	if win.since > 0 && win.since < floor {
		win.since, win.reset = 0, true
	}
	return win, true
}

// page sorts rows by revision and cuts them to limit.
func page[T any](rows []T, since int64, limit int, revision func(T) int64) (out []T, next int64, more bool) {
	sort.Slice(rows, func(i, j int) bool { return revision(rows[i]) < revision(rows[j]) })
	more = len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	next = since
	if len(rows) > 0 {
		next = revision(rows[len(rows)-1])
	}
	return rows, next, more
}

func (s *Server) handleStatePull(w http.ResponseWriter, r *http.Request, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	win, ok := pullWindow(r, s.state.StateFloor)
	since, limit, reset := win.since, win.limit, win.reset
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1 to 500")
		return
	}
	rows := make([]StateRow, 0)
	for _, row := range s.state.StateRows {
		if row.Revision > since {
			rows = append(rows, *row)
		}
	}
	rows, next, more := page(rows, since, limit, func(row StateRow) int64 { return row.Revision })
	fakectl.WriteJSON(w, http.StatusOK, pullResponse[StateRow]{
		Items: rows, NextSince: next, HasMore: more, Reset: reset,
	})
}

// StateRows returns the account's personal state rows, tombstones included.
func (s *Server) StateRows() []StateRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]StateRow, 0, len(s.state.StateRows))
	for _, row := range s.state.StateRows {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Revision < rows[j].Revision })
	return rows
}
