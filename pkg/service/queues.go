package service

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

func runTokenZapScript(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	token tokens.Token,
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
		return err
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
			cmds = append(before, append(result.NewCommands, after...)...)
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
			go launchPlaylistMedia(pl, cfg, st, db, lsq, pls, plq, activePlaylist)
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

				he.Success = err == nil
				err = db.UserDB.AddHistory(he)
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
