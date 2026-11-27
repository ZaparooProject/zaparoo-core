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

package platforms

import (
	"context"
	"errors"
)

// ErrLibraryRefused wraps a refusal by the media library for a reason the
// request itself carries, such as media that is not indexed, a deck this
// device does not own or a locked device.
var ErrLibraryRefused = errors.New("library request refused")

// MediaRef names one indexed media item, by its media ID or by system and
// path.
type MediaRef struct {
	SystemID string
	Path     string
	MediaID  int64
}

// LibraryItem is one media item of a library list.
type LibraryItem struct {
	SystemID string
	Name     string
	Path     string
	MediaID  int64
}

// MediaLibrary is the media library the API serves, lent to ZapScript so a
// command reads and edits the same lists the app shows. Each method answers
// the way the API method it stands for does.
type MediaLibrary interface {
	// Flagged lists media.search for every indexed game holding a user flag
	// such as "favorite", in name order.
	Flagged(ctx context.Context, flag string) ([]LibraryItem, error)
	// Recommended lists media.recommended's items in order. An empty section
	// is every section, RecommendedQueue is the queue layout and any other
	// value keeps the sections of that kind.
	Recommended(ctx context.Context, section string) ([]LibraryItem, error)
	// Similar lists media.similar for ref.
	Similar(ctx context.Context, ref MediaRef) ([]LibraryItem, error)
	// Versions lists media.versions for ref, the default version first.
	Versions(ctx context.Context, ref MediaRef) ([]LibraryItem, error)
	// MostPlayed lists media.history.top, of one system when systemID is set.
	MostPlayed(ctx context.Context, systemID string) ([]LibraryItem, error)
	// SetUserFlag turns a user flag such as "favorite" on or off for ref, as
	// media.tags.update does.
	SetUserFlag(ctx context.Context, ref MediaRef, flag string, on bool) error
	// EditDeck adds ref to an owned deck, or removes it, as decks.update
	// does. changed is false when the deck already was as asked.
	EditDeck(ctx context.Context, deckID string, ref MediaRef, add bool) (changed bool, err error)
}

// RecommendedQueue asks MediaLibrary.Recommended for the queue layout.
const RecommendedQueue = "queue"
