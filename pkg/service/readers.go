package service

import (
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

type toConnectDevice struct {
	connectionString string
	device           config.ReadersConnect
}

func connectReaders(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	rs := st.ListReaders()
	var toConnect []toConnectDevice
	toConnectStrs := func() []string {
		var tc []string
		for _, device := range toConnect {
			tc = append(tc, device.connectionString)
		}
		return tc
	}

	for _, device := range cfg.Readers().Connect {
		if !utils.Contains(rs, device.ConnectionString()) &&
			!utils.Contains(toConnectStrs(), device.ConnectionString()) {
			log.Debug().Msgf("config device not connected, adding: %s", device)
			toConnect = append(toConnect, toConnectDevice{
				connectionString: device.ConnectionString(),
				device:           device,
			})
		}
	}

	// user defined readers
	for _, device := range toConnect {
		if _, ok := st.GetReader(device.connectionString); !ok {
			rt := device.device.Driver
			for _, r := range pl.SupportedReaders(cfg) {
				ids := r.Ids()
				if utils.Contains(ids, rt) {
					log.Debug().Msgf("connecting to reader: %s", device)
					err := r.Open(device.device, iq)
					if err != nil {
						log.Error().Msgf("error opening reader: %s", err)
					} else {
						st.SetReader(device.connectionString, r)
						log.Info().Msgf("opened reader: %s", device)
						break
					}
				}
			}
		}
	}

	// auto-detect readers
	if cfg.AutoDetect() {
		for _, r := range pl.SupportedReaders(cfg) {
			detect := r.Detect(st.ListReaders())
			if detect != "" {
				ps := strings.SplitN(detect, ":", 2)
				if len(ps) != 2 {
					log.Error().Msgf("invalid auto-detect string: %s", detect)
					continue
				}

				device := config.ReadersConnect{
					Driver: ps[0],
					Path:   ps[1],
				}

				err := r.Open(device, iq)
				if err != nil {
					log.Error().Msgf("error opening detected reader %s: %s", detect, err)
				}
			}

			if r.Connected() {
				st.SetReader(detect, r)
			} else {
				err := r.Close()
				if err != nil {
					log.Debug().Msg("error closing reader")
				}
			}
		}
	}

	// list readers for update hook
	ids := st.ListReaders()
	rsm := make(map[string]*readers.Reader)
	for _, id := range ids {
		r, ok := st.GetReader(id)
		if ok && r != nil {
			rsm[id] = &r
		}
	}

	return nil
}

func runBeforeExitHook(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
	activeMedia models.ActiveMedia,
) {
	var systemIDs []string
	for _, l := range pl.Launchers(cfg) {
		if l.ID == activeMedia.SystemID {
			systemIDs = append(systemIDs, l.SystemID)
			system, err := systemdefs.LookupSystem(l.SystemID)
			if err == nil {
				systemIDs = append(systemIDs, system.Aliases...)
			}
			break
		}
	}

	if len(systemIDs) > 0 {
		for _, systemID := range systemIDs {
			defaults, ok := cfg.LookupSystemDefaults(systemID)
			if !ok || defaults.BeforeExit == "" {
				continue
			}

			log.Info().Msgf("running before_exit script: %s", defaults.BeforeExit)
			plsc := playlists.PlaylistController{
				Active: st.GetActivePlaylist(),
				Queue:  plq,
			}
			t := tokens.Token{
				ScanTime: time.Now(),
				Text:     defaults.BeforeExit,
			}
			err := runToken(pl, cfg, t, db, lsq, plsc)
			if err != nil {
				log.Error().Msgf("error running before_exit script: %s", err)
			}

			break
		}
	}
}

func timedExit(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
	exitTimer *time.Timer,
) *time.Timer {
	if exitTimer != nil {
		stopped := exitTimer.Stop()
		if stopped {
			log.Debug().Msg("cancelling previous exit timer")
		}
	}

	if !cfg.HoldModeEnabled() || st.GetLastScanned().FromAPI {
		return exitTimer
	}

	timerLen := time.Second * time.Duration(cfg.ReadersScan().ExitDelay)
	log.Debug().Msgf("exit timer set to: %s seconds", timerLen)
	exitTimer = time.NewTimer(timerLen)

	go func() {
		<-exitTimer.C

		if !cfg.HoldModeEnabled() {
			log.Debug().Msg("exit timer expired, but hold mode disabled")
			return
		}

		// on_remove hook script runs even if no media active
		onRemoveScript := cfg.ReadersScan().OnRemove
		if onRemoveScript != "" {
			log.Info().Msgf("running on_remove script: %s", onRemoveScript)
			plsc := playlists.PlaylistController{
				Active: st.GetActivePlaylist(),
				Queue:  plq,
			}
			t := tokens.Token{
				ScanTime: time.Now(),
				Text:     onRemoveScript,
			}
			err := runToken(pl, cfg, t, db, lsq, plsc)
			if err != nil {
				log.Error().Msgf("error running on_remove script: %s", err)
			}
		}

		activeMedia := st.ActiveMedia()
		if activeMedia == nil {
			log.Debug().Msg("no active media, cancelling exit")
			return
		}

		if st.GetSoftwareToken() == nil {
			log.Debug().Msg("no active software token, cancelling exit")
			return
		}

		if cfg.IsHoldModeIgnoredSystem(activeMedia.SystemID) {
			log.Debug().Msg("active system ignored in config, cancelling exit")
			return
		}

		runBeforeExitHook(pl, cfg, st, db, lsq, plq, *activeMedia)

		log.Info().Msg("exiting media")
		err := pl.StopActiveLauncher()
		if err != nil {
			log.Warn().Msgf("error killing launcher: %s", err)
		}

		lsq <- nil
	}()

	return exitTimer
}

// readerManager is the main service loop to manage active reader hardware
// connections and dispatch token scans from those readers to the token
// input queue.
//
// When a user scans or removes a token with a reader, the reader instance
// forwards it to the "scan queue" which is consumed by this manager.
// The manager will then, if necessary, dispatch the token object to the
// "token input queue" where it may be run.
// This manager also handles the logic of what to do when a token is removed
// from the reader.
func readerManager(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	itq chan<- tokens.Token,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
) {
	scanQueue := make(chan readers.Scan)

	var err error
	var lastError time.Time

	var prevToken *tokens.Token
	var exitTimer *time.Timer

	readerTicker := time.NewTicker(1 * time.Second)

	playFail := func() {
		if !cfg.AudioFeedback() {
			return
		}
		if time.Since(lastError) > 1*time.Second {
			err := pl.PlayAudio(config.FailSoundFilename)
			if err != nil {
				log.Warn().Msgf("error playing fail sound: %s", err)
			}
		}
	}

	// manage reader connections
	go func() {
		for {
			select {
			case <-st.GetContext().Done():
				return
			case <-readerTicker.C:
				rs := st.ListReaders()
				for _, device := range rs {
					r, ok := st.GetReader(device)
					if ok && r != nil && !r.Connected() {
						log.Debug().Msgf("pruning disconnected reader: %s", device)
						st.RemoveReader(device)
					}
				}

				err := connectReaders(pl, cfg, st, scanQueue)
				if err != nil {
					log.Error().Msgf("error connecting rs: %s", err)
				}
			}
		}
	}()

	// token pre-processing loop
preprocessing:
	for {
		var scan *tokens.Token

		select {
		case <-st.GetContext().Done():
			log.Debug().Msg("closing reader manager via context cancellation")
			break preprocessing
		case t := <-scanQueue:
			// a reader has sent a token for pre-processing
			log.Debug().Msgf("pre-processing token: %v", t)
			if t.Error != nil {
				log.Error().Msgf("error reading card: %s", err)
				playFail()
				lastError = time.Now()
				continue preprocessing
			}
			scan = t.Token
		case stoken := <-lsq:
			// a token has been launched that starts software, used for managing exits
			log.Debug().Msgf("new software token: %v", st)
			if exitTimer != nil && !utils.TokensEqual(stoken, st.GetSoftwareToken()) {
				if stopped := exitTimer.Stop(); stopped {
					log.Info().Msg("different software token inserted, cancelling exit")
				}
			}
			st.SetSoftwareToken(stoken)
			continue preprocessing
		}

		if utils.TokensEqual(scan, prevToken) {
			log.Debug().Msg("ignoring duplicate scan")
			continue preprocessing
		}

		prevToken = scan

		if scan != nil {
			log.Info().Msgf("new token scanned: %v", scan)
			st.SetActiveCard(*scan)

			if !st.RunZapScriptEnabled() {
				log.Debug().Msg("skipping token, run ZapScript disabled")
				continue preprocessing
			}

			if exitTimer != nil {
				stopped := exitTimer.Stop()
				activeToken := st.GetActiveCard()
				if stopped && utils.TokensEqual(scan, &activeToken) {
					log.Info().Msg("same token reinserted, cancelling exit")
					continue preprocessing
				} else if stopped {
					log.Info().Msg("new token inserted, restarting exit timer")
					exitTimer = timedExit(pl, cfg, st, db, lsq, plq, exitTimer)
				}
			}

			// avoid launching a token that was just written by a reader
			wt := st.GetWroteToken()
			if wt != nil && utils.TokensEqual(scan, wt) {
				log.Info().Msg("skipping launching just written token")
				st.SetWroteToken(nil)
				continue preprocessing
			} else {
				st.SetWroteToken(nil)
			}

			log.Info().Msgf("sending token to queue: %v", scan)

			if cfg.AudioFeedback() {
				err := pl.PlayAudio(config.SuccessSoundFilename)
				if err != nil {
					log.Warn().Msgf("error playing success sound: %s", err)
				}
			}

			itq <- *scan
		} else {
			log.Info().Msg("token was removed")
			st.SetActiveCard(tokens.Token{})
			exitTimer = timedExit(pl, cfg, st, db, lsq, plq, exitTimer)
		}
	}

	// daemon shutdown
	rs := st.ListReaders()
	for _, device := range rs {
		r, ok := st.GetReader(device)
		if ok && r != nil {
			err := r.Close()
			if err != nil {
				log.Warn().Msg("error closing reader")
			}
		}
	}
}
