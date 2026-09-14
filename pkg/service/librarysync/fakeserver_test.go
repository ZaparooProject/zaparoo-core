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

package librarysync_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// fakeOnline implements the device side of the Library sync contract in
// memory, closely enough to exercise every path Core takes.
type fakeOnline struct {
	t             *testing.T
	stateRows     map[string]*fakeStateRow
	ordinals      map[string]uint32
	rejected      map[string]string
	issued        map[uint32]bool
	held          *fakeInventory
	server        *httptest.Server
	calls         map[string]int
	rejectState   string
	token         string
	putError      string
	statePushes   [][]fakeStatePushItem
	resolved      [][]database.MediaIdentity
	stateRevision int64
	stateFloor    int64
	limitFirst    int
	mu            syncutil.Mutex
	nextID        uint32
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type fakeResolveItem struct {
	MediaIdentity database.MediaIdentity `json:"media_identity"`
}

type fakeResolveRequest struct {
	Items []fakeResolveItem `json:"items"`
}

// fakeStateRow is one personal state row as the fake account holds it.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type fakeStateRow struct {
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

func (row *fakeStateRow) isDefault() bool {
	return !row.Favorite && row.Intent == "none" && row.Reaction == "none"
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type fakeStatePushItem struct {
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

type fakeInventory struct {
	sha        string
	ordinals   []uint32
	generation int64
}

func newFakeOnline(t *testing.T) *fakeOnline {
	t.Helper()
	f := &fakeOnline{
		t:         t,
		ordinals:  make(map[string]uint32),
		rejected:  make(map[string]string),
		issued:    make(map[uint32]bool),
		calls:     make(map[string]int),
		nextID:    100,
		token:     "library-token",
		stateRows: make(map[string]*fakeStateRow),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOnline) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

func (f *fakeOnline) setPutError(code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putError = code
}

func (f *fakeOnline) setRejected(fingerprint, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code == "" {
		delete(f.rejected, fingerprint)
		return
	}
	f.rejected[fingerprint] = code
}

func (f *fakeOnline) setLimitFirst(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limitFirst = n
}

func (f *fakeOnline) resolvedBatches() [][]database.MediaIdentity {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]database.MediaIdentity(nil), f.resolved...)
}

func (f *fakeOnline) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = make(map[string]int)
	f.resolved = nil
}

func (f *fakeOnline) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	if strings.HasPrefix(r.URL.Path, "/v1/device/library/inventory/") {
		key = r.Method + " /v1/device/library/inventory/{sha256}"
	}
	f.calls[key]++
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch key {
	case "POST /v1/device/library/resolve":
		f.handleResolve(w, r)
	case "GET /v1/device/library/inventory":
		if f.held == nil {
			writeFakeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeFakeJSON(w, f.inventoryBody(false))
	case "DELETE /v1/device/library/inventory":
		f.held = nil
		w.WriteHeader(http.StatusNoContent)
	case "PUT /v1/device/library/inventory/{sha256}":
		f.handlePut(w, r)
	case "POST /v1/device/library/state":
		f.handleStatePush(w, r)
	case "GET /v1/device/library/state":
		f.handleStatePull(w, r)
	default:
		f.t.Errorf("unexpected request %s", key)
		w.WriteHeader(http.StatusTeapot)
	}
}

func (f *fakeOnline) handleResolve(w http.ResponseWriter, r *http.Request) {
	if f.limitFirst > 0 {
		f.limitFirst--
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too Many Requests"))
		return
	}
	var request fakeResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Items) == 0 ||
		len(request.Items) > 500 {
		writeFakeError(w, http.StatusBadRequest, "validation_error")
		return
	}
	batch := make([]database.MediaIdentity, 0, len(request.Items))
	items := make([]map[string]any, 0, len(request.Items))
	for i := range request.Items {
		identity := request.Items[i].MediaIdentity
		batch = append(batch, identity)
		fingerprint, err := database.ComputeMediaIdentityFingerprint(&identity)
		if err != nil || fingerprint != identity.ObservationFingerprint {
			items = append(items, map[string]any{"index": i, "status": "rejected", "code": "invalid_fingerprint"})
			continue
		}
		if code, ok := f.rejected[fingerprint]; ok {
			items = append(items, map[string]any{"index": i, "status": "rejected", "code": code})
			continue
		}
		ordinal, ok := f.ordinals[fingerprint]
		if !ok {
			f.nextID++
			ordinal = f.nextID
			f.ordinals[fingerprint] = ordinal
			f.issued[ordinal] = true
		}
		items = append(items, map[string]any{"index": i, "status": "resolved", "ordinal": ordinal})
	}
	f.resolved = append(f.resolved, batch)
	writeFakeJSON(w, map[string]any{"items": items, "resolved": 0, "rejected": 0})
}

