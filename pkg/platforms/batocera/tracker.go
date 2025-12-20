//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package batocera

import (
	"context"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// startGameTracker starts a background goroutine that polls the EmulationStation API
// every 2 seconds to detect externally launched/closed games. Returns a cleanup function
// that should be called to stop the tracker.
func (p *Platform) startGameTracker(
	setActiveMedia func(*models.ActiveMedia),
) (func() error, error) {
	// Initialize clock if not set (for tests that skip StartPre)
	if p.clock == nil {
		p.clock = clockwork.NewRealClock()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Poll every 2 seconds for responsive tracking
	ticker := p.clock.NewTicker(2 * time.Second)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.Chan():
				p.checkAndUpdateRunningGame(setActiveMedia)
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() error {
		cancel()
		return nil
	}, nil
}

// checkKodiPlaybackStatus checks if Kodi is running and what's playing.
// Returns the currently playing item as ActiveMedia, or nil if nothing is playing or Kodi is not running.
// Updates kodiActive flag based on Kodi availability.
func (p *Platform) checkKodiPlaybackStatus() *models.ActiveMedia {
	client := kodi.NewClient(p.cfg)

	// Try to get active players with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	players, err := client.GetActivePlayers(ctx)
	if err != nil {
		// Kodi is not running or not reachable
		log.Trace().Err(err).Msg("Kodi not reachable, clearing kodiActive flag")
		p.trackerMu.Lock()
		p.kodiActive = false
		p.trackerMu.Unlock()
		return nil
	}

	// Kodi is running - set the flag (may have been launched externally)
	p.trackerMu.Lock()
	wasActive := p.kodiActive
	p.kodiActive = true
	p.trackerMu.Unlock()

	if !wasActive {
		log.Debug().Msg("Kodi detected running, setting kodiActive=true")
	}

	// No active players means nothing is playing
	if len(players) == 0 {
		log.Debug().Msg("Kodi is running but nothing is playing")
		return nil
	}

	// Get the first active player's item
	player := players[0]
	item, err := client.GetPlayerItem(ctx, player.ID)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get Kodi player item")
		return nil
	}

	// Format the title to match how we index media
	title := item.Title
	if title == "" {
		title = item.Label
	}

	// Format based on media type
	switch item.Type {
	case "episode":
		// Format: "Show Name - S01E05 - Episode Title"
		if item.ShowTitle != "" {
			formatted := item.ShowTitle
			if item.Season > 0 || item.Episode > 0 {
				formatted += " - "
				if item.Season > 0 {
					formatted += "S" + helpers.PadNumber(item.Season, 2)
				}
				if item.Episode > 0 {
					formatted += "E" + helpers.PadNumber(item.Episode, 2)
				}
			}
			if title != "" {
				formatted += " - " + title
			}
			title = formatted
		}

	case "song":
		// Format: "Artist - Album - Song Title" (omit parts if missing)
		if len(item.Artist) > 0 {
			parts := []string{strings.Join(item.Artist, ", ")}
			if item.Album != "" {
				parts = append(parts, item.Album)
			}
			if title != "" {
				parts = append(parts, title)
			}
			title = strings.Join(parts, " - ")
		}

	case "movie":
		// Format: "Movie Title (Year)" if year available
		if item.Year > 0 && title != "" {
			title = title + " (" + helpers.PadNumber(item.Year, 4) + ")"
		}
	}

	if title == "" {
		title = item.Label
	}

	// Determine system ID based on item type (movie, episode, song, etc.)
	var systemID string
	switch item.Type {
	case "movie":
		systemID = systemdefs.SystemMovie
	case "episode":
		systemID = systemdefs.SystemTVEpisode
	case "song":
		systemID = systemdefs.SystemMusicTrack
	case "musicvideo":
		systemID = systemdefs.SystemVideo
	case "channel":
		systemID = systemdefs.SystemVideo
	default:
		// Fallback: use player type to determine category
		if player.Type == "audio" {
			systemID = systemdefs.SystemMusicTrack
		} else {
			systemID = systemdefs.SystemVideo
		}
		log.Debug().Msgf("unknown Kodi item type '%s', using fallback system '%s'", item.Type, systemID)
	}

	// Get system metadata for proper name
	systemMeta, err := assets.GetSystemMetadata(systemID)
	if err != nil {
		log.Warn().Err(err).Msgf("failed to get system metadata for %s", systemID)
		// Use systemID as fallback name
		return models.NewActiveMedia(systemID, systemID, item.File, title, "")
	}

	// Create active media for the playing item
	return models.NewActiveMedia(
		systemID,
		systemMeta.Name,
		item.File,
		title,
		"", // LauncherID unknown for background-detected Kodi playback
	)
}

// checkAndUpdateRunningGame polls the ES API and updates active media state if changed.
// Handles both game detection and Kodi playback monitoring.
func (p *Platform) checkAndUpdateRunningGame(
	setActiveMedia func(*models.ActiveMedia),
) {
	p.trackerMu.Lock()
	kodiActive := p.kodiActive
	lastKnown := p.lastKnownGame
	p.trackerMu.Unlock()

	// If Kodi is supposed to be active, check its playback status
	if kodiActive {
		kodiMedia := p.checkKodiPlaybackStatus()

		// Check if kodiActive was cleared by checkKodiPlaybackStatus (Kodi not running)
		p.trackerMu.RLock()
		kodiActive = p.kodiActive
		p.trackerMu.RUnlock()

		if !kodiActive {
			// Kodi closed externally
			log.Info().Msg("detected Kodi closed externally")
			p.trackerMu.Lock()
			p.lastKnownGame = nil
			p.trackerMu.Unlock()
			setActiveMedia(nil)
			return
		}

		// Kodi is running - check if playback state changed
		if kodiMedia == nil && lastKnown != nil {
			// Playback stopped
			log.Info().Msg("detected Kodi playback stopped")
			p.trackerMu.Lock()
			p.lastKnownGame = nil
			p.trackerMu.Unlock()
			setActiveMedia(nil)
			return
		}

		if kodiMedia != nil {
			// Check if this is a new/different item
			if lastKnown == nil || lastKnown.Path != kodiMedia.Path {
				if lastKnown == nil {
					log.Info().Msgf("detected Kodi playback started: %s", kodiMedia.Name)
				} else {
					log.Info().Msgf("detected Kodi playback change: %s -> %s", lastKnown.Name, kodiMedia.Name)
				}
				p.trackerMu.Lock()
				p.lastKnownGame = kodiMedia
				p.trackerMu.Unlock()
				setActiveMedia(kodiMedia)
			}
		}

		// Don't check ES API if Kodi is active
		return
	}

	// Kodi is not active - check EmulationStation API
	gameResp, isRunning, err := esapi.APIRunningGame()
	if err != nil {
		log.Debug().Err(err).Msg("failed to check running game in background tracker")
		return
	}

	// Handle no game running
	if !isRunning {
		// Before clearing, check if Kodi might be running (ES API doesn't report Kodi)
		// This detects externally launched Kodi (not launched via Zaparoo)
		kodiMedia := p.checkKodiPlaybackStatus()

		// Check if we discovered Kodi
		p.trackerMu.RLock()
		kodiActive = p.kodiActive
		p.trackerMu.RUnlock()

		if kodiActive {
			// Detected externally launched Kodi!
			if kodiMedia != nil {
				log.Info().Msgf("detected externally launched Kodi with playback: %s", kodiMedia.Name)
				p.trackerMu.Lock()
				p.lastKnownGame = kodiMedia
				p.trackerMu.Unlock()
				setActiveMedia(kodiMedia)
			} else {
				log.Info().Msg("detected externally launched Kodi (idle)")
				// Kodi is running but nothing playing - still set it as active so stop works correctly
				if lastKnown != nil {
					p.trackerMu.Lock()
					p.lastKnownGame = nil
					p.trackerMu.Unlock()
					setActiveMedia(nil)
				}
			}
			return
		}

		// No game and no Kodi - clear if we had something before
		if lastKnown != nil {
			log.Info().Msg("detected game closed externally")
			p.trackerMu.Lock()
			p.lastKnownGame = nil
			p.trackerMu.Unlock()
			setActiveMedia(nil)
		}
		return
	}

	// Game is running - convert to ActiveMedia
	systemID, err := fromBatoceraSystem(gameResp.SystemName)
	if err != nil {
		log.Warn().Err(err).Msgf("failed to convert system %s in background tracker", gameResp.SystemName)
		return
	}

	systemMeta, err := assets.GetSystemMetadata(systemID)
	if err != nil {
		log.Warn().Err(err).Msgf("failed to get system metadata for %s in background tracker", systemID)
		return
	}

	newGame := models.NewActiveMedia(
		systemID,
		systemMeta.Name,
		gameResp.Path,
		gameResp.Name,
		"", // LauncherID unknown for externally launched games
	)

	// Check if this is actually a new game (compare by path)
	if lastKnown != nil && lastKnown.Path == newGame.Path {
		// Same game still running, no update needed
		return
	}

	// New game detected or game changed
	if lastKnown == nil {
		log.Info().Msgf("detected game launched externally: %s", newGame.Name)
	} else {
		log.Info().Msgf("detected game change: %s -> %s", lastKnown.Name, newGame.Name)
	}

	p.trackerMu.Lock()
	p.lastKnownGame = newGame
	p.trackerMu.Unlock()

	setActiveMedia(newGame)
}
