/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023, 2024 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"golang.org/x/exp/slices"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

func inExitGameBlocklist(platform platforms.Platform, cfg *config.Instance) bool {
	var blocklist []string
	for _, v := range cfg.ReadersScan().IgnoreSystem {
		blocklist = append(blocklist, strings.ToLower(v))
	}
	return slices.Contains(blocklist, strings.ToLower(platform.GetActiveLauncher()))
}

func launchToken(
	platform platforms.Platform,
	cfg *config.Instance,
	token tokens.Token,
	db *database.Database,
	lsq chan<- *tokens.Token,
	plsc playlists.PlaylistController,
) error {
	text := token.Text

	mappingText, mapped := getMapping(cfg, db, platform, token)
	if mapped {
		log.Info().Msgf("found mapping: %s", mappingText)
		text = mappingText
	}

	if text == "" {
		return fmt.Errorf("no ZapScript in token")
	}

	log.Info().Msgf("launching ZapScript: %s", text)
	cmds := strings.Split(text, "||")

	for i, cmd := range cmds {
		err, softwareSwap := zapscript.LaunchToken(
			platform,
			cfg,
			plsc,
			token,
			cmd,
			len(cmds),
			i,
		)
		if err != nil {
			return err
		}

		if softwareSwap && !token.FromAPI {
			log.Info().Msgf("current software launched set to: %s", token.UID)
			lsq <- &token
		}
	}

	return nil
}

func processTokenQueue(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	itq <-chan tokens.Token,
	db *database.Database,
	lsq chan<- *tokens.Token,
	plq chan *playlists.Playlist,
) {
	for {
		select {
		case pls := <-plq:
			activePlaylist := st.GetActivePlaylist()

			if pls == nil {
				if activePlaylist != nil {
					log.Info().Msg("clearing active playlist")
				}
				activePlaylist = nil
				continue
			} else if activePlaylist == nil {
				log.Info().Msg("setting new active playlist, launching token")
				activePlaylist = pls
				go func() {
					t := tokens.Token{
						Text:     pls.Current(),
						ScanTime: time.Now(),
						Source:   tokens.SourcePlaylist,
					}
					plsc := playlists.PlaylistController{
						Active: activePlaylist,
						Queue:  plq,
					}
					err := launchToken(platform, cfg, t, db, lsq, plsc)
					if err != nil {
						log.Error().Err(err).Msgf("error launching token")
					}
				}()
				continue
			} else {
				if pls.Current() == activePlaylist.Current() {
					log.Debug().Msg("playlist current token unchanged, skipping")
					continue
				}

				log.Info().Msg("updating active playlist, launching token")
				activePlaylist = pls
				go func() {
					t := tokens.Token{
						Text:     pls.Current(),
						ScanTime: time.Now(),
						Source:   tokens.SourcePlaylist,
					}
					plsc := playlists.PlaylistController{
						Active: activePlaylist,
						Queue:  plq,
					}
					err := launchToken(platform, cfg, t, db, lsq, plsc)
					if err != nil {
						log.Error().Err(err).Msgf("error launching token")
					}
				}()
				continue
			}
		case t := <-itq:
			// TODO: change this channel to send a token pointer or something
			if t.ScanTime.IsZero() {
				// ignore empty tokens
				continue
			}

			log.Info().Msgf("processing token: %v", t)

			err := platform.AfterScanHook(t)
			if err != nil {
				log.Error().Err(err).Msgf("error writing tmp scan result")
			}

			he := database.HistoryEntry{
				Time: t.ScanTime,
				Type: t.Type,
				UID:  t.UID,
				Text: t.Text,
				Data: t.Data,
			}

			if !st.RunZapScriptEnabled() {
				log.Debug().Msg("ZapScript disabled, skipping run")
				err = db.AddHistory(he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
				continue
			}

			// launch tokens in separate thread
			go func() {
				plsc := playlists.PlaylistController{
					Active: st.GetActivePlaylist(),
					Queue:  plq,
				}

				err = launchToken(platform, cfg, t, db, lsq, plsc)
				if err != nil {
					log.Error().Err(err).Msgf("error launching token")
				}

				he.Success = err == nil
				err = db.AddHistory(he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
			}()
		case <-st.GetContext().Done():
			log.Debug().Msg("Exiting Service worker via context cancellation")
			break
		}
	}
}

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (func() error, error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl)
	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)
	lsq := make(chan *tokens.Token)
	plq := make(chan *playlists.Playlist)

	log.Info().Msg("running platform pre start")
	err := pl.StartPre(cfg)
	if err != nil {
		log.Error().Err(err).Msg("platform start pre error")
		return nil, err
	}

	dirs := []string{
		pl.DataDir(),
		pl.TempDir(),
		filepath.Join(pl.DataDir(), platforms.MappingsDir),
	}
	for _, dir := range dirs {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, err
		}
	}

	log.Info().Msg("opening user database")
	db, err := database.Open(pl)
	if err != nil {
		log.Error().Err(err).Msgf("error opening user database")
		return nil, err
	}

	log.Info().Msg("loading mapping files")
	err = cfg.LoadMappings(filepath.Join(pl.DataDir(), platforms.MappingsDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading mapping files")
		return nil, err
	}

	log.Info().Msg("starting API service")
	go api.Start(pl, cfg, st, itq, db, ns)

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		go groovyproxy.Start(cfg, st, itq)
	}

	log.Info().Msg("starting reader manager")
	go readerManager(pl, cfg, st, db, itq, lsq, plq)

	log.Info().Msg("starting input token queue manager")
	go processTokenQueue(pl, cfg, st, itq, db, lsq, plq)

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.Notifications)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		return nil, err
	}

	return func() error {
		err = pl.Stop()
		if err != nil {
			log.Warn().Msgf("error stopping platform: %s", err)
		}
		st.StopService()
		close(plq)
		close(lsq)
		close(itq)
		return nil
	}, nil
}
