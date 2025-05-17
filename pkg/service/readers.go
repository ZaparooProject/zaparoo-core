package service

import (
	"strings"
	"time"

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

func shouldExit(
	cfg *config.Instance,
	pl platforms.Platform,
	st *state.State,
) bool {
	if !cfg.HoldModeEnabled() {
		return false
	}

	// do not exit from menu, there is nowhere to go anyway
	if st.ActiveMedia().SystemID == "" {
		return false
	}

	if st.GetLastScanned().FromAPI {
		return false
	}

	if inExitGameBlocklist(cfg, st) {
		return false
	}

	return true
}

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

			activeLauncher := st.ActiveMedia().LauncherID
			softToken := st.GetSoftwareToken()
			if activeLauncher == "" || softToken == nil {
				log.Debug().Msg("no active launcher, not exiting")
				return
			}

			// run before_exit hook if one exists for system
			var systemIds []string
			for _, l := range pl.Launchers(cfg) {
				if l.ID == activeLauncher {
					systemIds = append(systemIds, l.SystemID)
					system, err := systemdefs.LookupSystem(l.SystemID)
					if err == nil {
						systemIds = append(systemIds, system.Aliases...)
					}
					break
				}
			}
			if len(systemIds) > 0 {
				for _, systemId := range systemIds {
					defaults, ok := cfg.LookupSystemDefaults(systemId)
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
						break
					}
				}
			}

			// exit the media
			log.Info().Msg("exiting media")
			err := pl.StopActiveLauncher()
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
	isStopped := false
	for !isStopped {
		var scan *tokens.Token

		select {
		case <-st.GetContext().Done():
			log.Debug().Msg("Closing Readers via context cancellation")
			isStopped = true
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
