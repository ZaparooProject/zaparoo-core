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
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	manifestFormat  = "cas-v1"
	schemaVersion   = 1
	emptySHA256     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	packTrailerSize = 4
	// maxPackBytes is the largest pack Core builds.
	maxPackBytes = 64 << 20
)

// backupCategories are the categories Core's restore accepts.
var backupCategories = []string{"zaparoo", "settings", "inputs", "saves", "savestates"}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type categorySummary struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type sourceDevice struct {
	Platform *string `json:"platform,omitempty"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Linked   bool    `json:"linked"`
	Current  bool    `json:"current"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type backupResponse struct {
	CreatedAt        time.Time                  `json:"created_at"`
	CoreVersion      *string                    `json:"core_version,omitempty"`
	Platform         *string                    `json:"platform,omitempty"`
	RestoredAt       *time.Time                 `json:"restored_at,omitempty"`
	SourceDevice     *sourceDevice              `json:"source_device,omitempty"`
	Categories       map[string]categorySummary `json:"categories"`
	BackupType       string                     `json:"backup_type"`
	ManifestHash     string                     `json:"manifest_hash"`
	ManifestRootHash string                     `json:"manifest_root_hash"`
	ID               string                     `json:"id"`
	Manifest         json.RawMessage            `json:"manifest,omitempty"`
	SchemaVersion    int                        `json:"schema_version"`
	SizeBytes        int64                      `json:"size_bytes"`
	Deduplicated     bool                       `json:"deduplicated,omitempty"`
}

// canonicalManifest returns the canonical manifest and its sha256 hex, the
// form Core recomputes to check manifest_hash: empty categories dropped,
// entries sorted by path, marshaled compactly.
func canonicalManifest(categories map[string][]ManifestEntry) (data []byte, hash string) {
	sorted := make(map[string][]ManifestEntry, len(categories))
	for category, entries := range categories {
		if len(entries) == 0 {
			continue
		}
		copied := slices.Clone(entries)
		sort.Slice(copied, func(i, j int) bool { return copied[i].Path < copied[j].Path })
		sorted[category] = copied
	}
	// Field order matches Core's manifest struct, so the bytes are the same.
	//
	//nolint:govet // Field order is part of the hashed bytes.
	data, _ = json.Marshal(struct {
		Format     string                     `json:"format"`
		Categories map[string][]ManifestEntry `json:"categories"`
	}{Format: manifestFormat, Categories: sorted})
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

// rootHash is the fake's manifest root hash: any 64-hex value that names
// the snapshot is enough for Core, which only hands it back as a delta base.
func rootHash(manifestHash string) string {
	sum := sha256.Sum256([]byte("root:" + manifestHash))
	return hex.EncodeToString(sum[:])
}

func summarize(categories map[string][]ManifestEntry) (out map[string]categorySummary, total int64) {
	out = make(map[string]categorySummary)
	for category, entries := range categories {
		for _, entry := range entries {
			summary := out[category]
			summary.Files++
			summary.Bytes += entry.Size
			out[category] = summary
			total += entry.Size
		}
	}
	return out, total
}

// toResponse renders a backup for deviceID. Call with s.mu held.
func (s *Server) toResponse(b *Backup, deviceID string, withManifest bool) backupResponse {
	summaries, _ := summarize(b.Categories)
	platform := b.Platform
	resp := backupResponse{
		CreatedAt: b.CreatedAt, CoreVersion: b.CoreVersion, Platform: &platform,
		RestoredAt: b.RestoredAt, Categories: summaries, ManifestHash: b.ManifestHash,
		ManifestRootHash: b.ManifestRootHash, BackupType: b.BackupType, ID: b.ID,
		SchemaVersion: schemaVersion, SizeBytes: b.SizeBytes,
	}
	if dev := s.state.Devices[b.DeviceID]; dev != nil {
		devPlatform := dev.Platform
		resp.SourceDevice = &sourceDevice{
			Platform: &devPlatform, ID: dev.ID, Name: dev.Name, Linked: dev.Linked,
			Current: dev.ID == deviceID,
		}
	}
	if withManifest {
		resp.Manifest, _ = canonicalManifest(b.Categories)
	}
	return resp
}

// latestBackup is the device's newest backup. Call with s.mu held.
func (s *Server) latestBackup(deviceID string) *Backup {
	for i := len(s.state.Backups) - 1; i >= 0; i-- {
		if s.state.Backups[i].DeviceID == deviceID {
			return s.state.Backups[i]
		}
	}
	return nil
}

func (s *Server) backupByID(id string) *Backup {
	for _, b := range s.state.Backups {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// usedBytes is the storage the account's packs take. Call with s.mu held.
func (s *Server) usedBytes() int64 {
	var used int64
	for _, size := range s.state.PackSizes {
		used += size
	}
	return used
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type backupListResponse struct {
	Items             []backupResponse `json:"items"`
	StorageUsedBytes  int64            `json:"storage_used_bytes"`
	StorageQuotaBytes int64            `json:"storage_quota_bytes"`
}

func (s *Server) handleBackupList(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	resp := backupListResponse{
		Items:             make([]backupResponse, 0, len(s.state.Backups)),
		StorageUsedBytes:  s.usedBytes(),
		StorageQuotaBytes: s.state.Account.QuotaBytes(),
	}
	for i := len(s.state.Backups) - 1; i >= 0; i-- {
		resp.Items = append(resp.Items, s.toResponse(s.state.Backups[i], deviceID, false))
	}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) handleBackupGet(w http.ResponseWriter, r *http.Request, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.backupByID(r.PathValue("id"))
	if b == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	fakectl.WriteJSON(w, http.StatusOK, s.toResponse(b, deviceID, true))
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type restoreCompleteRequest struct {
	RestoreID string `json:"restore_id"`
}

func (s *Server) handleRestoreComplete(w http.ResponseWriter, r *http.Request, _ string) {
	var body restoreCompleteRequest
	if err := decodeJSON(r, &body); err != nil || body.RestoreID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "restore_id is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.backupByID(r.PathValue("id"))
	if b == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	at := now()
	b.RestoredAt = &at
	w.WriteHeader(http.StatusNoContent)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type snapshotRequest struct {
	CoreVersion   *string                    `json:"core_version"`
	Categories    map[string][]ManifestEntry `json:"categories"`
	BackupType    string                     `json:"backup_type"`
	SchemaVersion int                        `json:"schema_version"`
}

func (s *Server) handleBackupCommit(w http.ResponseWriter, r *http.Request, deviceID string) {
	var body snapshotRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commit(w, deviceID, r.Header.Get(headerPlatform), &body)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type deltaRequest struct {
	CoreVersion          *string                    `json:"core_version"`
	Upsert               map[string][]ManifestEntry `json:"upsert"`
	Remove               map[string][]string        `json:"remove"`
	BaseManifestRootHash string                     `json:"base_manifest_root_hash"`
	BackupType           string                     `json:"backup_type"`
	SchemaVersion        int                        `json:"schema_version"`
}

// handleBackupDelta applies a delta to the device's newest backup. A base
// that is not that backup is refused with backup_base_mismatch, which Core
// answers with a full commit.
func (s *Server) handleBackupDelta(w http.ResponseWriter, r *http.Request, deviceID string) {
	var body deltaRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	base := s.latestBackup(deviceID)
	if base == nil || base.ManifestRootHash != body.BaseManifestRootHash {
		writeError(w, http.StatusConflict, "backup_base_mismatch", "base is not the device's latest backup")
		return
	}
	files := make(map[string]map[string]ManifestEntry)
	for category, entries := range base.Categories {
		files[category] = make(map[string]ManifestEntry, len(entries))
		for _, entry := range entries {
			files[category][entry.Path] = entry
		}
	}
	for category, paths := range body.Remove {
		for _, path := range paths {
			delete(files[category], path)
		}
	}
	for category, entries := range body.Upsert {
		if files[category] == nil {
			files[category] = make(map[string]ManifestEntry, len(entries))
		}
		for _, entry := range entries {
			files[category][entry.Path] = entry
		}
	}
	categories := make(map[string][]ManifestEntry, len(files))
	for category, byPath := range files {
		for _, entry := range byPath {
			categories[category] = append(categories[category], entry)
		}
	}
	s.commit(w, deviceID, r.Header.Get(headerPlatform), &snapshotRequest{
		CoreVersion: body.CoreVersion, Categories: categories,
		BackupType: body.BackupType, SchemaVersion: body.SchemaVersion,
	})
}

// commit stores a snapshot, or returns the device's newest backup marked
// deduplicated when it holds the same files. Call with s.mu held.
func (s *Server) commit(w http.ResponseWriter, deviceID, platform string, body *snapshotRequest) {
	if body.SchemaVersion != schemaVersion {
		writeErrorFields(w, http.StatusBadRequest, "validation_error", "unsupported schema version",
			map[string]string{"schema_version": "must be " + strconv.Itoa(schemaVersion)})
		return
	}
	if body.BackupType == "" {
		writeErrorFields(w, http.StatusBadRequest, "validation_error", "backup_type is required",
			map[string]string{"backup_type": "required"})
		return
	}
	var missing int
	for category, entries := range body.Categories {
		if len(entries) > 0 && !slices.Contains(backupCategories, category) {
			writeErrorFields(w, http.StatusBadRequest, "validation_error", "unknown category",
				map[string]string{"categories": category})
			return
		}
		for _, entry := range entries {
			if _, ok := s.state.Objects[entry.SHA256]; !ok && entry.SHA256 != emptySHA256 {
				missing++
			}
		}
	}
	if missing > 0 {
		writeError(w, http.StatusConflict, "missing_objects",
			fmt.Sprintf("%d objects have not been uploaded", missing))
		return
	}
	_, manifestHash := canonicalManifest(body.Categories)
	if latest := s.latestBackup(deviceID); latest != nil && latest.ManifestHash == manifestHash {
		resp := s.toResponse(latest, deviceID, true)
		resp.Deduplicated = true
		fakectl.WriteJSON(w, http.StatusOK, resp)
		return
	}
	if platform == "" {
		platform = s.state.Devices[deviceID].Platform
	}
	categories := make(map[string][]ManifestEntry, len(body.Categories))
	for category, entries := range body.Categories {
		if len(entries) > 0 {
			categories[category] = slices.Clone(entries)
		}
	}
	_, size := summarize(categories)
	b := &Backup{
		CreatedAt: now(), CoreVersion: body.CoreVersion, Categories: categories,
		ID: s.nextID("backup-"), DeviceID: deviceID, Platform: platform,
		BackupType: body.BackupType, ManifestHash: manifestHash,
		ManifestRootHash: rootHash(manifestHash), SizeBytes: size,
	}
	s.state.Backups = append(s.state.Backups, b)
	fakectl.WriteJSON(w, http.StatusCreated, s.toResponse(b, deviceID, true))
}

type hashesRequest struct {
	Hashes []string `json:"hashes"`
}

type checkResponse struct {
	Missing []string `json:"missing"`
}

func (s *Server) handleObjectsCheck(w http.ResponseWriter, r *http.Request, _ string) {
	var body hashesRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	resp := checkResponse{Missing: []string{}}
	s.mu.Lock()
	for _, hash := range body.Hashes {
		if _, ok := s.state.Objects[hash]; !ok && hash != emptySHA256 {
			resp.Missing = append(resp.Missing, hash)
		}
	}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type packObjects struct {
	PackHash  string      `json:"pack_hash"`
	Objects   []packEntry `json:"objects"`
	SizeBytes int64       `json:"size_bytes"`
}

type packEntry struct {
	Hash   string `json:"hash"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
}

type locateResponse struct {
	Packs   []*packObjects `json:"packs"`
	Missing []string       `json:"missing"`
}

func (s *Server) handleObjectsLocate(w http.ResponseWriter, r *http.Request, _ string) {
	var body hashesRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	resp := locateResponse{Packs: []*packObjects{}, Missing: []string{}}
	byPack := make(map[string]*packObjects)
	s.mu.Lock()
	for _, hash := range body.Hashes {
		ref, ok := s.state.Objects[hash]
		if !ok {
			resp.Missing = append(resp.Missing, hash)
			continue
		}
		group := byPack[ref.Pack]
		if group == nil {
			group = &packObjects{PackHash: ref.Pack, SizeBytes: s.state.PackSizes[ref.Pack]}
			byPack[ref.Pack] = group
			resp.Packs = append(resp.Packs, group)
		}
		group.Objects = append(group.Objects, packEntry{Hash: hash, Offset: ref.Offset, Length: ref.Length})
	}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type packResponse struct {
	CreatedAt   time.Time `json:"created_at"`
	PackHash    string    `json:"pack_hash"`
	SizeBytes   int64     `json:"size_bytes"`
	ObjectCount int       `json:"object_count"`
}

// handlePackPut stores a pack after checking it the way Core builds one:
// objects, then a JSON footer of {hash, offset, length}, then the footer's
// length as a big-endian uint32, all named by the sha256 of the body.
func (s *Server) handlePackPut(w http.ResponseWriter, r *http.Request, _ string) {
	hash := r.PathValue("hash")
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPackBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if len(body) > maxPackBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "pack is larger than 64 MiB")
		return
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != hash {
		writeError(w, http.StatusBadRequest, "integrity_mismatch", "pack body does not match its hash")
		return
	}
	entries, err := parsePack(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "integrity_mismatch", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := http.StatusOK
	if _, exists := s.state.PackSizes[hash]; !exists {
		if s.usedBytes()+int64(len(body)) > s.state.Account.QuotaBytes() {
			writeError(w, http.StatusForbidden, "quota_exceeded", "backup storage quota exceeded")
			return
		}
		if err := s.savePack(hash, body); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		s.packs[hash] = body
		s.state.PackSizes[hash] = int64(len(body))
		for _, entry := range entries {
			if _, known := s.state.Objects[entry.Hash]; !known {
				s.state.Objects[entry.Hash] = objectRef{Pack: hash, Offset: entry.Offset, Length: entry.Length}
			}
		}
		status = http.StatusCreated
	}
	fakectl.WriteJSON(w, status, packResponse{
		CreatedAt: now(), PackHash: hash, SizeBytes: int64(len(body)), ObjectCount: len(entries),
	})
}

// parsePack checks a pack's footer and that every object in it matches its
// hash.
func parsePack(body []byte) ([]packEntry, error) {
	if len(body) < packTrailerSize {
		return nil, errors.New("pack is shorter than its trailer")
	}
	footerLen := int(binary.BigEndian.Uint32(body[len(body)-packTrailerSize:]))
	dataEnd := len(body) - packTrailerSize - footerLen
	if footerLen <= 0 || dataEnd < 0 {
		return nil, errors.New("pack footer length is out of range")
	}
	var entries []packEntry
	if err := json.Unmarshal(body[dataEnd:len(body)-packTrailerSize], &entries); err != nil {
		return nil, fmt.Errorf("decoding pack footer: %w", err)
	}
	for _, entry := range entries {
		if entry.Offset < 0 || entry.Length < 0 || entry.Offset+entry.Length > int64(dataEnd) {
			return nil, fmt.Errorf("pack object %s is outside the pack", entry.Hash)
		}
		object := sha256.Sum256(body[entry.Offset : entry.Offset+entry.Length])
		if hex.EncodeToString(object[:]) != entry.Hash {
			return nil, fmt.Errorf("pack object %s does not match its hash", entry.Hash)
		}
	}
	return entries, nil
}

func (s *Server) handlePackGet(w http.ResponseWriter, r *http.Request, _ string) {
	s.mu.Lock()
	body, ok := s.packs[r.PathValue("hash")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such pack")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}
