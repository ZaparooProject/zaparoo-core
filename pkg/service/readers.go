package service

import (
	"errors"
	"strings"
	"time"
    	"os/exec"

	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/tapto/pkg/config"
	"github.com/wizzomafizzo/tapto/pkg/platforms"
	"github.com/wizzomafizzo/tapto/pkg/readers"
	"github.com/wizzomafizzo/tapto/pkg/service/state"
	"github.com/wizzomafizzo/tapto/pkg/tokens"
	"github.com/wizzomafizzo/tapto/pkg/utils"
)

func shouldExit(
	cfg *config.UserConfig,
	pl platforms.Platform,
	st *state.State,
) bool {
	if !cfg.GetExitGame() {
		return false
	}

	// do not exit from menu, there is nowhere to go anyway
	if pl.GetActiveLauncher() == "" {
		return false
	}

	if st.GetLastScanned().Remote || st.IsLauncherDisabled() {
		return false
	}

	if inExitGameBlocklist(pl, cfg) {
		return false
	}

	return true
}

func connectReaders(
	pl platforms.Platform,
	cfg *config.UserConfig,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	rs := st.ListReaders()
	var toConnect []string

	// TODO: this needs to gather the final list of reader paths, resolve any
	// symlinks, remove duplicates, and then connect to them

	userDevice := cfg.GetConnectionString()
	if userDevice != "" && !utils.Contains(rs, userDevice) {
		log.Debug().Msgf("user device not connected, adding: %s", userDevice)
		toConnect = append(toConnect, userDevice)
	}

	for _, device := range cfg.GetReader() {
		if !utils.Contains(rs, device) && !utils.Contains(toConnect, device) {
			log.Debug().Msgf("config device not connected, adding: %s", device)
			toConnect = append(toConnect, device)
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
			_ = r.Close()
		}
	}

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

func stopMPlayer() {
    psCmd := exec.Command("sh", "-c", "ps aux | grep mplayer | grep -v grep")
    output, err := psCmd.Output()
    if err != nil {
        log.Info().Msgf("mplayer processes not detected.")
        return
    }

    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        if line == "" {
            continue
        }

        log.Debug().Msgf("Processing line: %s", line)

        fields := strings.Fields(line)
        if len(fields) < 2 {
            log.Warn().Msgf("Unexpected line format: %s", line)
            continue
        }

        pid := fields[0]
        log.Info().Msgf("Killing mplayer process with PID: %s", pid)

        killCmd := exec.Command("kill", "-9", pid)
        if err := killCmd.Run(); err != nil {
            log.Error().Msgf("Failed to kill process %s: %v", pid, err)
        }
    }
}


func readerManager(
	pl platforms.Platform,
	cfg *config.UserConfig,
	st *state.State,
	launchQueue *tokens.TokenQueue,
	softwareQueue chan *tokens.Token,
) {
	inputQueue := make(chan readers.Scan)

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
		if exitTimer != nil {
			stopped := exitTimer.Stop()
			if stopped {
				log.Info().Msg("cancelling previous exit timer")
			}
		}

		timerLen := time.Second * time.Duration(cfg.GetExitGameDelay())
		log.Debug().Msgf("exit timer set to: %s seconds", timerLen)
		exitTimer = time.NewTimer(timerLen)

		go func() {
			<-exitTimer.C

			if pl.GetActiveLauncher() == "" || st.GetSoftwareToken() == nil {
				log.Debug().Msg("no active launcher, not exiting")
				return
			}

			log.Info().Msg("exiting software")
			err := pl.KillLauncher()
			if err != nil {
				log.Warn().Msgf("error killing launcher: %s", err)
			}

			softwareQueue <- nil
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

				err := connectReaders(pl, cfg, st, inputQueue)
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
		case t := <-inputQueue:
			// a reader has sent a token for pre-processing
			log.Debug().Msgf("pre-processing token: %v", t)
			if t.Error != nil {
				log.Error().Msgf("error reading card: %s", err)
				playFail()
				lastError = time.Now()
				continue
			}
			scan = t.Token
		case stoken := <-softwareQueue:
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
			if !st.IsLauncherDisabled() {
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
				launchQueue.Enqueue(*scan)
			}
		} else {
			log.Info().Msg("token was removed")
			st.SetActiveCard(tokens.Token{})
			stopMPlayer()
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
