//go:build linux || darwin

package mister

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
)

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
