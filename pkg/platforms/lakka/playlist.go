//go:build linux && !android

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

package lakka

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// playlistsDir is RetroArch's playlist_directory on Lakka.
	playlistsDir      = "/storage/playlists"
	playlistExtension = ".lpl"
	// detectCore is what RetroArch records when an entry has no fixed core.
	detectCore = "DETECT"

	// Playlists are written by RetroArch but sit on a share anyone on the
	// network can write to, so their size and shape are bounded.
	maxPlaylistSize    = 16 << 20
	maxPlaylistFiles   = 512
	maxPlaylistEntries = 100_000
	maxPlaylistField   = 4096
	maxPlaylistLabel   = 1024
	// legacyEntryLines is how many lines each entry takes in the plain text
	// playlist format RetroArch wrote before 1.7.6.
	legacyEntryLines = 6
)

// playlistEntry is one item of a RetroArch playlist, as written.
type playlistEntry struct {
	Path     string
	Label    string
	CorePath string
	CoreName string
	CRC32    string
	DBName   string
}

// playlistFile is a parsed RetroArch playlist.
type playlistFile struct {
	DefaultCorePath string
	Entries         []playlistEntry
}

// lenientString decodes a JSON string and treats any other value as empty, so
// one odd field does not discard its entry.
type lenientString string

func (s *lenientString) UnmarshalJSON(data []byte) error {
	var v string
	if json.Unmarshal(data, &v) == nil {
		*s = lenientString(v)
	}
	return nil
}

//nolint:tagliatelle // RetroArch playlists use snake_case fields.
type jsonPlaylistItem struct {
	Path     lenientString `json:"path"`
	Label    lenientString `json:"label"`
	CorePath lenientString `json:"core_path"`
	CoreName lenientString `json:"core_name"`
	CRC32    lenientString `json:"crc32"`
	DBName   lenientString `json:"db_name"`
}

// parsePlaylist reads a RetroArch playlist in either the JSON format or the
// older six-lines-per-entry text format. Entries without a usable path are
// dropped; field values are otherwise returned as written.
func parsePlaylist(data []byte) (playlistFile, error) {
	if len(data) > maxPlaylistSize {
		return playlistFile{}, fmt.Errorf("playlist is larger than %d bytes", maxPlaylistSize)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return playlistFile{}, nil
	}
	if trimmed[0] == '{' {
		return parseJSONPlaylist(trimmed)
	}
	return parseLegacyPlaylist(data)
}

func parseJSONPlaylist(data []byte) (playlistFile, error) {
	//nolint:tagliatelle // RetroArch playlists use snake_case fields.
	var doc struct {
		DefaultCorePath lenientString     `json:"default_core_path"`
		Items           []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return playlistFile{}, fmt.Errorf("decode playlist: %w", err)
	}
	pl := playlistFile{DefaultCorePath: cleanField(string(doc.DefaultCorePath))}
	for _, raw := range doc.Items {
		if len(pl.Entries) >= maxPlaylistEntries {
			break
		}
		var item jsonPlaylistItem
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		entry := playlistEntry{
			Path:     cleanField(string(item.Path)),
			Label:    cleanField(string(item.Label)),
			CorePath: cleanField(string(item.CorePath)),
			CoreName: cleanField(string(item.CoreName)),
			CRC32:    cleanField(string(item.CRC32)),
			DBName:   cleanField(string(item.DBName)),
		}
		if entry.Path != "" {
			pl.Entries = append(pl.Entries, entry)
		}
	}
	return pl, nil
}

func parseLegacyPlaylist(data []byte) (playlistFile, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	pl := playlistFile{}
	for i := 0; i+legacyEntryLines <= len(lines); i += legacyEntryLines {
		if len(pl.Entries) >= maxPlaylistEntries {
			break
		}
		entry := playlistEntry{
			Path:     cleanField(lines[i]),
			Label:    cleanField(lines[i+1]),
			CorePath: cleanField(lines[i+2]),
			CoreName: cleanField(lines[i+3]),
			CRC32:    cleanField(lines[i+4]),
			DBName:   cleanField(lines[i+5]),
		}
		if entry.Path != "" {
			pl.Entries = append(pl.Entries, entry)
		}
	}
	if len(pl.Entries) == 0 && strings.TrimSpace(string(data)) != "" {
		return playlistFile{}, errors.New("not a playlist")
	}
	return pl, nil
}

// cleanField trims a value and empties it when it is too long or holds a
// control character no path or name has.
func cleanField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxPlaylistField || strings.ContainsAny(s, "\x00\r\n") {
		return ""
	}
	return s
}

// playlistTarget is the file a playlist entry plays. RetroArch names a file
// inside an archive as "archive.zip#file"; the index holds the archive, which
// RetroArch loads directly.
func playlistTarget(entryPath string) (path, inner string, ok bool) {
	if entryPath == "" || !filepath.IsAbs(entryPath) {
		return "", "", false
	}
	lower := strings.ToLower(entryPath)
	for _, ext := range []string{".zip#", ".7z#"} {
		if i := strings.Index(lower, ext); i >= 0 {
			cut := i + len(ext) - 1
			inner = entryPath[cut+1:]
			entryPath = entryPath[:cut]
			break
		}
	}
	path = filepath.Clean(entryPath)
	if path == "/" {
		return "", "", false
	}
	return path, inner, true
}

// playlistItem is a playlist entry resolved to a Core system and the path the
// index holds for it.
type playlistItem struct {
	SystemID string
	Path     string
	// Inner is the file inside the archive at Path, if any.
	Inner string
	Name  string
	// Core is the core path the playlist recorded, or empty when the entry
	// leaves the choice to RetroArch.
	Core string
}

