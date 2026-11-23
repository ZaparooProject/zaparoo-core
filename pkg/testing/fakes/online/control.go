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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

// Name is the fake's name on the sim's control socket.
const Name = "online"

// ErrNoLinkRequest is returned when there is no link request to act on.
var ErrNoLinkRequest = errors.New("no pending link request")

// LinkRequests returns every link request, oldest first.
func (s *Server) LinkRequests() []LinkRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LinkRequest, 0, len(s.state.LinkRequests))
	for _, req := range s.state.LinkRequests {
		s.expireLink(req)
		out = append(out, *req)
	}
	return out
}

// pendingLink finds a pending request by ID, or the newest pending one for
// an empty ID. Call with s.mu held.
func (s *Server) pendingLink(id string) (*LinkRequest, error) {
	for i := len(s.state.LinkRequests) - 1; i >= 0; i-- {
		req := s.state.LinkRequests[i]
		s.expireLink(req)
		if id != "" && req.ID != id {
			continue
		}
		if req.Status != LinkPending {
			if id == "" {
				continue
			}
			return nil, fmt.Errorf("link request %s is %s, not pending", id, req.Status)
		}
		return req, nil
	}
	if id != "" {
		return nil, fmt.Errorf("%w: %s", ErrNoLinkRequest, id)
	}
	return nil, ErrNoLinkRequest
}

// ApproveLink approves a pending link request, the newest one when id is
// empty, as the account owner entering its code would. Core's next poll
// gets a claim token.
func (s *Server) ApproveLink(id string) (LinkRequest, error) {
	s.mu.Lock()
	req, err := s.pendingLink(id)
	if err != nil {
		s.mu.Unlock()
		return LinkRequest{}, err
	}
	req.Status = LinkApproved
	req.ClaimToken = randomToken("zpc1_")
	out := *req
	s.mu.Unlock()
	return out, s.save()
}

// DenyLink denies a pending link request, the newest one when id is empty.
// Core's next poll ends the flow.
func (s *Server) DenyLink(id string) (LinkRequest, error) {
	s.mu.Lock()
	req, err := s.pendingLink(id)
	if err != nil {
		s.mu.Unlock()
		return LinkRequest{}, err
	}
	req.Status = LinkDenied
	out := *req
	s.mu.Unlock()
	return out, s.save()
}

// SetAccount replaces the account's flags.
func (s *Server) SetAccount(account Account) error {
	s.mu.Lock()
	s.state.Account = account
	s.mu.Unlock()
	return s.save()
}

// Account returns the account's flags.
func (s *Server) Account() Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Account
}

// Devices returns every device the account has had linked.
func (s *Server) Devices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, 0, len(s.state.Devices))
	for _, dev := range s.state.Devices {
		out = append(out, *dev)
	}
	return out
}

// SeedDevice links a device without the link flow, accepting bearer as its
// token. A sim that writes the same bearer into Core's auth.toml for the
// fake's URL starts out linked.
func (s *Server) SeedDevice(id, platform, bearer string) (Device, error) {
	if id == "" || bearer == "" {
		return Device{}, errors.New("seeding a device needs an id and a bearer")
	}
	s.mu.Lock()
	dev := s.linkDevice(id, platform, "", "")
	s.state.Tokens[bearer] = dev.ID
	out := *dev
	s.mu.Unlock()
	return out, s.save()
}

// Backups returns every backup on the account, oldest first.
func (s *Server) Backups() []Backup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Backup, 0, len(s.state.Backups))
	for _, b := range s.state.Backups {
		out = append(out, *b)
	}
	return out
}

// Requests returns the device requests the fake served, oldest first.
func (s *Server) Requests() []fakectl.Request { return s.log.List() }

// ClearRequests empties the request log.
func (s *Server) ClearRequests() { s.log.Clear() }

// ControlHandler serves the control API. The sim mounts it at /fakes/online/
// with the prefix stripped:
//
//	GET    link-requests           every link request
//	POST   link-requests/approve   {"id"}: approve one, newest pending when empty
//	POST   link-requests/deny      {"id"}: deny one, newest pending when empty
//	GET    account                 the account flags
//	POST   account                 {"premium","limited"}: replace them
//	GET    devices                 linked devices
//	POST   devices                 {"id","platform","bearer"}: seed a device
//	GET    ops                     queued operations and their results
//	POST   ops                     {"kind","params"}: queue an operation
//	GET    backups                 committed backups
//	GET    faults                  faults set
//	POST   faults                  {"pathPrefix","kind"}: set a fault
//	DELETE faults[?prefix=]        clear one fault, or all of them
//	GET    requests                the request log
//	DELETE requests                clear it
func (s *Server) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /link-requests", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.LinkRequests())
	})
	linkAction := func(action func(string) (LinkRequest, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ID string `json:"id"`
			}
			if !decodeControl(w, r, &body) {
				return
			}
			req, err := action(body.ID)
			if err != nil {
				controlError(w, http.StatusConflict, err)
				return
			}
			fakectl.WriteJSON(w, http.StatusOK, req)
		}
	}
	mux.HandleFunc("POST /link-requests/approve", linkAction(s.ApproveLink))
	mux.HandleFunc("POST /link-requests/deny", linkAction(s.DenyLink))
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Account())
	})
	mux.HandleFunc("POST /account", func(w http.ResponseWriter, r *http.Request) {
		var account Account
		if !decodeControl(w, r, &account) {
			return
		}
		if err := s.SetAccount(account); err != nil {
			controlError(w, http.StatusInternalServerError, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, s.Account())
	})
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Devices())
	})
	mux.HandleFunc("POST /devices", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string `json:"id"`
			Platform string `json:"platform"`
			Bearer   string `json:"bearer"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		dev, err := s.SeedDevice(body.ID, body.Platform, body.Bearer)
		if err != nil {
			controlError(w, http.StatusBadRequest, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, dev)
	})
	mux.HandleFunc("GET /ops", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Operations())
	})
	mux.HandleFunc("POST /ops", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind   string          `json:"kind"`
			Params json.RawMessage `json:"params"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		op, err := s.EnqueueOperation(body.Kind, body.Params)
		if err != nil {
			controlError(w, http.StatusBadRequest, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, op)
	})
	mux.HandleFunc("GET /backups", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Backups())
	})
	mux.HandleFunc("GET /faults", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Faults())
	})
	mux.HandleFunc("POST /faults", func(w http.ResponseWriter, r *http.Request) {
		var fault Fault
		if !decodeControl(w, r, &fault) {
			return
		}
		if err := s.SetFault(fault); err != nil {
			controlError(w, http.StatusBadRequest, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, s.Faults())
	})
	mux.HandleFunc("DELETE /faults", func(w http.ResponseWriter, r *http.Request) {
		if prefix := r.URL.Query().Get("prefix"); prefix != "" {
			s.ClearFault(prefix)
		} else {
			s.ClearFaults()
		}
		fakectl.WriteJSON(w, http.StatusOK, s.Faults())
	})
	mux.Handle("/requests", s.log.RequestsHandler())
	return mux
}

func decodeControl(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := decodeJSON(r, out); err != nil {
		controlError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func controlError(w http.ResponseWriter, status int, err error) {
	fakectl.WriteJSON(w, status, map[string]string{"error": strings.TrimSpace(err.Error())})
}
