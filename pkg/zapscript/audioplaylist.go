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

package zapscript

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

const (
	// maxPlaylistFileBytes bounds a playlist file read from disk.
	maxPlaylistFileBytes = 1 << 20
	// maxPlaylistItems bounds the items of a playlist read from a file,
	// counting those of the playlist files it lists.
	maxPlaylistItems = 10000
	// maxPlaylistFileReads bounds how many playlist files one playlist reads,
	// itself included.
	maxPlaylistFileReads = 256
)

var (
	// errPlaylistFileTooLarge is returned for a playlist file over
	// maxPlaylistFileBytes.
	errPlaylistFileTooLarge = errors.New("playlist file is too large")
	// errHLSManifest is returned for an m3u that is an HLS stream manifest
	// rather than a list of media. Native audio plays HLS from its URL.
	errHLSManifest = errors.New("playlist file is an HLS manifest, play it from its URL")
)

// playlistFileLaunchError is returned by a launch whose target is a playlist
// file. The command runner starts it as a playlist once the launch guard is
// released, since the playlist's first item takes the guard itself.
type playlistFileLaunchError struct {
	path string
}

func (a *playlistFileLaunchError) Error() string {
	return "playlist file: " + a.path
}

// isPlaylistFileExt reports whether path names a playlist file.
func isPlaylistFileExt(path string) bool {
	return slices.Contains(audio.PlaylistExtensions, strings.ToLower(filepath.Ext(path)))
}

// isPlaylistFileTarget reports whether a launch target is a playlist file to
// play as a playlist. A playlist file another launcher takes, such as a
// multi-disc .m3u in a console's folder, belongs to that launcher.
func isPlaylistFileTarget(pl platforms.Platform, env *platforms.CmdEnv, target *launchTarget) bool {
	// A playlist URL is resolved by native audio, not read from disk.
	if target.audioStream != nil || audio.IsStreamURL(target.path) || !isPlaylistFileExt(target.path) {
		return false
	}
	if target.systemID != "" {
		return target.systemID == systemdefs.SystemAudio
	}
	if id := env.Cmd.AdvArgs.Get(zapscript.KeyLauncher); id != "" {
		return id == platforms.NativeAudioLauncherID
	}
	// The background slot only plays native audio.
	if slot, err := commandSlot(env); err == nil && slot == mediaslot.Background {
		return findLauncher(pl, env, platforms.NativeAudioLauncherID) != nil
	}
	return !claimedByOtherLauncher(env.Cfg, pl, target.path)
}

// claimedByOtherLauncher reports whether a launcher other than native audio
// takes path.
func claimedByOtherLauncher(cfg *config.Instance, pl platforms.Platform, path string) bool {
	if pl == nil {
		return false
	}
	return hasOtherThanNativeAudio(helpers.PathToLaunchers(cfg, pl, path))
}

// hasOtherThanNativeAudio reports whether launchers holds one that is not
// native audio.
func hasOtherThanNativeAudio(launchers []platforms.Launcher) bool {
	for i := range launchers {
		if launchers[i].ID != platforms.NativeAudioLauncherID {
			return true
		}
	}
	return false
}

// playlistEntry is one entry of a playlist file as written.
type playlistEntry struct {
	title    string
	location string
	// script marks a pls entry, which runs as ZapScript when it is not a
	// file. An m3u entry is always a location.
	script bool
}

// playlistFileReader reads a playlist file into playlist items. A listed
// playlist file that no other launcher takes has its items read in its
// place, so playlist files nest.
type playlistFileReader struct {
	cfg      *config.Instance
	pl       platforms.Platform
	pathRoot string
	// reading is the chain of playlist files being read, outermost first,
	// as resolved by playlistChainKey.
	reading   []string
	items     []playlists.PlaylistItem
	reads     int
	truncated bool
}

// readPlaylistFile reads the items of a .pls, .m3u or .m3u8 file. Relative
// entries are found next to the playlist, then under the token's path root.
func readPlaylistFile(pl platforms.Platform, env *platforms.CmdEnv, path string) ([]playlists.PlaylistItem, error) {
	r := &playlistFileReader{pl: pl}
	if env != nil {
		r.cfg = env.Cfg
		if filepath.IsAbs(env.PathRoot) {
			r.pathRoot = env.PathRoot
		}
	}
	if err := r.read(path); err != nil {
		return nil, err
	}
	if len(r.items) == 0 {
		return nil, fmt.Errorf("no items found in playlist file: %s", path)
	}
	return r.items, nil
}