func (f *fakeOnline) handlePut(w http.ResponseWriter, r *http.Request) {
	if f.putError != "" {
		writeFakeError(w, http.StatusBadRequest, f.putError)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeFakeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if !strings.HasSuffix(r.URL.Path, "/"+digest) {
		writeFakeError(w, http.StatusBadRequest, "integrity_mismatch")
		return
	}
	generation, genErr := strconv.ParseInt(r.Header.Get("X-Zaparoo-Library-Generation"), 10, 64)
	count, countErr := strconv.ParseUint(r.Header.Get("X-Zaparoo-Library-Item-Count"), 10, 32)
	if genErr != nil || countErr != nil || r.Header.Get("X-Zaparoo-Library-Schema-Version") != "1" ||
		r.Header.Get("Content-Type") != "application/octet-stream" {
		writeFakeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	bitmap := roaring.New()
	if unmarshalErr := bitmap.UnmarshalBinary(body); unmarshalErr != nil {
		writeFakeError(w, http.StatusBadRequest, "invalid_bitmap")
		return
	}
	if bitmap.GetCardinality() != count {
		writeFakeError(w, http.StatusBadRequest, "item_count_mismatch")
		return
	}
	for _, ordinal := range bitmap.ToArray() {
		if !f.issued[ordinal] {
			writeFakeError(w, http.StatusBadRequest, "unknown_ordinal")
			return
		}
	}
	if f.held != nil && generation < f.held.generation {
		writeFakeJSON(w, f.inventoryBody(true))
		return
	}
	f.held = &fakeInventory{sha: digest, ordinals: bitmap.ToArray(), generation: generation}
	writeFakeJSON(w, f.inventoryBody(false))
}

func (f *fakeOnline) inventoryBody(superseded bool) map[string]any {
	return map[string]any{
		"content_sha256":   f.held.sha,
		"index_generation": f.held.generation,
		"schema_version":   1,
		"item_count":       len(f.held.ordinals),
		"size_bytes":       0,
		"system_counts":    map[string]int{},
		"committed_at":     "2026-09-14T00:00:00Z",
		"deduplicated":     false,
		"unchanged":        false,
		"superseded":       superseded,
	}
}

func writeFakeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeFakeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": code}})
}

func fakeStateKey(mediaType, systemID, slug string, tagList []string) (key string, variants []string) {
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

// putStateRow writes a row as if another device or Zaparoo Online had.
func (f *fakeOnline) putStateRow(row *fakeStateRow) *fakeStateRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	key, variants := fakeStateKey(row.MediaType, row.SystemID, row.CoreSlug, row.VariantTags)
	f.stateRevision++
	stored := *row
	stored.VariantTags = variants
	stored.Revision = f.stateRevision
	if stored.PreferredTags == nil {
		stored.PreferredTags = []string{}
	}
	if stored.Intent == "" {
		stored.Intent = "none"
	}
	if stored.Reaction == "" {
		stored.Reaction = "none"
	}
	f.stateRows[key] = &stored
	copied := stored
	return &copied
}

func (f *fakeOnline) stateRow(mediaType, systemID, slug string, variants ...string) *fakeStateRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	key, _ := fakeStateKey(mediaType, systemID, slug, variants)
	row, ok := f.stateRows[key]
	if !ok {
		return nil
	}
	copied := *row
	return &copied
}

// eraseState erases every state row the way an account-wide erase does.
func (f *fakeOnline) eraseState() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateRevision++
	f.stateFloor = f.stateRevision
	f.stateRows = make(map[string]*fakeStateRow)
}

func (f *fakeOnline) pushedState() [][]fakeStatePushItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]fakeStatePushItem(nil), f.statePushes...)
}

func (f *fakeOnline) setRejectState(code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectState = code
}

func (f *fakeOnline) handleStatePush(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Items []fakeStatePushItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Items) == 0 {
		writeFakeError(w, http.StatusBadRequest, "validation_error")
		return
	}
	f.statePushes = append(f.statePushes, request.Items)
	results := make([]map[string]any, 0, len(request.Items))
	for i := range request.Items {
		item := &request.Items[i]
		if item.Tags == nil {
			writeFakeError(w, http.StatusBadRequest, "validation_error")
			return
		}
		if f.rejectState != "" {
			results = append(results, map[string]any{
				"index": i, "status": "rejected", "code": f.rejectState, "state": nil,
			})
			continue
		}
		key, variants := fakeStateKey(item.MediaType, item.SystemID, item.CoreSlug, item.Tags)
		row := f.stateRows[key]
		switch {
		case row == nil && item.BaseRevision > 0:
			results = append(results, map[string]any{"index": i, "status": "conflict", "state": nil})
			continue
		case row != nil && row.Revision != item.BaseRevision:
			results = append(results, map[string]any{"index": i, "status": "conflict", "state": row})
			continue
		}
		next := fakeStateRow{
			MediaType: item.MediaType, SystemID: item.SystemID, CoreSlug: item.CoreSlug,
			VariantTags: variants, Intent: "none", Reaction: "none", PreferredTags: []string{},
		}
		if row != nil && !row.Deleted {
			next = *row
		}
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
		if next.Favorite && next.Reaction == "disliked" {
			results = append(results, map[string]any{
				"index": i, "status": "rejected", "code": "invalid_state", "state": nil,
			})
			continue
		}
		if row == nil && next.isDefault() {
			results = append(results, map[string]any{"index": i, "status": "applied", "state": nil})
			continue
		}
		f.stateRevision++
		next.Revision = f.stateRevision
		next.Deleted = next.isDefault()
		stored := next
		f.stateRows[key] = &stored
		results = append(results, map[string]any{"index": i, "status": "applied", "state": &stored})
	}
	writeFakeJSON(w, map[string]any{"items": results, "applied": 0, "conflicts": 0, "rejected": 0})
}

func (f *fakeOnline) handleStatePull(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		writeFakeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	reset := false
	if since > 0 && since < f.stateFloor {
		reset = true
		since = 0
	}
	rows := make([]*fakeStateRow, 0)
	for _, row := range f.stateRows {
		if row.Revision > since {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Revision < rows[j].Revision })
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	next := since
	if len(rows) > 0 {
		next = rows[len(rows)-1].Revision
	}
	writeFakeJSON(w, map[string]any{"items": rows, "next_since": next, "has_more": hasMore, "reset": reset})
}
