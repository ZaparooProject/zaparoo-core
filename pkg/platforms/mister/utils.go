//go:build linux

package mister

import (
	"fmt"
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
	mrextconfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	mrextmister "github.com/wizzomafizzo/mrext/pkg/mister"
)

func ExitGame() {
	_ = mrextmister.LaunchMenu()
}

func GetActiveCoreName() string {
	coreName, err := mrextmister.GetActiveCoreName()
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
	for _, parent := range mrextconfig.GamesFolders {
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
	_, err := os.Stat(mrextconfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	dev, err := os.OpenFile(mrextconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func(dev *os.File) {
		err := dev.Close()
		if err != nil {
			log.Error().Msgf("error closing cmd interface: %s", err)
		}
	}(dev)

	_, err = fmt.Fprintf(dev, "%s %s\n", cmd, args)
	if err != nil {
		return err
	}

	return nil
}
