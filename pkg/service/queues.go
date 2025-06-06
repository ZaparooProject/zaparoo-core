package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

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

	pls := plsc.Active

	for i, cmd := range cmds {
		result, err := zapscript.LaunchToken(
			platform,
			cfg,
			playlists.PlaylistController{
				Active: pls,
				Queue:  plsc.Queue,
			},
			token,
			cmd,
			len(cmds),
			i,
			db,
		)
		if err != nil {
			return err
		}

		if result.MediaChanged && !token.FromAPI {
			log.Debug().Any("token", token).Msg("media changed, updating token")
			log.Info().Msgf("current media launched set to: %s", token.UID)
			lsq <- &token
		}

		if result.PlaylistChanged {
			pls = result.Playlist
		}
	}

	return nil
}

func launchPlaylistMedia(
	platform platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	lsq chan<- *tokens.Token,
	pls *playlists.Playlist,
	plq chan<- *playlists.Playlist,
	activePlaylist *playlists.Playlist,
) {
	t := tokens.Token{
		Text:     pls.Current().Path,
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

	he := database.HistoryEntry{
		Time:       t.ScanTime,
		Type:       t.Type,
		TokenID:    t.UID,
		TokenValue: t.Text,
		TokenData:  t.Data,
	}
	he.Success = err == nil
	err = db.UserDB.AddHistory(he)
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

	if pls == nil {
		// request to clear playlist
		if activePlaylist != nil {
			log.Info().Msg("clearing playlist")
		}
		st.SetActivePlaylist(nil)
		return
	} else if activePlaylist == nil {
		// new playlist loaded
		st.SetActivePlaylist(pls)
		if pls.Playing {
			log.Info().Any("pls", pls).Msg("setting new playlist, launching token")
			go launchPlaylistMedia(pl, cfg, db, lsq, pls, plq, activePlaylist)
		} else {
			log.Info().Any("pls", pls).Msg("setting new playlist")
		}
		return
	} else {
		// active playlist updated
		if pls.Current() == activePlaylist.Current() &&
			pls.Playing == activePlaylist.Playing {
			log.Debug().Msg("playlist current token unchanged, skipping")
			return
		}

		st.SetActivePlaylist(pls)
		if pls.Playing {
			log.Info().Any("pls", pls).Msg("updating playlist, launching token")
			go launchPlaylistMedia(pl, cfg, db, lsq, pls, plq, activePlaylist)
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

			err := platform.ScanHook(t)
			if err != nil {
				log.Error().Err(err).Msgf("error writing tmp scan result")
			}

			he := database.HistoryEntry{
				Time:       t.ScanTime,
				Type:       t.Type,
				TokenID:    t.UID,
				TokenValue: t.Text,
				TokenData:  t.Data,
			}

			if !st.RunZapScriptEnabled() {
				log.Debug().Msg("ZapScript disabled, skipping run")
				err = db.UserDB.AddHistory(he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
				continue
			}

			// launch tokens in a separate thread
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
				err = db.UserDB.AddHistory(he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
			}()
		case <-st.GetContext().Done():
			log.Debug().Msg("exiting service worker via context cancellation")
			break
		}
	}
}
