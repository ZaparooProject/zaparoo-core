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
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	// linkInterval is the poll interval the fake asks for, in seconds. Core
	// takes any positive value; one second keeps a sim's link fast.
	linkInterval = 1
	linkTTL      = 10 * time.Minute

	pathWellKnown   = "/.well-known/zaparoo"
	pathLinkCreate  = "/v1/device-link-requests"
	pathLinkPoll    = "/v1/device-link-requests/poll"
	pathClaimRedeem = "/v1/device-claims/redeem"
	pathImages      = "/images/"

	// featureCloudBackup is the feature /v1/device/me lists for an account
	// with cloud backup.
	featureCloudBackup = "cloud_backup"
)

type deviceHandler func(w http.ResponseWriter, r *http.Request, deviceID string)

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pathWellKnown, s.handleWellKnown)
	mux.HandleFunc("POST "+pathLinkCreate, s.handleLinkCreate)
	mux.HandleFunc("POST "+pathLinkPoll, s.handleLinkPoll)
	mux.HandleFunc("POST "+pathClaimRedeem, s.handleClaimRedeem)
	mux.HandleFunc("GET "+pathImages+"{name}", s.handleImage)

	device := func(pattern string, h deviceHandler) {
		mux.HandleFunc(pattern, s.authenticated(h))
	}
	device("POST /v1/device/heartbeat", s.handleHeartbeat)
	device("GET /v1/device/me", s.handleMe)
	device("DELETE /v1/device/me", s.handleRevoke)

	premium := func(pattern string, h deviceHandler) {
		device(pattern, s.requirePremium(h))
	}
	premium("GET /v1/device/backups", s.handleBackupList)
	premium("POST /v1/device/backups", s.handleBackupCommit)
	premium("POST /v1/device/backups/delta", s.handleBackupDelta)
	premium("GET /v1/device/backups/{id}", s.handleBackupGet)
	premium("POST /v1/device/backups/{id}/restore-complete", s.handleRestoreComplete)
	premium("POST /v1/device/backup-objects/check", s.handleObjectsCheck)
	premium("POST /v1/device/backup-objects/locate", s.handleObjectsLocate)
	premium("PUT /v1/device/backup-packs/{hash}", s.handlePackPut)
	premium("GET /v1/device/backup-packs/{hash}", s.handlePackGet)

	device("GET /v1/device/play-sessions/watermark", s.handlePlayWatermark)
	device("POST /v1/device/play-sessions", s.handlePlayUpload)

	device("POST /v1/device/library/resolve", s.handleResolve)
	device("GET /v1/device/library/inventory", s.handleInventoryGet)
	device("DELETE /v1/device/library/inventory", s.handleInventoryDelete)
	device("PUT /v1/device/library/inventory/{sha256}", s.handleInventoryPut)
	device("POST /v1/device/library/state", s.handleStatePush)
	device("GET /v1/device/library/state", s.handleStatePull)
	device("POST /v1/device/decks", s.handleDeckPush)
	device("GET /v1/device/decks", s.handleDeckPull)
	device("POST /v1/device/decks/{id}/image-uploads", s.handleDeckImageUpload)
	device("GET /v1/device/saved-decks", s.handleSavedDecks)

	device("GET /v1/device/remote-sessions/wait", s.handleWait)
	device("POST /v1/device/operations/{id}/accepted", s.handleOpAccepted)
	device("POST /v1/device/operations/{id}/result", s.handleOpResult)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such route in the fake online service")
	})
	return mux
}

func (s *Server) serveDevice(w http.ResponseWriter, r *http.Request) {
	s.muxOnce.Do(func() { s.mux = s.routes() })
	s.mux.ServeHTTP(w, r)
	if r.Method != http.MethodGet {
		_ = s.save()
	}
}

// authenticated resolves the bearer to a linked device, answering 401 for a
// missing or unknown one, which Core reads as the device being unlinked.
func (s *Server) authenticated(h deviceHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || bearer == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing device token")
			return
		}
		s.mu.Lock()
		deviceID, known := s.state.Tokens[bearer]
		if known {
			dev := s.state.Devices[deviceID]
			switch {
			case dev == nil || !dev.Linked:
				known = false
			case r.Header.Get(headerPlatform) != "":
				dev.Platform = r.Header.Get(headerPlatform)
			}
		}
		s.mu.Unlock()
		if !known {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unknown device token")
			return
		}
		h(w, r, deviceID)
	}
}

