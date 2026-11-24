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

// Package romm is an offline fake of a RomM server: the platform and ROM
// listings, ROM details, artwork, ROM downloads and device linking Core uses.
// Its catalogue is a fixed list, a ROM tree on disk, or both.
package romm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	coreromm "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/library"
)

// Name is the fake's name, and the mount the sim gives its control API.
const Name = "romm"

const (
	assetPrefix = "/assets/romm/resources/"
	linkPath    = "/pair/device"
	// linkTTL is how long a link request stays usable.
	linkTTL = 15 * time.Minute
	// linkInterval is the poll interval a link request asks for, in seconds.
	linkInterval = 1
	updatedAt    = "2026-01-01T00:00:00"
)

// Fault is a failure the fake applies to its service requests until cleared:
// a status to answer with, a delay, or both, optionally limited to a path
// prefix.
type Fault = fakehttp.Fault

// Link states.
const (
	LinkPending  = "pending"
	LinkApproved = "approved"
	LinkDenied   = "denied"
	LinkUsed     = "used"
)

// Options configures a fake RomM server.
type Options struct {
	// Content holds ROM downloads, keyed "<rom id>/<file name>".
	Content map[string][]byte
	// Assets holds artwork, keyed by its full path, such as
	// "/assets/romm/resources/roms/1/cover/big.png".
	Assets map[string][]byte
	// LibraryRoot is a ROM tree with one ES-DE system folder per platform,
	// such as <root>/snes/Game.sfc. Each folder becomes a platform and each
	// ROM file a single-file ROM with a generated cover; downloads serve the
	// file from disk. Its IDs follow those of Platforms and Roms.
	LibraryRoot string
	// Token, when set, is the bearer token every API request except device
	// linking must carry, and the token linking hands out. Without one the
	// server is open until a link is approved, which sets a generated token.
	Token string
	// Platforms and Roms are a fixed catalogue served as given.
	Platforms []coreromm.Platform
	Roms      []coreromm.Rom
	// RequestLogLimit bounds the request log; 0 keeps 1000.
	RequestLogLimit int
	// AutoApproveLinks approves each link request as it is made.
	AutoApproveLinks bool
}

// Link is one device link request.
//
//nolint:tagliatelle // The control API spells its fields in snake_case.
type Link struct {
	Created    time.Time `json:"created"`
	DeviceCode string    `json:"device_code"`
	UserCode   string    `json:"user_code"`
	Name       string    `json:"name"`
	Identifier string    `json:"client_device_identifier"`
	Platform   string    `json:"platform,omitempty"`
	Version    string    `json:"client_version,omitempty"`
	Status     string    `json:"status"`
	Scopes     []string  `json:"requested_scopes"`
}

// Server is a fake RomM server.
type Server struct {
	service   http.Handler
	control   http.Handler
	loadErr   error
	log       *fakectl.RequestLog
	content   map[string][]byte
	assets    map[string][]byte
	files     map[int]string
	links     map[string]*Link
	token     string
	listener  fakehttp.Listener
	platforms []coreromm.Platform
	roms      []coreromm.Rom
	faults    fakehttp.Faults
	opts      Options
	codes     int
	mu        syncutil.RWMutex
}

// New returns a fake RomM server. A LibraryRoot that cannot be read leaves
// the server with only the fixed catalogue; Err reports why.
func New(opts Options) *Server { //nolint:gocritic // The fakes share New(opts Options).
	s := &Server{
		opts:  opts,
		log:   fakectl.NewRequestLog(opts.RequestLogLimit),
		links: map[string]*Link{},
		token: opts.Token,
	}
	s.loadErr = s.load()
	s.service = fakehttp.Service(s.log, &s.faults, http.HandlerFunc(s.serve))
	s.control = s.controlMux()
	return s
}

