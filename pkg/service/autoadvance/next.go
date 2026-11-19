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

package autoadvance

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
)

// maxLabelBytes keeps an item name inside what a UI title can carry.
const maxLabelBytes = 200

// nextAfterCompletion decides where a playlist goes when its current item is
// done: the same item again for repeat-one, the next item, the first item
// again for repeat-all, or nowhere. It reports false at the end of a playlist
// that does not repeat, which is the caller's cue to close it. Both slots use
// it, so a track that finished, a game that exited and a timer that ran out
// all end a playlist the same way.
func nextAfterCompletion(pls *playlists.Playlist) (*playlists.Playlist, bool) {
	if pls == nil || len(pls.Items) == 0 {
		return nil, false
	}
	if pls.LoopOne {
		// ForceRelaunch bypasses the playlistNeedsUpdate dedup.
		next := playlists.Play(*pls)
		next.ForceRelaunch = true
		return next, true
	}
	return nextItem(pls)
}

// nextItem is the item after the current one, wrapping only when the
// playlist loops. Unlike nextAfterCompletion it ignores single-item repeat:
// it is where a person asking to move on goes.
func nextItem(pls *playlists.Playlist) (*playlists.Playlist, bool) {
	if pls == nil || len(pls.Items) == 0 || (pls.Index+1 >= len(pls.Items) && !pls.Loop) {
		return nil, false
	}
	next := playlists.Next(*pls) // Next already wraps to 0
	next.Playing = true
	if len(pls.Items) <= 1 {
		// A single-item loop lands on the item it left.
		next.ForceRelaunch = true
	}
	return next, true
}

// itemLabel is what a countdown calls an item: its name, or else what its
// script points at.
func itemLabel(item playlists.PlaylistItem) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		if strings.HasPrefix(item.ZapScript, "**") {
			name = item.ZapScript
		} else {
			name = filepath.Base(item.ZapScript)
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
	}
	if len(name) > maxLabelBytes {
		name = strings.ToValidUTF8(name[:maxLabelBytes], "") + "..."
	}
	return name
}