func (r *playlistFileReader) read(path string) error {
	r.reads++
	var entries []playlistEntry
	var err error
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".m3u" || ext == ".m3u8" {
		entries, err = readM3UFile(path)
	} else {
		entries, err = readPlsFile(path)
	}
	if err != nil {
		return err
	}
	r.reading = append(r.reading, playlistChainKey(path))
	defer func() { r.reading = r.reading[:len(r.reading)-1] }()
	for _, e := range entries {
		if len(r.items) >= maxPlaylistItems {
			if !r.truncated {
				r.truncated = true
				log.Warn().Str("path", path).Int("max", maxPlaylistItems).
					Msg("playlist file has too many items, the rest are left out")
			}
			return nil
		}
		r.add(path, e)
	}
	return nil
}

// add turns one entry into playlist items.
func (r *playlistFileReader) add(playlistPath string, e playlistEntry) {
	loc := e.location
	if strings.ContainsFunc(loc, isDisallowedControl) {
		log.Warn().Str("path", playlistPath).Msg("playlist entry has control characters, skipping it")
		return
	}
	if strings.Contains(loc, "://") {
		if !validEntryURL(loc) {
			log.Warn().Str("path", playlistPath).Str("entry", loc).Msg("playlist entry is not a valid URL, skipping it")
			return
		}
		// A URL is a stream or ZapLink location, so its query string is
		// part of it rather than launch arguments.
		r.items = append(r.items, playlists.PlaylistItem{Name: e.title, ZapScript: literalLaunch(loc)})
		return
	}
	if e.script {
		// A pls file entry may carry launch arguments after its name.
		if file, args, ok := splitEntryArgs(loc); ok {
			if found, ok := r.find(playlistPath, file); ok {
				// Running it would replace this playlist with a fresh read
				// of a file in the chain, which can list itself again.
				if r.isReading(found) {
					log.Warn().Str("path", found).Msg("playlist file lists itself, skipping the entry")
					return
				}
				r.items = append(r.items, playlists.PlaylistItem{Name: e.title, ZapScript: found + args})
				return
			}
		}
	}
	found, ok := r.find(playlistPath, loc)
	if ok && r.isNestedPlaylist(found) {
		r.readNested(found)
		return
	}
	script := found
	if !e.script {
		script = literalLaunch(found)
	}
	r.items = append(r.items, playlists.PlaylistItem{Name: e.title, ZapScript: script})
}

// isNestedPlaylist reports whether a found entry is a playlist file to read
// in place of the entry.
func (r *playlistFileReader) isNestedPlaylist(path string) bool {
	if !isPlaylistFileExt(path) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return !claimedByOtherLauncher(r.cfg, r.pl, path)
}

func (r *playlistFileReader) readNested(path string) {
	if r.isReading(path) {
		log.Warn().Str("path", path).Msg("playlist file lists itself, skipping the entry")
		return
	}
	if r.reads >= maxPlaylistFileReads {
		log.Warn().Str("path", path).Int("max", maxPlaylistFileReads).
			Msg("playlist lists too many playlist files, skipping the entry")
		return
	}
	if err := r.read(path); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("skipping a listed playlist file")
	}
}

// isReading reports whether path is a playlist file in the chain being read.
func (r *playlistFileReader) isReading(path string) bool {
	return slices.Contains(r.reading, playlistChainKey(path))
}

