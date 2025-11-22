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

package service

import (
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func runTokenZapScript(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	token tokens.Token, //nolint:gocritic // single-use parameter in service function
	db *database.Database,
	lsq chan<- *tokens.Token,
	plsc playlists.PlaylistController,
) error {
	if !st.RunZapScriptEnabled() {
		log.Warn().Msg("ignoring ZapScript, run ZapScript is disabled")
		return nil
	}

	mappedValue, hasMapping := getMapping(cfg, db, platform, token)
	if hasMapping {
		log.Info().Msgf("found mapping: %s", mappedValue)
		token.Text = mappedValue
	}

	reader := parser.NewParser(token.Text)
	script, err := reader.ParseScript()
	if err != nil {
		return fmt.Errorf("failed to parse script: %w", err)
	}

	log.Info().Msgf("running script (%d cmds): %s", len(script.Cmds), token.Text)

	pls := plsc.Active

	cmds := script.Cmds
	for i := 0; i < len(cmds); i++ {
		cmd := cmds[i]

		result, err := zapscript.RunCommand(
			platform, cfg,
			playlists.PlaylistController{
				Active: pls,
				Queue:  plsc.Queue,
			},
			token,
			cmd,
			len(script.Cmds),
			i,
			db,
			st,
		)
		if err != nil {
			return fmt.Errorf("failed to run zapscript command: %w", err)
		}

		if result.MediaChanged && !token.FromAPI {
			log.Debug().Any("token", token).Msg("media changed, updating token")
			log.Info().Msgf("current media launched set to: %s", token.UID)
			lsq <- &token
		}

		if result.PlaylistChanged {
			pls = result.Playlist
		}

		if result.Unsafe {
			log.Warn().Msg("token has been flagged as unsafe")
			token.Unsafe = true
		}

		// if a command results in additional commands to run (like from a
		// remote query) inject them to be run immediately after this command
		if len(result.NewCommands) > 0 {
			log.Info().Msgf("injecting %d new commands: %v", len(result.NewCommands), result.NewCommands)
			before := cmds[:i+1]
			after := cmds[i+1:]
			before = append(before, result.NewCommands...)
			cmds = before
			cmds = append(cmds, after...)
		}
	}

	return nil
}

func launchPlaylistMedia(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	lsq chan<- *tokens.Token,
	pls *playlists.Playlist,
	plq chan<- *playlists.Playlist,
	activePlaylist *playlists.Playlist,
) {
	t := tokens.Token{
		Text:     pls.Current().ZapScript,
		ScanTime: time.Now(),
		Source:   tokens.SourcePlaylist,
	}
	plsc := playlists.PlaylistController{
		Active: activePlaylist,
		Queue:  plq,
	}

	err := runTokenZapScript(platform, cfg, st, t, db, lsq, plsc)
	if err != nil {
		log.Error().Err(err).Msgf("error launching token")
	}

	now := time.Now()
	uptime, uptimeErr := helpers.GetSystemUptime()
	if uptimeErr != nil {
		log.Warn().Err(uptimeErr).Msg("failed to get system uptime for history entry, using 0")
		uptime = 0
	}
	monotonicStart := int64(uptime.Seconds())

	he := database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           t.ScanTime,
		Type:           t.Type,
		TokenID:        t.UID,
		TokenValue:     t.Text,
		TokenData:      t.Data,
		ClockReliable:  helpers.IsClockReliable(now),
		BootUUID:       st.BootUUID(),
		MonotonicStart: monotonicStart,
		CreatedAt:      now,
	}
	he.Success = err == nil
	err = db.UserDB.AddHistory(&he)
	if err != nil {
		log.Error().Err(err).Msgf("error adding history")
	}
}

func handlePlaylist(
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	st *state.State,
	pls *playlists.Playlist,
	lsq chan<- *tokens.Token,
	plq chan<- *playlists.Playlist,
) {
	activePlaylist := st.GetActivePlaylist()

	switch {
	case pls == nil:
		// request to clear playlist
		if activePlaylist != nil {
			log.Info().Msg("clearing playlist")
		}
		st.SetActivePlaylist(nil)
		return
	case activePlaylist == nil:
		// new playlist loaded
		st.SetActivePlaylist(pls)
		if pls.Playing {
			log.Info().Any("pls", pls).Msg("setting new playlist, launching token")
			go launchPlaylistMedia(pl, cfg, st, db, lsq, pls, plq, activePlaylist)
		} else {
			log.Info().Any("pls", pls).Msg("setting new playlist")
		}
		return
	default:
		// active playlist updated
		if pls.Current() == activePlaylist.Current() &&
			pls.Playing == activePlaylist.Playing {
			log.Debug().Msg("playlist current token unchanged, skipping")
			return
		}

		st.SetActivePlaylist(pls)
		if pls.Playing {
			log.Info().Any("pls", pls).Msg("updating playlist, launching token")
			go launchPlaylistMedia(pl, cfg, st, db, lsq, pls, plq, activePlaylist)
		} else {
			log.Info().Any("pls", pls).Msg("updating playlist")
		}
		return
	}
}