// load builds the catalogue from the options and the library tree.
func (s *Server) load() error {
	platforms := slices.Clone(s.opts.Platforms)
	roms := slices.Clone(s.opts.Roms)
	content := maps.Clone(s.opts.Content)
	if content == nil {
		content = map[string][]byte{}
	}
	assets := maps.Clone(s.opts.Assets)
	if assets == nil {
		assets = map[string][]byte{}
	}
	files := map[int]string{}
	var err error
	if s.opts.LibraryRoot != "" {
		var found []library.File
		found, err = library.Scan(s.opts.LibraryRoot)
		if err == nil {
			platforms, roms = addLibrary(found, platforms, roms, assets, files)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.platforms, s.roms, s.content, s.assets, s.files = platforms, roms, content, assets, files
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	return nil
}

func addLibrary(
	found []library.File, platforms []coreromm.Platform, roms []coreromm.Rom,
	assets map[string][]byte, files map[int]string,
) ([]coreromm.Platform, []coreromm.Rom) {
	nextPlatform, nextRom := 1, 1
	for i := range platforms {
		nextPlatform = max(nextPlatform, platforms[i].ID+1)
	}
	for i := range roms {
		nextRom = max(nextRom, roms[i].ID+1)
	}
	byFolder := map[string]int{}
	for _, f := range found {
		idx, ok := byFolder[f.Folder]
		if !ok {
			slug := PlatformSlug(f.Folder, f.SystemID)
			platforms = append(platforms, coreromm.Platform{
				ID: nextPlatform, Slug: slug, FSSlug: f.Folder, Name: f.Folder, UpdatedAt: updatedAt,
			})
			nextPlatform++
			idx = len(platforms) - 1
			byFolder[f.Folder] = idx
		}
		pl := &platforms[idx]
		pl.ROMCount++
		id := nextRom
		nextRom++
		cover := fmt.Sprintf("roms/%d/%d/cover/big.png", pl.ID, id)
		assets[assetPrefix+cover] = library.PNG(f.Folder + "/" + f.Rel)
		base := library.BaseName(f.Name)
		dir := path.Dir(f.Rel)
		filePath := f.Folder
		if dir != "." {
			filePath += "/" + dir
		}
		roms = append(roms, coreromm.Rom{
			ID: id, PlatformID: pl.ID, PlatformSlug: pl.Slug, PlatformFSSlug: pl.FSSlug,
			Name: base, FSName: f.Name, FSNameNoTags: base, FSNameNoExt: base,
			FSExtension: strings.TrimPrefix(path.Ext(f.Name), "."), FSSizeBytes: f.Size,
			Files: []coreromm.RomFile{{
				ID: id, FileName: f.Name, FilePath: filePath, FileSizeBytes: f.Size,
			}},
			PathCoverLarge: cover + "?ts=1", PathCoverSmall: cover + "?ts=1",
			UpdatedAt: updatedAt,
		})
		files[id] = f.Path
	}
	return platforms, roms
}

// PlatformSlug is the RomM slug the fake gives an ES-DE folder of systemID:
// the folder's own name when RomM uses it for that system, else RomM's first
// slug for the system, else the system ID, which Core matches by name.
func PlatformSlug(folder, systemID string) string {
	if id, ok := coreromm.SystemForPlatform(folder, ""); ok && id == systemID {
		return strings.ToLower(folder)
	}
	if slugs := coreromm.PlatformSlugs(systemID); len(slugs) > 0 {
		return slugs[0]
	}
	return strings.ToLower(systemID)
}

// Err reports why the library tree could not be loaded, if it could not.
func (s *Server) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Reload rebuilds the catalogue from the options and the library tree,
// dropping assets and content set since.
func (s *Server) Reload() error {
	err := s.load()
	s.mu.Lock()
	s.loadErr = err
	s.mu.Unlock()
	return err
}

// Handler serves the fake RomM API.
func (s *Server) Handler() http.Handler { return s.service }

// Start listens on addr, such as "127.0.0.1:0", and returns the base URL.
func (s *Server) Start(addr string) (string, error) {
	return s.listener.Start(addr, s.service) //nolint:wrapcheck // Already descriptive.
}

// URL is the base URL from Start, or "" before it.
func (s *Server) URL() string { return s.listener.URL() }

// Close stops the server started by Start.
func (s *Server) Close() error { return s.listener.Close() } //nolint:wrapcheck // Already descriptive.

// Requests returns the logged requests.
func (s *Server) Requests() []fakectl.Request { return s.log.List() }

// ClearRequests empties the request log.
func (s *Server) ClearRequests() { s.log.Clear() }

// SetFault applies a fault to every later request.
func (s *Server) SetFault(f Fault) error {
	if err := s.faults.Set(f); err != nil {
		return fmt.Errorf("set fault: %w", err)
	}
	return nil
}

// ClearFault removes the fault.
func (s *Server) ClearFault() { s.faults.Clear() }

// SetToken sets the bearer token requests must carry; "" opens the server.
func (s *Server) SetToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
}

// Token is the bearer token requests must carry, or "".
func (s *Server) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token
}