type playlistIndex struct {
	bySystem map[string][]playlistItem
	byPath   map[string]playlistItem
}

type cachedPlaylist struct {
	modTime time.Time
	file    playlistFile
	size    int64
}

// playlistStore reads RetroArch's playlists and keeps the parsed result until a
// playlist file changes.
type playlistStore struct {
	fs        afero.Fs
	hasSystem func(string) bool
	index     *playlistIndex
	files     map[string]cachedPlaylist
	dir       string
	mu        syncutil.Mutex
}

func newPlaylistStore(fs afero.Fs, dir string, hasSystem func(string) bool) *playlistStore {
	return &playlistStore{fs: fs, dir: dir, hasSystem: hasSystem, files: make(map[string]cachedPlaylist)}
}

// systemFor maps a libretro database or playlist name to the first of its
// systems this platform can launch.
func (s *playlistStore) systemFor(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(name), playlistExtension)
	for _, id := range libretrothumbs.SystemsForDatabase(name) {
		if s.hasSystem(id) {
			return id
		}
	}
	return ""
}

// cached returns the last index read, reading one if there is none.
func (s *playlistStore) cached() *playlistIndex {
	s.mu.Lock()
	index := s.index
	s.mu.Unlock()
	if index != nil {
		return index
	}
	return s.load()
}

// lookup reports the playlist item indexed at path for systemID.
func (ix *playlistIndex) lookup(path, systemID string) (playlistItem, bool) {
	item, ok := ix.byPath[path]
	if !ok || item.SystemID != systemID {
		return playlistItem{}, false
	}
	return item, true
}

// load rereads the playlist directory, parsing only playlists that changed.
func (s *playlistStore) load() *playlistIndex {
	s.mu.Lock()
	defer s.mu.Unlock()

	infos, err := afero.ReadDir(s.fs, s.dir)
	if err != nil {
		log.Debug().Err(err).Str("dir", s.dir).Msg("cannot list retroarch playlists")
		infos = nil
	}

	names := make([]string, 0, len(infos))
	changed := false
	for _, info := range infos {
		name := info.Name()
		if info.IsDir() || !strings.EqualFold(filepath.Ext(name), playlistExtension) {
			continue
		}
		if len(names) >= maxPlaylistFiles {
			log.Debug().Str("dir", s.dir).Int("max", maxPlaylistFiles).
				Msg("too many retroarch playlists, ignoring the rest")
			break
		}
		names = append(names, name)
		if cached, ok := s.files[name]; ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
			continue
		}
		changed = true
		s.files[name] = cachedPlaylist{
			modTime: info.ModTime(),
			size:    info.Size(),
			file:    s.read(name, info.Size()),
		}
	}
	for name := range s.files {
		if !slices.Contains(names, name) {
			delete(s.files, name)
			changed = true
		}
	}
	if s.index != nil && !changed {
		return s.index
	}
	s.index = s.build(names)
	return s.index
}

func (s *playlistStore) read(name string, size int64) playlistFile {
	path := filepath.Join(s.dir, name)
	if size > maxPlaylistSize {
		log.Debug().Str("path", path).Int64("size", size).Msg("skipping oversized retroarch playlist")
		return playlistFile{}
	}
	f, err := s.fs.Open(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("cannot open retroarch playlist")
		return playlistFile{}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxPlaylistSize+1))
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("cannot read retroarch playlist")
		return playlistFile{}
	}
	pl, err := parsePlaylist(data)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("skipping unreadable retroarch playlist")
		return playlistFile{}
	}
	return pl
}

// build merges the parsed playlists. Playlists named after a system come
// first, so their entries and recorded cores win over the same file in a
// mixed playlist such as the history or favorites.
func (s *playlistStore) build(names []string) *playlistIndex {
	index := &playlistIndex{
		bySystem: make(map[string][]playlistItem),
		byPath:   make(map[string]playlistItem),
	}
	slices.Sort(names)
	type ordered struct {
		name     string
		systemID string
	}
	var named, mixed []ordered
	for _, name := range names {
		if id := s.systemFor(name); id != "" {
			named = append(named, ordered{name: name, systemID: id})
		} else {
			mixed = append(mixed, ordered{name: name})
		}
	}
	for _, o := range append(named, mixed...) {
		pl := s.files[o.name].file
		unmapped := 0
		for i := range pl.Entries {
			entry := &pl.Entries[i]
			systemID := o.systemID
			if systemID == "" {
				systemID = s.systemFor(entry.DBName)
			}
			if systemID == "" {
				unmapped++
				continue
			}
			path, inner, ok := playlistTarget(entry.Path)
			if !ok {
				continue
			}
			if _, seen := index.byPath[path]; seen {
				continue
			}
			item := playlistItem{
				SystemID: systemID,
				Path:     path,
				Inner:    inner,
				Name:     entryName(entry, path, inner),
				Core:     entryCore(entry.CorePath, pl.DefaultCorePath),
			}
			index.byPath[path] = item
			index.bySystem[systemID] = append(index.bySystem[systemID], item)
		}
		if unmapped > 0 {
			log.Debug().Str("playlist", o.name).Int("entries", unmapped).
				Msg("skipping retroarch playlist entries with no matching system")
		}
	}
	return index
}

func entryName(entry *playlistEntry, path, inner string) string {
	if entry.Label != "" && len(entry.Label) <= maxPlaylistLabel {
		return entry.Label
	}
	file := filepath.Base(path)
	if inner != "" {
		file = filepath.Base(inner)
	}
	return strings.TrimSuffix(file, filepath.Ext(file))
}

func entryCore(corePath, defaultCorePath string) string {
	for _, core := range []string{corePath, defaultCorePath} {
		if core != "" && !strings.EqualFold(core, detectCore) {
			return core
		}
	}
	return ""
}
