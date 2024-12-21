//go:build linux || darwin

package mister

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"os"
	"os/exec"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/rs/zerolog/log"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
	"github.com/wizzomafizzo/mrext/pkg/utils"
)

// TODO: don't want to depend on external aplay command, but i'm out of
//       time to keep messing with this. oto/beep would not work for me
//       and are annoying to compile statically

func Setup(tr *Tracker) error {
	if _, err := os.Stat(SuccessSoundFile); err != nil {
		// copy success sound to temp
		sf, err := os.Create(SuccessSoundFile)
		if err != nil {
			log.Error().Msgf("error creating success sound file: %s", err)
		}
		_, err = sf.Write(assets.SuccessSound)
		if err != nil {
			log.Error().Msgf("error writing success sound file: %s", err)
		}
		_ = sf.Close()
	}

	if _, err := os.Stat(FailSoundFile); err != nil {
		// copy fail sound to temp
		ff, err := os.Create(FailSoundFile)
		if err != nil {
			log.Error().Msgf("error creating fail sound file: %s", err)
		}
		_, err = ff.Write(assets.FailSound)
		if err != nil {
			log.Error().Msgf("error writing fail sound file: %s", err)
		}
		_ = ff.Close()
	}

	// attempt arcadedb update
	go func() {
		haveInternet := utils.WaitForInternet(30)
		if !haveInternet {
			log.Warn().Msg("no internet connection, skipping network tasks")
			return
		}

		arcadeDbUpdated, err := UpdateArcadeDb()
		if err != nil {
			log.Error().Msgf("failed to download arcade database: %s", err)
		}

		if arcadeDbUpdated {
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		} else {
			log.Info().Msg("arcade database is up to date")
		}

		m, err := ReadArcadeDb()
		if err != nil {
			log.Error().Msgf("failed to read arcade database: %s", err)
		} else {
			log.Info().Msgf("arcade database has %d entries", len(m))
		}
	}()

	return nil
}

func PlaySuccess(cfg *config.Instance) {
	if !cfg.AudioFeedback() {
		return
	}

	err := exec.Command("aplay", SuccessSoundFile).Start()
	if err != nil {
		log.Error().Msgf("error playing success sound: %s", err)
	}
}

func PlayFail(cfg *config.Instance) {
	if !cfg.AudioFeedback() {
		return
	}

	err := exec.Command("aplay", FailSoundFile).Start()
	if err != nil {
		log.Error().Msgf("error playing fail sound: %s", err)
	}
}

func ExitGame() {
	_ = mrextMister.LaunchMenu()
}

func GetActiveCoreName() string {
	coreName, err := mrextMister.GetActiveCoreName()
	if err != nil {
		log.Error().Msgf("error trying to get the core name: %s", err)
	}
	return coreName
}

func NormalizePath(cfg *config.Instance, path string) string {
	sys, err := games.BestSystemMatch(UserConfigToMrext(cfg), path)
	if err != nil {
		return path
	}

	var match string
	for _, parent := range mrextConfig.GamesFolders {
		if strings.HasPrefix(path, parent) {
			match = path[len(parent):]
			break
		}
	}

	if match == "" {
		return path
	}

	match = strings.Trim(match, "/")

	parts := strings.Split(match, "/")
	if len(parts) < 2 {
		return path
	}

	return sys.Id + "/" + strings.Join(parts[1:], "/")
}