// SetAsset serves body at an asset path such as
// "/assets/romm/resources/roms/1/cover/big.png".
func (s *Server) SetAsset(assetPath string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[assetPath] = body
}

// DeleteAsset stops serving an asset path.
func (s *Server) DeleteAsset(assetPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, assetPath)
}

// SetContent serves body as the download of a ROM under fileName.
func (s *Server) SetContent(romID int, fileName string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.content[strconv.Itoa(romID)+"/"+fileName] = body
}

// Platforms returns the served platforms.
func (s *Server) Platforms() []coreromm.Platform {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.platforms)
}

// Roms returns the served ROMs.
func (s *Server) Roms() []coreromm.Rom {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.roms)
}

// Links returns the device link requests, oldest first.
func (s *Server) Links() []Link {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Link, 0, len(s.links))
	for _, l := range s.links {
		out = append(out, *l)
	}
	slices.SortFunc(out, func(a, b Link) int { return a.Created.Compare(b.Created) })
	return out
}

// ApproveLink approves the pending link with userCode, or every pending link
// when userCode is "". It returns how many it approved.
func (s *Server) ApproveLink(userCode string) int { return s.decideLinks(userCode, LinkApproved) }

// DenyLink denies the pending link with userCode, or every pending link when
// userCode is "". It returns how many it denied.
func (s *Server) DenyLink(userCode string) int { return s.decideLinks(userCode, LinkDenied) }

