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

// Package libretro is an offline fake of the libretro thumbnail server: a
// directory listing per playlist and kind folder, and a generated PNG per
// thumbnail. Point Core at it with [scraper.libretro_thumbnails] base_url.
package libretro

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scummvmnames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/library"
)

// Name is the fake's name, and the mount the sim gives its control API.
const Name = "libretro"

// Kinds are the thumbnail folders the server has under each playlist.
var Kinds = []string{"Named_Boxarts", "Named_Snaps", "Named_Titles"}

// Fault is a failure the fake applies to its service requests until cleared:
// a status to answer with, a delay, or both, optionally limited to a path
// prefix.
type Fault = fakehttp.Fault

// Thumbnail is one image the server holds.
type Thumbnail struct {
	Playlist string `json:"playlist"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	// Path is where it is served, unescaped.
	Path string `json:"path"`
}

// Options configures a fake libretro thumbnail server.
type Options struct {
	// LibraryRoot is a ROM tree with one ES-DE system folder per system, such
	// as <root>/nes/Game (USA).nes. Every ROM of a system libretro has a
	// playlist for gets a thumbnail of each of Kinds under the system's first
	// playlist, named the way the scraper looks it up.
	LibraryRoot string
	// Kinds limits the kinds generated for the library; nil means all.
	Kinds []string
	// Thumbnails are served as well as the library's.
	Thumbnails []Thumbnail
	// RequestLogLimit bounds the request log; 0 keeps 1000.
	RequestLogLimit int
}

// Server is a fake libretro thumbnail server.
type Server struct {
	loadErr  error
	service  http.Handler
	control  http.Handler
	log      *fakectl.RequestLog
	images   map[string]map[string]map[string]struct{} // playlist, kind, name
	listener fakehttp.Listener
	opts     Options
	faults   fakehttp.Faults
	mu       syncutil.RWMutex
}

// New returns a fake thumbnail server. A LibraryRoot that cannot be read
// leaves only Options.Thumbnails; Err reports why.
func New(opts Options) *Server {
	s := &Server{opts: opts, log: fakectl.NewRequestLog(opts.RequestLogLimit)}
	s.loadErr = s.load()
	s.service = fakehttp.Service(s.log, &s.faults, http.HandlerFunc(s.serve))
	s.control = s.controlMux()
	return s
}

func (s *Server) load() error {
	images := map[string]map[string]map[string]struct{}{}
	add := func(playlist, kind, name string) {
		if images[playlist] == nil {
			images[playlist] = map[string]map[string]struct{}{}
		}
		if images[playlist][kind] == nil {
			images[playlist][kind] = map[string]struct{}{}
		}
		images[playlist][kind][name] = struct{}{}
	}
	for _, t := range s.opts.Thumbnails {
		add(t.Playlist, t.Kind, ThumbnailName(t.Name))
	}
	var err error
	if s.opts.LibraryRoot != "" {
		kinds := s.opts.Kinds
		if kinds == nil {
			kinds = Kinds
		}
		var found []library.File
		found, err = library.Scan(s.opts.LibraryRoot)
		for _, f := range found {
			playlists := libretrothumbs.Playlists(f.SystemID)
			if len(playlists) == 0 {
				continue
			}
			for _, name := range Names(f.SystemID, f.Path) {
				for _, kind := range kinds {
					add(playlists[0], kind, name)
				}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images = images
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	return nil
}

// ThumbnailName applies libretro's file-name rule: &*/:`<>?\| become _.
func ThumbnailName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|", r) {
			return '_'
		}
		return r
	}, name)
}

// Names are the thumbnail names the server holds for the ROM at romPath, the
// path Core indexes it under: its file name without the extension, and for an
// arcade set or ScummVM target the game's title too, which is what libretro
// names those after.
func Names(systemID, romPath string) []string {
	file := filepath.ToSlash(romPath)
	base := path.Base(file)
	if ext := path.Ext(base); ext != "" && len(ext) <= 5 {
		base = strings.TrimSuffix(base, ext)
	}
	names := []string{ThumbnailName(base)}
	ext := path.Ext(file)
	if arcadenames.IsArcadeSystem(systemID) && arcadenames.SetArchive(ext) {
		if entry, ok := arcadenames.Lookup(base); ok {
			names = append(names, ThumbnailName(entry.Title))
		}
	}
	if scummvmnames.IsTargetFile(systemID, ext) {
		id := strings.TrimSuffix(base, path.Ext(base))
		if title, ok := scummvmnames.Title(id, path.Base(path.Dir(file))); ok {
			names = append(names, ThumbnailName(title))
		}
	}
	return names
}