// playlistChainKey identifies a playlist file in the chain being read by its
// path with symlinks resolved, so a symlinked folder cannot lead back to a
// file already being read under a longer path.
func playlistChainKey(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// find resolves a file entry to the file, or track of a file, it names: as
// written when absolute, otherwise next to the playlist and then under the
// token's path root. An entry with no such file is returned as written.
func (r *playlistFileReader) find(playlistPath, entry string) (string, bool) {
	if filepath.IsAbs(entry) {
		return entry, pathExists(entry)
	}
	rel := filepath.FromSlash(strings.ReplaceAll(entry, `\`, "/"))
	roots := []string{filepath.Dir(playlistPath)}
	if r.pathRoot != "" {
		roots = append(roots, r.pathRoot)
	}
	for _, root := range roots {
		candidate := filepath.Join(root, rel)
		if pathExists(candidate) {
			return candidate, true
		}
		// One track of a multi-track chiptune file is <file>/<n>.
		if file, _, ok := audio.SplitTrackPath(candidate); ok && pathExists(file) {
			return candidate, true
		}
	}
	return entry, false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// splitEntryArgs splits launch arguments, a trailing ?..., off the file name
// of a pls entry.
func splitEntryArgs(entry string) (file, args string, ok bool) {
	i := strings.LastIndex(entry, "?")
	if i <= 0 || i < strings.LastIndexAny(entry, `/\`) {
		return entry, "", false
	}
	return entry[:i], entry[i:], true
}

// isDisallowedControl reports a control character no playlist entry has.
func isDisallowedControl(r rune) bool {
	return r != '\t' && unicode.IsControl(r)
}

// validEntryURL reports whether a URL entry parses with a scheme, and a host
// when it is a web URL.
func validEntryURL(entry string) bool {
	u, err := url.Parse(entry)
	if err != nil || u.Scheme == "" {
		return false
	}
	if scheme := strings.ToLower(u.Scheme); (scheme == "http" || scheme == "https") && u.Host == "" {
		return false
	}
	return true
}

// readBoundedFile reads a playlist file, refusing one over
// maxPlaylistFileBytes.
func readBoundedFile(path string) ([]byte, error) {
	//nolint:gosec // Safe: reads playlist files for media management
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read playlist file '%s': %w", path, err)
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(io.LimitReader(f, maxPlaylistFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read playlist file '%s': %w", path, err)
	}
	if len(content) > maxPlaylistFileBytes {
		return nil, fmt.Errorf("%w: %s", errPlaylistFileTooLarge, path)
	}
	return content, nil
}

// readM3UFile reads an m3u playlist: one entry per line, # lines are
// comments, and #EXTINF:<duration>,<title> names the entry after it. An HLS
// manifest, marked by #EXT-X- tags, is refused.
func readM3UFile(path string) ([]playlistEntry, error) {
	content, err := readBoundedFile(path)
	if err != nil {
		return nil, err
	}
	content = []byte(strings.TrimPrefix(string(content), "\xef\xbb\xbf"))

	entries := make([]playlistEntry, 0)
	title := ""
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(strings.ToUpper(line), "#EXT-X-") {
				return nil, fmt.Errorf("%w: %s", errHLSManifest, path)
			}
			if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				if _, t, found := strings.Cut(rest, ","); found {
					title = strings.TrimSpace(t)
				}
			}
			continue
		}
		entries = append(entries, playlistEntry{title: title, location: line})
		title = ""
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no items found in m3u file: %s", path)
	}
	return entries, nil
}

// zapScriptSpecials are the characters that mean something to the ZapScript
// parser in a launch: arguments, commands, expressions, titles, JSON and
// traits.
const zapScriptSpecials = "^?&=|*[]{}@#:,\"'"

// literalLaunch escapes a file path or URL so it runs as a launch of exactly
// that location. A stream URL keeps its query string instead of having it
// read as launch arguments.
func literalLaunch(location string) string {
	var b strings.Builder
	b.Grow(len(location) + 8)
	for _, r := range location {
		if strings.ContainsRune(zapScriptSpecials, r) {
			_, _ = b.WriteRune('^')
		}
		_, _ = b.WriteRune(r)
	}
	return b.String()
}

// startPlaylistFile plays a playlist file. Its entries run as ZapScript, so a
// stream URL plays as radio and a ZapLink resolves the way a scanned one
// does. The file is content found on disk, not a script the user wrote, so
// its entries run untrusted.
func startPlaylistFile(pl platforms.Platform, env *platforms.CmdEnv, path string) (platforms.CmdResult, error) {
	items, err := readPlaylistFile(pl, env, path)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	slot, err := commandSlot(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	pls := playlists.NewPlaylist(path, name, items)
	pls.Slot = slot
	pls.Unsafe = true
	pls.AllowedCommands = env.AllowedCommands
	pls = playlists.Play(*pls)
	if err := queuePlaylistUpdate(env, pls); err != nil {
		return platforms.CmdResult{}, err
	}
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}