func (s *Server) decideLinks(userCode, status string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.links {
		if l.Status != LinkPending || (userCode != "" && !strings.EqualFold(l.UserCode, userCode)) {
			continue
		}
		l.Status = status
		n++
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch p {
	case "/api/auth/device/init":
		s.serveLinkInit(w, r)
		return
	case "/api/auth/device/token":
		s.serveLinkToken(w, r)
		return
	case linkPath:
		serveLinkPage(w, r)
		return
	}
	if !s.authorized(r) {
		fakectl.WriteJSON(w, http.StatusUnauthorized, map[string]string{"detail": "Not authenticated"})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch {
	case p == "/api/platforms":
		// A scrape starts with the platform list: pick up ROMs generated
		// after the fake started.
		if s.opts.LibraryRoot != "" {
			_ = s.Reload()
		}
		fakectl.WriteJSON(w, http.StatusOK, s.Platforms())
	case p == "/api/roms":
		s.serveRomList(w, r)
	case strings.HasPrefix(p, "/api/roms/"):
		s.serveRom(w, r, strings.TrimPrefix(p, "/api/roms/"))
	case strings.HasPrefix(p, "/assets/"):
		s.mu.RLock()
		body, ok := s.assets[p]
		s.mu.RUnlock()
		if !ok {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(body))
		_, _ = w.Write(body)
	default:
		notFound(w)
	}
}

func notFound(w http.ResponseWriter) {
	fakectl.WriteJSON(w, http.StatusNotFound, map[string]string{"detail": "Not Found"})
}

func (s *Server) authorized(r *http.Request) bool {
	token := s.Token()
	return token == "" || r.Header.Get("Authorization") == "Bearer "+token
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type romPage struct {
	Items  []coreromm.Rom `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

func (s *Server) serveRomList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("with_files") != "true" {
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "files not requested"})
		return
	}
	var platformIDs []int
	for _, raw := range q["platform_ids"] {
		for part := range strings.SplitSeq(raw, ",") {
			id, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				fakectl.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"detail": "bad platform_ids"})
				return
			}
			platformIDs = append(platformIDs, id)
		}
	}
	limit, offset := 50, 0
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
		offset = v
	}
	var all []coreromm.Rom
	roms := s.Roms()
	for i := range roms {
		if len(platformIDs) == 0 || slices.Contains(platformIDs, roms[i].PlatformID) {
			all = append(all, roms[i])
		}
	}
	page := romPage{Items: []coreromm.Rom{}, Total: len(all), Limit: limit, Offset: offset}
	if offset < len(all) {
		page.Items = all[offset:min(offset+limit, len(all))]
	}
	fakectl.WriteJSON(w, http.StatusOK, page)
}

func (s *Server) serveRom(w http.ResponseWriter, r *http.Request, rest string) {
	idText, file, hasFile := strings.Cut(rest, "/content/")
	id, err := strconv.Atoi(idText)
	if err != nil || strconv.Itoa(id) != idText {
		notFound(w)
		return
	}
	rom, ok := s.rom(id)
	if !ok {
		notFound(w)
		return
	}
	if !hasFile {
		fakectl.WriteJSON(w, http.StatusOK, rom)
		return
	}
	if r.URL.Query().Get("purpose") != "play" {
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "purpose must be play"})
		return
	}
	s.mu.RLock()
	body, haveBody := s.content[idText+"/"+file]
	diskPath, onDisk := s.files[id]
	s.mu.RUnlock()
	switch {
	case haveBody:
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(body))
	case onDisk && len(rom.Files) == 1 && rom.Files[0].FileName == file:
		f, openErr := os.Open(diskPath) //nolint:gosec // G304: a file the fake's own library scan found.
		if openErr != nil {
			notFound(w)
			return
		}
		defer func() { _ = f.Close() }()
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, file, time.Time{}, f)
	default:
		notFound(w)
	}
}

func (s *Server) rom(id int) (coreromm.Rom, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.roms {
		if s.roms[i].ID == id {
			return s.roms[i], true
		}
	}
	return coreromm.Rom{}, false
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type linkInit struct {
	Identifier string   `json:"client_device_identifier"`
	Name       string   `json:"name"`
	Client     string   `json:"client"`
	Platform   string   `json:"platform"`
	Version    string   `json:"client_version"`
	Scopes     []string `json:"requested_scopes"`
}

func (s *Server) serveLinkInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req linkInit
	if err := fakehttp.DecodeJSON(r, &req); err != nil {
		fakectl.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": []string{err.Error()}})
		return
	}
	if req.Identifier == "" || req.Name == "" {
		fakectl.WriteJSON(w, http.StatusUnprocessableEntity,
			map[string]any{"detail": []string{"client_device_identifier and name are required"}})
		return
	}
	l := &Link{
		Created: time.Now().UTC(), DeviceCode: randomHex(16), Name: req.Name, Identifier: req.Identifier,
		Platform: req.Platform, Version: req.Version, Scopes: req.Scopes, Status: LinkPending,
	}
	if s.opts.AutoApproveLinks {
		l.Status = LinkApproved
	}
	s.mu.Lock()
	s.codes++
	l.UserCode = fmt.Sprintf("SIM%d-%04d", s.codes/10000, s.codes%10000)
	s.links[l.DeviceCode] = l
	s.mu.Unlock()
	//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
	fakectl.WriteJSON(w, http.StatusCreated, struct {
		DeviceCode               string `json:"device_code"`
		UserCode                 string `json:"user_code"`
		VerificationPath         string `json:"verification_path"`
		VerificationPathComplete string `json:"verification_path_complete"`
		ExpiresIn                int    `json:"expires_in"`
		Interval                 int    `json:"interval"`
	}{
		DeviceCode: l.DeviceCode, UserCode: l.UserCode, VerificationPath: linkPath,
		VerificationPathComplete: linkPath + "?user_code=" + url.QueryEscape(l.UserCode),
		ExpiresIn:                int(linkTTL / time.Second), Interval: linkInterval,
	})
}

func (s *Server) serveLinkToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"` //nolint:tagliatelle // RomM's API field.
	}
	if err := fakehttp.DecodeJSON(r, &req); err != nil || req.DeviceCode == "" {
		fakectl.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": []string{"device_code required"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[req.DeviceCode]
	if !ok || time.Since(l.Created) > linkTTL {
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "expired_token"})
		return
	}
	switch l.Status {
	case LinkPending:
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "authorization_pending"})
	case LinkDenied:
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "access_denied"})
	case LinkApproved:
		l.Status = LinkUsed
		if s.token == "" {
			s.token = "romm-sim-" + randomHex(12)
		}
		//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
		fakectl.WriteJSON(w, http.StatusOK, struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
		}{AccessToken: s.token, TokenType: "bearer"})
	default:
		fakectl.WriteJSON(w, http.StatusBadRequest, map[string]string{"detail": "expired_token"})
	}
}

func serveLinkPage(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("user_code")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, "<!doctype html><title>RomM device link</title>"+
		"<p>Fake RomM server. Approve code <code>%s</code> through the sim control API.</p>",
		html.EscapeString(code))
}