func processTokenQueue(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	itq <-chan tokens.Token,
	db *database.Database,
	lsq chan<- *tokens.Token,
	plq chan *playlists.Playlist,
	limitsManager *playtime.LimitsManager,
) {
	for {
		select {
		case pls := <-plq:
			handlePlaylist(cfg, platform, db, st, pls, lsq, plq)
			continue
		case t := <-itq:
			// TODO: change this channel to send a token pointer or something
			if t.ScanTime.IsZero() {
				// ignore empty tokens
				continue
			}

			log.Info().Msgf("processing token: %v", t)

			// Play success sound immediately on scan success
			if path, enabled := cfg.SuccessSoundPath(helpers.DataDir(platform)); enabled {
				if path == "" {
					// Use embedded default sound
					if audioErr := audio.PlayWAVBytes(assets.SuccessSound); audioErr != nil {
						log.Warn().Msgf("error playing success sound: %s", audioErr)
					}
				} else {
					// Use custom sound file
					if audioErr := audio.PlayFile(path); audioErr != nil {
						log.Warn().Str("path", path).Msgf("error playing custom success sound: %s", audioErr)
					}
				}
			}

			err := platform.ScanHook(&t)
			if err != nil {
				log.Error().Err(err).Msgf("error writing tmp scan result")
			}

			now := time.Now()
			uptime, uptimeErr := helpers.GetSystemUptime()
			if uptimeErr != nil {
				log.Warn().Err(uptimeErr).Msg("failed to get system uptime for history entry, using 0")
				uptime = 0
			}
			monotonicStart := int64(uptime.Seconds())

			he := database.HistoryEntry{
				ID:             uuid.New().String(),
				Time:           t.ScanTime,
				Type:           t.Type,
				TokenID:        t.UID,
				TokenValue:     t.Text,
				TokenData:      t.Data,
				ClockReliable:  helpers.IsClockReliable(now),
				BootUUID:       st.BootUUID(),
				MonotonicStart: monotonicStart,
				CreatedAt:      now,
			}

			// Check playtime limits before launching
			if limitErr := limitsManager.CheckBeforeLaunch(); limitErr != nil {
				log.Warn().Err(limitErr).Msg("playtime: launch blocked by daily limit")

				// Send playtime limit notification
				notifications.PlaytimeLimitReached(st.Notifications, models.PlaytimeLimitReachedParams{
					Reason: "daily",
				})

				// Play limit sound
				if path, enabled := cfg.LimitSoundPath(helpers.DataDir(platform)); enabled {
					if path == "" {
						// Use embedded default sound
						if audioErr := audio.PlayWAVBytes(assets.LimitSound); audioErr != nil {
							log.Warn().Msgf("error playing limit sound: %s", audioErr)
						}
					} else {
						// Use custom sound file
						if audioErr := audio.PlayFile(path); audioErr != nil {
							log.Warn().Str("path", path).Msgf("error playing custom limit sound: %s", audioErr)
						}
					}
				}

				// Add to history as failed
				he.Success = false
				if histErr := db.UserDB.AddHistory(&he); histErr != nil {
					log.Error().Err(histErr).Msgf("error adding history")
				}

				// Skip launch
				continue
			}

			// launch tokens in a separate thread
			go func() {
				plsc := playlists.PlaylistController{
					Active: st.GetActivePlaylist(),
					Queue:  plq,
				}

				err = runTokenZapScript(platform, cfg, st, t, db, lsq, plsc)
				if err != nil {
					log.Error().Err(err).Msgf("error launching token")
				}

				// Play fail sound only if ZapScript fails
				if err != nil {
					if path, enabled := cfg.FailSoundPath(helpers.DataDir(platform)); enabled {
						if path == "" {
							// Use embedded default sound
							if audioErr := audio.PlayWAVBytes(assets.FailSound); audioErr != nil {
								log.Warn().Msgf("error playing fail sound: %s", audioErr)
							}
						} else {
							// Use custom sound file
							if audioErr := audio.PlayFile(path); audioErr != nil {
								log.Warn().Str("path", path).Msgf("error playing custom fail sound: %s", audioErr)
							}
						}
					}
				}

				he.Success = err == nil
				err = db.UserDB.AddHistory(&he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
			}()
		case <-st.GetContext().Done():
			log.Debug().Msg("exiting service worker via context cancellation")
			return
		}
	}
}
