//go:build linux || darwin

package mister

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"os"
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

func RunDevCmd(cmd string, args string) error {
	_, err := os.Stat(mrextConfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	dev, err := os.OpenFile(mrextConfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func(dev *os.File) {
		err := dev.Close()
		if err != nil {
			log.Error().Msgf("error closing cmd interface: %s", err)
		}
	}(dev)

	_, err = dev.WriteString(fmt.Sprintf("%s %s\n", cmd, args))
	if err != nil {
		return err
	}

	return nil
}