// Err reports why the library tree could not be loaded, if it could not.
func (s *Server) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Reload rebuilds the thumbnails from the options and the library tree.
func (s *Server) Reload() error {
	err := s.load()
	s.mu.Lock()
	s.loadErr = err
	s.mu.Unlock()
	return err
}

// Handler serves the fake thumbnail server.
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

// SetFault applies a fault to every later request.
func (s *Server) SetFault(f Fault) error {
	if err := s.faults.Set(f); err != nil {
		return fmt.Errorf("set fault: %w", err)
	}
	return nil
}

// ClearFault removes the fault.
func (s *Server) ClearFault() { s.faults.Clear() }

// Thumbnails lists the served thumbnails, sorted.
func (s *Server) Thumbnails() []Thumbnail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Thumbnail
	for playlist, kinds := range s.images {
		for kind, names := range kinds {
			for name := range names {
				out = append(out, Thumbnail{
					Playlist: playlist, Kind: kind, Name: name,
					Path: "/" + playlist + "/" + kind + "/" + name + ".png",
				})
			}
		}
	}
	slices.SortFunc(out, func(a, b Thumbnail) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	playlist, kind, file := parts[0], parts[1], parts[2]
	if file == "" && s.opts.LibraryRoot != "" {
		// The scraper lists a folder before fetching from it: rescan, so ROMs
		// added after the fake started are served.
		_ = s.Reload()
	}
	s.mu.RLock()
	names, ok := s.images[playlist][kind]
	var listed []string
	_, exists := names[strings.TrimSuffix(file, ".png")]
	if ok && file == "" {
		listed = make([]string, 0, len(names))
		for name := range names {
			listed = append(listed, name)
		}
	}
	s.mu.RUnlock()
	switch {
	case !ok:
		http.NotFound(w, r)
	case file == "":
		slices.Sort(listed)
		writeListing(w, r.URL.Path, listed)
	case strings.HasSuffix(file, ".png") && exists:
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(library.PNG(r.URL.Path)) //nolint:gosec // A generated PNG, served as image/png.
	default:
		http.NotFound(w, r)
	}
}

// writeListing writes an Apache-style index page like the real server's.
func writeListing(w http.ResponseWriter, dir string, names []string) {
	var b strings.Builder
	title := html.EscapeString("Index of " + dir)
	b.WriteString("<html><head><title>" + title + "</title></head><body><h1>" + title + "</h1><pre>")
	b.WriteString(`<a href="../">../</a>` + "\n")
	for _, name := range names {
		file := name + ".png"
		b.WriteString(`<a href="` + url.PathEscape(file) + `">` + html.EscapeString(file) + "</a>\n")
	}
	b.WriteString("</pre></body></html>\n")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

// ControlHandler serves the control API, mounted by the sim at
// /fakes/libretro/:
//
//	GET    requests     logged requests; DELETE clears them
//	GET    fault        current fault; POST sets one, DELETE clears it
//	GET    thumbnails   served thumbnails
//	POST   reload       rebuild the thumbnails from the library tree
func (s *Server) ControlHandler() http.Handler { return s.control }

func (s *Server) controlMux() http.Handler {
	mux := fakehttp.ControlMux(s.log, &s.faults)
	mux.HandleFunc("GET /thumbnails", func(w http.ResponseWriter, _ *http.Request) {
		thumbs := s.Thumbnails()
		if thumbs == nil {
			thumbs = []Thumbnail{}
		}
		fakectl.WriteJSON(w, http.StatusOK, thumbs)
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Reload(); err != nil {
			fakehttp.WriteError(w, http.StatusInternalServerError, err)
			return
		}
		fakectl.WriteJSON(w, http.StatusOK, map[string]int{"thumbnails": len(s.Thumbnails())})
	})
	return mux
}

// CLI runs a control command against the fake through call and prints the
// JSON reply:
//
//	requests [clear]
//	fault [set <status> [path-prefix] | delay <ms> [path-prefix] | clear]
//	thumbnails
//	reload
func CLI(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	//nolint:wrapcheck // The shared CLI's errors are already descriptive.
	return fakehttp.CLI(ctx, Name, args, call, stdout, map[string]fakehttp.Command{
		"thumbnails": func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
			if len(args) != 0 {
				return errors.New("usage: thumbnails")
			}
			return fakehttp.Print(ctx, call, stdout, http.MethodGet, "thumbnails", nil)
		},
		"reload": func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
			if len(args) != 0 {
				return fmt.Errorf("usage: %s", "reload")
			}
			return fakehttp.Print(ctx, call, stdout, http.MethodPost, "reload", nil)
		},
	})
}
