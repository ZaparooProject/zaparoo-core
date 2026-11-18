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

package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
)

// ErrNoPlayer is returned when no controllable Jellyfin player matches the
// configured client.
var ErrNoPlayer = errors.New("no matching jellyfin player")

// playChunk is how many item IDs one play command carries. The IDs travel in
// the query string, and Jellyfin's web server limits the request line to 8 KiB.
const playChunk = 100

// selectSession picks the player to drive. The configured client is matched
// against each session's device name, then its client name, then its device
// ID. With no client configured, the only controllable player is used.
func selectSession(ctx context.Context, c Client) (*Session, error) {
	all, err := c.ControllableSessions(ctx)
	if err != nil {
		return nil, err //nolint:wrapcheck // the client error already names the request
	}
	sessions := make([]Session, 0, len(all))
	for _, s := range all {
		if s.ID != "" && s.DeviceID != c.DeviceID() {
			sessions = append(sessions, s)
		}
	}
	// The most recently active session wins when several match.
	slices.SortStableFunc(sessions, func(a, b Session) int {
		return b.LastActivityDate.Compare(a.LastActivityDate)
	})

	want := strings.TrimSpace(c.PlayerName())
	if want == "" {
		if len(sessions) == 1 {
			return &sessions[0], nil
		}
		if len(sessions) == 0 {
			return nil, fmt.Errorf("%w: no jellyfin player can be remote controlled, open a Jellyfin app first",
				ErrNoPlayer)
		}
		return nil, fmt.Errorf("%w: several jellyfin players are available, set client to one of: %s",
			ErrNoPlayer, describeSessions(sessions))
	}
	fields := []func(*Session) string{
		func(s *Session) string { return s.DeviceName },
		func(s *Session) string { return s.Client },
		func(s *Session) string { return s.DeviceID },
	}
	for _, field := range fields {
		for i := range sessions {
			if strings.EqualFold(strings.TrimSpace(field(&sessions[i])), want) {
				return &sessions[i], nil
			}
		}
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("%w: jellyfin player %q is not available, no player can be remote controlled",
			ErrNoPlayer, want)
	}
	return nil, fmt.Errorf("%w: jellyfin player %q is not available, available players: %s",
		ErrNoPlayer, want, describeSessions(sessions))
}

func describeSessions(sessions []Session) string {
	names := make([]string, 0, len(sessions))
	for _, s := range sessions {
		names = append(names, fmt.Sprintf("%s (%s)", s.DeviceName, s.Client))
	}
	return strings.Join(names, ", ")
}

// playItems starts items on a session. Lists longer than one command carries
// are queued behind the first chunk. It returns the IDs the player accepted:
// once the first chunk plays, a failure to queue the rest is logged rather
// than returned, since the media Core asked for is playing and failing the
// launch would leave it running with no stop or exit tracking.
func playItems(ctx context.Context, c Client, sessionID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("nothing to play")
	}
	command := "PlayNow"
	sent := 0
	for chunk := range slices.Chunk(ids, playChunk) {
		if err := c.Play(ctx, sessionID, command, chunk); err != nil {
			if sent == 0 {
				return nil, fmt.Errorf("failed to send %s to jellyfin player: %w", command, err)
			}
			log.Warn().Err(err).Int("queued", sent).Int("total", len(ids)).
				Msg("jellyfin player is playing, but the rest of the queue could not be added")
			return ids[:sent], nil
		}
		sent += len(chunk)
		command = "PlayLast"
	}
	return ids, nil
}
