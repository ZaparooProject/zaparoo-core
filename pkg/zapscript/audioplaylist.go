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
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
)

// maxPlaylistFileBytes bounds a playlist file read from disk.
const maxPlaylistFileBytes = 1 << 20

// errPlaylistFileTooLarge is returned for a playlist file over
// maxPlaylistFileBytes.
var errPlaylistFileTooLarge = errors.New("playlist file is too large")

// audioPlaylistLaunchError is returned by a launch whose target is a playlist file
// kept with audio. The command runner starts it as a playlist once the launch
// guard is released, since the playlist's first item takes the guard itself.
type audioPlaylistLaunchError struct {
	path string
}

func (a *audioPlaylistLaunchError) Error() string {
	return "audio playlist file: " + a.path
}

// isAudioPlaylistExt reports whether path names a playlist file kept with
// audio.
func isAudioPlaylistExt(path string) bool {
	return slices.Contains(audio.PlaylistExtensions, strings.ToLower(filepath.Ext(path)))
}

// isAudioPlaylistTarget reports whether a launch target is an audio playlist
// file: a .pls or .m3u that native audio would be the one to launch. A
// playlist file a system's launcher takes, such as a multi-disc .m3u in a
// console's folder, belongs to that launcher.
func isAudioPlaylistTarget(pl platforms.Platform, env *platforms.CmdEnv, target *launchTarget) bool {
	// A playlist URL is resolved by native audio, not read from disk.
	if target.audioStream != nil || audio.IsStreamURL(target.path) || !isAudioPlaylistExt(target.path) {
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
	launcher, err := helpers.FindLauncher(env.Cfg, pl, target.path)
	return err == nil && launcher.ID == platforms.NativeAudioLauncherID
}

// readPlaylistFile reads the items of a .pls or .m3u file.
func readPlaylistFile(path string) ([]playlists.PlaylistItem, error) {
	if strings.EqualFold(filepath.Ext(path), ".m3u") {
		return readM3UFile(path)
	}
	return readPlsFile(path)
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
// comments, and #EXTINF:<duration>,<title> names the entry after it. An entry
// that is not a URL or an absolute path is resolved against the playlist's
// folder when the file is there.
func readM3UFile(path string) ([]playlists.PlaylistItem, error) {
	content, err := readBoundedFile(path)
	if err != nil {
		return nil, err
	}
	content = []byte(strings.TrimPrefix(string(content), "\xef\xbb\xbf"))

	items := make([]playlists.PlaylistItem, 0)
	title := ""
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				if _, t, found := strings.Cut(rest, ","); found {
					title = strings.TrimSpace(t)
				}
			}
			continue
		}
		items = append(items, playlists.PlaylistItem{
			Name:      title,
			ZapScript: literalLaunch(resolvePlaylistEntry(path, line)),
		})
		title = ""
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no items found in m3u file: %s", path)
	}
	return items, nil
}

// resolvePlaylistEntry turns a relative file entry into the absolute path of
// the file, or track of a file, next to the playlist, leaving URLs, absolute paths and entries
// with no such file as written.
func resolvePlaylistEntry(playlistPath, entry string) string {
	if strings.Contains(entry, "://") || filepath.IsAbs(entry) {
		return entry
	}
	rel := filepath.FromSlash(strings.ReplaceAll(entry, `\`, "/"))
	candidate := filepath.Join(filepath.Dir(playlistPath), rel)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// One track of a multi-track chiptune file is <file>/<n>.
	if file, _, ok := audio.SplitTrackPath(candidate); ok {
		if _, err := os.Stat(file); err == nil {
			return candidate
		}
	}
	return entry
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

// startAudioPlaylistFile plays a playlist file kept with audio. Its entries
// run as ZapScript, so a stream URL plays as radio and a ZapLink resolves
// the way a scanned one does. The file is content found on disk, not a
// script the user wrote, so its entries run untrusted.
func startAudioPlaylistFile(env *platforms.CmdEnv, path string) (platforms.CmdResult, error) {
	items, err := readPlaylistFile(path)
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
