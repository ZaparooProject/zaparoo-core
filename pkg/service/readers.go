package service

import (
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

func shouldExit(
	cfg *config.Instance,
	pl platforms.Platform,
	st *state.State,
) bool {
	if !cfg.HoldModeEnabled() {
		return false
	}

	// do not exit from menu, there is nowhere to go anyway
	if pl.GetActiveLauncher() == "" {
		return false
	}

	if st.GetLastScanned().Remote {
		return false
	}

	if inExitGameBlocklist(pl, cfg) {
		return false
	}

	return true
}

func connectReaders(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	rs := st.ListReaders()
	var toConnect []string

	// TODO: this needs to gather the final list of reader paths, resolve any
	// symlinks, remove duplicates, and then connect to them

	for _, device := range cfg.Readers().Connect {
		connStr := device.Driver + ":" + device.Path
		if !utils.Contains(rs, connStr) && !utils.Contains(toConnect, connStr) {
			log.Debug().Msgf("config device not connected, adding: %s", device)
			toConnect = append(toConnect, connStr)
		}
	}

	// user defined readers
	for _, device := range toConnect {
		if _, ok := st.GetReader(device); !ok {
			ps := strings.SplitN(device, ":", 2)
			if len(ps) != 2 {
				return errors.New("invalid device string")
			}

			rt := ps[0]

			for _, r := range pl.SupportedReaders(cfg) {
				ids := r.Ids()
				if utils.Contains(ids, rt) {
					log.Debug().Msgf("connecting to reader: %s", device)
					err := r.Open(device, iq)
					if err != nil {
						log.Error().Msgf("error opening reader: %s", err)
					} else {
						st.SetReader(device, r)
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
				err := r.Open(detect, iq)
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
	err := pl.ReadersUpdateHook(rsm)
	if err != nil {
		return err
	}

	return nil
}

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
	stopService := make(chan bool)

	playFail := func() {
		if time.Since(lastError) > 1*time.Second {
			pl.PlayFailSound(cfg)
		}
	}

	startTimedExit := func() {
		// TODO: this should be moved to processTokenQueue

		if exitTimer != nil {
			stopped := exitTimer.Stop()
			if stopped {
				log.Info().Msg("cancelling previous exit timer")
			}
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

			activeLauncher := pl.GetActiveLauncher()
			softToken := st.GetSoftwareToken()
			if activeLauncher == "" || softToken == nil {
				log.Debug().Msg("no active launcher, not exiting")
				return
			}

			// run before_exit hook if one exists for system
			var launcher platforms.Launcher
			found := false
			for _, l := range pl.Launchers() {
				if l.Id == activeLauncher {
					launcher = l
					found = true
					break
				}
			}
			if found {
				defaults, ok := cfg.LookupSystemDefaults(launcher.SystemId)
				if ok && defaults.BeforeExit != "" {
					log.Info().Msgf("running on remove script: %s", defaults.BeforeExit)
					plsc := playlists.PlaylistController{
						Active: st.GetActivePlaylist(),
						Queue:  plq,
					}
					t := tokens.Token{
						ScanTime: time.Now(),
						Text:     defaults.BeforeExit,
					}
					err := launchToken(pl, cfg, t, db, lsq, plsc)
					if err != nil {
						log.Error().Msgf("error launching on remove script: %s", err)
					}
				}
			}

			// exit the media
			log.Info().Msg("exiting media")
			err := pl.KillLauncher()
			if err != nil {
				log.Warn().Msgf("error killing launcher: %s", err)
			}

			lsq <- nil
		}()
	}

	// manage reader connections
	go func() {
		for {
			select {
			case <-stopService:
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
	for !st.ShouldStopService() {
		var scan *tokens.Token

		select {
		case t := <-scanQueue:
			// a reader has sent a token for pre-processing
			log.Debug().Msgf("pre-processing token: %v", t)
			if t.Error != nil {
				log.Error().Msgf("error reading card: %s", err)
				playFail()
				lastError = time.Now()
				continue
			}
			scan = t.Token
		case stoken := <-lsq:
			// a token has been launched that starts software
			log.Debug().Msgf("new software token: %v", st)

			if exitTimer != nil && !utils.TokensEqual(stoken, st.GetSoftwareToken()) {
				if stopped := exitTimer.Stop(); stopped {
					log.Info().Msg("different software token inserted, cancelling exit")
				}
			}

			st.SetSoftwareToken(stoken)
			continue
		}

		if utils.TokensEqual(scan, prevToken) {
			log.Debug().Msg("ignoring duplicate scan")
			continue
		}

		prevToken = scan

		if scan != nil {
			log.Info().Msgf("new token scanned: %v", scan)
			st.SetActiveCard(*scan)

			if !st.RunZapScriptEnabled() {
				log.Debug().Msg("skipping token, run ZapScript disabled")
				continue
			}

			if exitTimer != nil {
				stopped := exitTimer.Stop()
				if stopped && utils.TokensEqual(scan, st.GetSoftwareToken()) {
					log.Info().Msg("same token reinserted, cancelling exit")
					continue
				} else if stopped {
					log.Info().Msg("new token inserted, restarting exit timer")
					startTimedExit()
				}
			}

			wt := st.GetWroteToken()
			if wt != nil && utils.TokensEqual(scan, wt) {
				log.Info().Msg("skipping launching just written token")
				st.SetWroteToken(nil)
				continue
			} else {
				st.SetWroteToken(nil)
			}

			log.Info().Msgf("sending token: %v", scan)
			pl.PlaySuccessSound(cfg)
			itq <- *scan
		} else {
			log.Info().Msg("token was removed")
			st.SetActiveCard(tokens.Token{})
			if shouldExit(cfg, pl, st) {
				startTimedExit()
			}
		}
	}

	// daemon shutdown
	stopService <- true
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