func (s *Server) requirePremium(h deviceHandler) deviceHandler {
	return func(w http.ResponseWriter, r *http.Request, deviceID string) {
		s.mu.Lock()
		premium := s.state.Account.Premium
		s.mu.Unlock()
		if !premium {
			writeError(w, http.StatusForbidden, "not_available", "cloud backup is not available on this account")
			return
		}
		h(w, r, deviceID)
	}
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type wellKnown struct {
	Trusted   []string `json:"trusted"`
	ZapScript int      `json:"zapscript"`
	Auth      int      `json:"auth"`
}

func (*Server) handleWellKnown(w http.ResponseWriter, _ *http.Request) {
	fakectl.WriteJSON(w, http.StatusOK, wellKnown{ZapScript: 1, Auth: 1, Trusted: []string{}})
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type linkCreateResponse struct {
	ExpiresAt               time.Time `json:"expires_at"`
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURL         string    `json:"verification_url"`
	VerificationURLComplete string    `json:"verification_url_complete"`
	Interval                int       `json:"interval"`
}

func (s *Server) handleLinkCreate(w http.ResponseWriter, r *http.Request) {
	created := now()
	userCode := newUserCode()
	base := s.baseURL(r)
	s.mu.Lock()
	req := &LinkRequest{
		ID:         s.nextID("link-"),
		UserCode:   userCode,
		DeviceCode: randomToken("zpl1_"),
		Status:     LinkPending,
		Platform:   r.Header.Get(headerPlatform),
		OS:         r.Header.Get(headerOS),
		Arch:       r.Header.Get(headerArch),
		DeviceHint: r.Header.Get(headerHint),
		CreatedAt:  created,
		ExpiresAt:  created.Add(linkTTL),
	}
	s.state.LinkRequests = append(s.state.LinkRequests, req)
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusCreated, linkCreateResponse{
		ExpiresAt:               req.ExpiresAt,
		DeviceCode:              req.DeviceCode,
		UserCode:                req.UserCode,
		VerificationURL:         base + "/link",
		VerificationURLComplete: base + "/link?code=" + strings.ReplaceAll(req.UserCode, "-", ""),
		Interval:                linkInterval,
	})
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type linkPollRequest struct {
	DeviceCode string `json:"device_code"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type linkPollResponse struct {
	Status   string `json:"status"`
	Token    string `json:"token,omitempty"`
	ClaimURL string `json:"claim_url,omitempty"`
	Interval int    `json:"interval"`
}

// handleLinkPoll answers pending and approved requests with 200, and every
// request that can no longer be claimed (unknown, denied, expired or already
// claimed) with 401, which Core treats as the end of the flow.
func (s *Server) handleLinkPoll(w http.ResponseWriter, r *http.Request) {
	var body linkPollRequest
	if err := decodeJSON(r, &body); err != nil || body.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "device_code is required")
		return
	}
	base := s.baseURL(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	req := s.linkRequestByDeviceCode(body.DeviceCode)
	if req != nil {
		s.expireLink(req)
	}
	if req == nil || (req.Status != LinkPending && req.Status != LinkApproved) {
		writeError(w, http.StatusUnauthorized, "invalid_grant", "link request is not pending")
		return
	}
	resp := linkPollResponse{Status: req.Status, Interval: linkInterval}
	if req.Status == LinkApproved {
		resp.Token = req.ClaimToken
		resp.ClaimURL = base + pathClaimRedeem
	}
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

type claimRequest struct {
	Token string `json:"token"`
}

type claimResponse struct {
	Bearer string `json:"bearer"`
}

// handleClaimRedeem swaps an approved request's claim token for a device
// bearer. A device hint naming a device the account already has reuses that
// device's record, the way Core expects a re-link to.
func (s *Server) handleClaimRedeem(w http.ResponseWriter, r *http.Request) {
	var body claimRequest
	if err := decodeJSON(r, &body); err != nil || body.Token == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "token is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var req *LinkRequest
	for _, candidate := range s.state.LinkRequests {
		if candidate.ClaimToken == body.Token {
			req = candidate
			break
		}
	}
	if req != nil {
		s.expireLink(req)
	}
	if req == nil || req.Status != LinkApproved {
		writeError(w, http.StatusUnauthorized, "invalid_grant", "claim token is not valid")
		return
	}
	platform := r.Header.Get(headerPlatform)
	if platform == "" {
		platform = req.Platform
	}
	hint := r.Header.Get(headerHint)
	if hint == "" {
		hint = req.DeviceHint
	}
	dev := s.linkDevice(hint, platform, r.Header.Get(headerOS), r.Header.Get(headerArch))
	bearer := randomToken("zpd1_")
	s.state.Tokens[bearer] = dev.ID
	req.Status = LinkClaimed
	req.DeviceID = dev.ID
	fakectl.WriteJSON(w, http.StatusOK, claimResponse{Bearer: bearer})
}

// linkDevice returns the device record for a fresh link: the hinted device
// when the account has it, otherwise a new one. Call with s.mu held.
func (s *Server) linkDevice(hint, platform, goos, arch string) *Device {
	if dev := s.state.Devices[hint]; hint != "" && dev != nil {
		dev.Linked = true
		dev.LinkedAt = now()
		if platform != "" {
			dev.Platform = platform
		}
		s.revokeTokens(dev.ID)
		return dev
	}
	id := hint
	if id == "" || s.state.Devices[id] != nil {
		id = s.nextID("device-")
	}
	name := "Zaparoo Core"
	if platform != "" {
		name += " (" + platform + ")"
	}
	dev := &Device{
		ID: id, Name: name, Platform: platform, OS: goos, Arch: arch,
		LinkedAt: now(), Linked: true,
	}
	s.state.Devices[id] = dev
	return dev
}

// revokeTokens drops every bearer issued to a device. Call with s.mu held.
func (s *Server) revokeTokens(deviceID string) {
	for token, id := range s.state.Tokens {
		if id == deviceID {
			delete(s.state.Tokens, token)
		}
	}
}

// linkRequestByDeviceCode finds a link request. Call with s.mu held.
func (s *Server) linkRequestByDeviceCode(code string) *LinkRequest {
	for _, req := range s.state.LinkRequests {
		if req.DeviceCode == code {
			return req
		}
	}
	return nil
}

// expireLink marks an open request past its expiry as expired. Call with
// s.mu held.
func (*Server) expireLink(req *LinkRequest) {
	if (req.Status == LinkPending || req.Status == LinkApproved) && now().After(req.ExpiresAt) {
		req.Status = LinkExpired
	}
}

// newUserCode returns a code like "ABCD-2345" from letters and digits that
// read unambiguously.
func newUserCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	out := make([]byte, 0, 9)
	for i, b := range buf {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(b)%len(alphabet)])
	}
	return string(out)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type heartbeatRequest struct {
	CoreVersion       string          `json:"core_version"`
	Capabilities      json.RawMessage `json:"capabilities"`
	LastBackupAttempt json.RawMessage `json:"last_backup_attempt"`
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, deviceID string) {
	var body heartbeatRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	s.mu.Lock()
	dev := s.state.Devices[deviceID]
	at := now()
	dev.LastHeartbeatAt = &at
	if body.CoreVersion != "" {
		dev.CoreVersion = body.CoreVersion
	}
	if len(body.Capabilities) > 0 {
		dev.Capabilities = body.Capabilities
	}
	if len(body.LastBackupAttempt) > 0 && string(body.LastBackupAttempt) != "null" {
		dev.LastBackupAttempt = body.LastBackupAttempt
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type protection struct {
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	State          string     `json:"state"`
	RecoveryPoints int        `json:"recovery_points"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type meResponse struct {
	LinkedAt          time.Time       `json:"linked_at"`
	BackupProtection  *protection     `json:"backup_protection"`
	LastBackupAttempt json.RawMessage `json:"last_backup_attempt"`
	Name              string          `json:"name"`
	ID                string          `json:"id"`
	Features          []string        `json:"features"`
	BackupActive      bool            `json:"backup_active"`
}

// handleMe reports the device record. Protection is "never" until the device
// has a backup and "protected" after, the two states Core documents that the
// fake can reach.
func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	dev := *s.state.Devices[deviceID]
	premium := s.state.Account.Premium
	prot := &protection{State: "never"}
	for _, b := range s.state.Backups {
		if b.DeviceID != deviceID {
			continue
		}
		prot.RecoveryPoints++
		created := b.CreatedAt
		if prot.LastSuccessAt == nil || created.After(*prot.LastSuccessAt) {
			prot.LastSuccessAt = &created
		}
	}
	s.mu.Unlock()
	if prot.RecoveryPoints > 0 {
		prot.State = "protected"
	}
	features := []string{}
	if premium {
		features = append(features, featureCloudBackup)
	}
	attempt := dev.LastBackupAttempt
	if len(attempt) == 0 {
		attempt = json.RawMessage("null")
	}
	fakectl.WriteJSON(w, http.StatusOK, meResponse{
		Name: dev.Name, LinkedAt: dev.LinkedAt, ID: dev.ID, Features: features,
		BackupProtection: prot, LastBackupAttempt: attempt, BackupActive: premium,
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, _ *http.Request, deviceID string) {
	s.mu.Lock()
	s.state.Devices[deviceID].Linked = false
	s.revokeTokens(deviceID)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleImage serves deck images the fake stored, so the image URLs it hands
// out resolve.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	data, ok := s.state.Images[r.PathValue("name")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	_, _ = w.Write(data)
}
