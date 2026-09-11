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

package steam

import (
	"bytes"
	"io"
	"path/filepath"
	"strconv"

	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
)

const (
	maxInstallMetadataSize = 1 << 20
	steamFullyInstalled    = 4
)

// isAppInstalled requires positive local evidence. Missing, inaccessible, or
// invalid metadata all take the details path; an explicit run bypasses this check.
func (c *Client) isAppInstalled(steamRoot, id string) bool {
	if !filepath.IsAbs(steamRoot) {
		return false
	}
	main := findSteamAppsDirFS(c.fs, steamRoot)
	if c.libraryHasInstalledApp(main, id) {
		return true
	}
	metadata, ok := c.readInstallMetadata(filepath.Join(main, "libraryfolders.vdf"))
	if !ok {
		return false
	}
	libraries, ok := metadata["libraryfolders"].(map[string]any)
	if !ok {
		return false
	}
	for key, entry := range libraries {
		if _, err := strconv.ParseUint(key, 10, 32); err != nil {
			continue
		}
		var path string
		switch library := entry.(type) {
		case map[string]any:
			path, _ = library["path"].(string) //nolint:revive // Invalid paths are skipped below.
		case string:
			// Older libraryfolders files store numbered library paths directly.
			path = library
		}
		if !filepath.IsAbs(path) {
			continue
		}
		// The cached "apps" index may lag a moved or newly installed game.
		// Check the manifest itself in every configured library instead.
		if c.libraryHasInstalledApp(findSteamAppsDirFS(c.fs, path), id) {
			return true
		}
	}
	return false
}

func (c *Client) libraryHasInstalledApp(steamApps, id string) bool {
	metadata, ok := c.readInstallMetadata(filepath.Join(steamApps, "appmanifest_"+id+".acf"))
	if !ok {
		return false
	}
	state, ok := metadata["appstate"].(map[string]any)
	if !ok {
		return false
	}
	manifestID, _ := state["appid"].(string)      //nolint:revive // Missing fields fail validation below.
	flags, _ := state["stateflags"].(string)      //nolint:revive // Missing fields fail validation below.
	installDir, _ := state["installdir"].(string) //nolint:revive // Missing fields fail validation below.
	stateFlags, err := strconv.ParseUint(flags, 10, 32)
	if manifestID != id || err != nil || stateFlags&steamFullyInstalled == 0 ||
		!filepath.IsLocal(installDir) || filepath.Clean(installDir) == "." {
		return false
	}
	// A manifest can survive removal or an interrupted install. Updates are
	// still launchable when FullyInstalled is set alongside other state bits.
	info, err := c.fs.Stat(filepath.Join(steamApps, "common", installDir))
	return err == nil && info.IsDir()
}

func (c *Client) readInstallMetadata(path string) (map[string]any, bool) {
	info, err := c.fs.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstallMetadataSize {
		log.Debug().Err(err).Str("path", path).Msg("Steam installation metadata unavailable")
		return nil, false
	}
	f, err := c.fs.Open(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("cannot read Steam installation metadata")
		return nil, false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", path).Msg("cannot close Steam installation metadata")
		}
	}()
	data, err := io.ReadAll(io.LimitReader(f, maxInstallMetadataSize+1))
	if err != nil || len(data) > maxInstallMetadataSize || !validInstallVDF(data) {
		log.Debug().Err(err).Str("path", path).Msg("invalid Steam installation metadata")
		return nil, false
	}
	// The VDF parser requires a newline to terminate a comment at EOF.
	metadata, err := vdf.NewParser(bytes.NewReader(append(data, '\n'))).Parse()
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("cannot parse Steam installation metadata")
		return nil, false
	}
	return normalizeVDFKeys(metadata), true
}

// validInstallVDF rejects truncated maps and excessive nesting before using the
// permissive VDF parser. It also rejects NUL, which that parser treats as EOF.
func validInstallVDF(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	// Each level alternates keys and values; the outer level permits one map.
	wantValue := []bool{false}
	closed := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			continue
		}
		if closed {
			return false
		}
		level := len(wantValue) - 1
		switch ch {
		case '{':
			if !wantValue[level] || len(wantValue) >= 64 {
				return false
			}
			wantValue[level] = false
			wantValue = append(wantValue, false)
		case '}':
			if level == 0 || wantValue[level] {
				return false
			}
			wantValue = wantValue[:level]
			closed = level == 1
		default:
			if level == 0 && wantValue[level] {
				return false
			}
			if ch == '"' {
				for i++; i < len(data) && data[i] != '"'; i++ {
					if data[i] == '\\' {
						i++
					}
				}
				if i >= len(data) {
					return false
				}
			} else {
				if !installVDFIdent(ch) {
					return false
				}
				for i+1 < len(data) && installVDFIdent(data[i+1]) {
					i++
				}
			}
			wantValue[level] = !wantValue[level]
		}
	}
	return closed
}

func installVDFIdent(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_'
}