func jsonBody(v map[string]string) []byte {
	data, _ := json.Marshal(v) //nolint:errchkjson // A string map always encodes.
	return data
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ControlHandler serves the control API, mounted by the sim at /fakes/romm/:
//
//	GET    requests          logged requests; DELETE clears them
//	GET    fault             current fault; POST sets one, DELETE clears it
//	GET    platforms         served platforms
//	GET    roms              served ROMs
//	GET    links             device link requests
//	POST   links/approve     {"user_code":"..."}; empty approves every pending link
//	POST   links/deny        {"user_code":"..."}; empty denies every pending link
//	GET    token             {"token":"..."}; PUT sets it, DELETE opens the server
//	POST   reload            rebuild the catalogue from the library tree
func (s *Server) ControlHandler() http.Handler { return s.control }

func (s *Server) controlMux() http.Handler {
	mux := fakehttp.ControlMux(s.log, &s.faults)
	mux.HandleFunc("GET /platforms", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Platforms())
	})
	mux.HandleFunc("GET /roms", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Roms())
	})
	mux.HandleFunc("GET /links", func(w http.ResponseWriter, _ *http.Request) {
		fakectl.WriteJSON(w, http.StatusOK, s.Links())
	})
	decide := func(fn func(string) int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				UserCode string `json:"user_code"` //nolint:tagliatelle // Matches the link field.
			}
			if err := fakehttp.DecodeJSON(r, &req); err != nil {
				fakehttp.WriteError(w, http.StatusBadRequest, err)
				return
			}
			n := fn(req.UserCode)
			if n == 0 {
				fakehttp.WriteError(w, http.StatusNotFound, errors.New("no matching pending link"))
				return
			}
			fakectl.WriteJSON(w, http.StatusOK, map[string]int{"changed": n})
		}
	}
	mux.HandleFunc("POST /links/approve", decide(s.ApproveLink))
	mux.HandleFunc("POST /links/deny", decide(s.DenyLink))
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
		case http.MethodPut, http.MethodPost:
			var req struct {
				Token string `json:"token"`
			}
			if err := fakehttp.DecodeJSON(r, &req); err != nil {
				fakehttp.WriteError(w, http.StatusBadRequest, err)
				return
			}
			s.SetToken(req.Token)
		case http.MethodDelete:
			s.SetToken("")
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, map[string]string{"token": s.Token()})
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Reload(); err != nil {
			fakehttp.WriteError(w, http.StatusInternalServerError, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, map[string]int{"platforms": len(s.Platforms()), "roms": len(s.Roms())})
	})
	return mux
}

// CLI runs a control command against the fake through call and prints the
// JSON reply:
//
//	requests [clear]
//	fault [set <status> [path-prefix] | delay <ms> [path-prefix] | clear]
//	platforms
//	roms
//	links [approve|deny [user-code]]
//	token [set <token> | clear]
//	reload
func CLI(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	get := func(route string) fakehttp.Command {
		return func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
			if len(args) != 0 {
				return fmt.Errorf("usage: %s", route)
			}
			return fakehttp.Print(ctx, call, stdout, http.MethodGet, route, nil)
		}
	}
	//nolint:wrapcheck // The shared CLI's errors are already descriptive.
	return fakehttp.CLI(ctx, Name, args, call, stdout, map[string]fakehttp.Command{
		"platforms": get("platforms"),
		"roms":      get("roms"),
		"links":     linksCommand,
		"token":     tokenCommand,
		"reload": func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
			if len(args) != 0 {
				return errors.New("usage: reload")
			}
			return fakehttp.Print(ctx, call, stdout, http.MethodPost, "reload", nil)
		},
	})
}

// printReply sends one control request and prints its JSON reply.
func printReply(ctx context.Context, call fakectl.Caller, stdout io.Writer, method, p string, body []byte) error {
	return fakehttp.Print(ctx, call, stdout, method, p, body) //nolint:wrapcheck // Already descriptive.
}

func linksCommand(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	if len(args) == 0 {
		return printReply(ctx, call, stdout, http.MethodGet, "links", nil)
	}
	if (args[0] != "approve" && args[0] != "deny") || len(args) > 2 {
		return errors.New("usage: links [approve|deny [user-code]]")
	}
	code := ""
	if len(args) == 2 {
		code = args[1]
	}
	return printReply(ctx, call, stdout, http.MethodPost, "links/"+args[0],
		jsonBody(map[string]string{"user_code": code}))
}

func tokenCommand(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return printReply(ctx, call, stdout, http.MethodGet, "token", nil)
	case len(args) == 1 && args[0] == "clear":
		return printReply(ctx, call, stdout, http.MethodDelete, "token", nil)
	case len(args) == 2 && args[0] == "set":
		return printReply(ctx, call, stdout, http.MethodPut, "token",
			jsonBody(map[string]string{"token": args[1]}))
	default:
		return errors.New("usage: token [set <token> | clear]")
	}
}
