//go:build linux

package mister

import (
	"fmt"
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	mrextconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/games"
	mrextmister "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/mister"
	"github.com/rs/zerolog/log"
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

	return sys.ID + "/" + strings.Join(parts[1:], "/")
}

func RunDevCmd(cmd, args string) error {
	_, err := os.Stat(mrextconfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	dev, err := os.OpenFile(mrextconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func(dev *os.File) {
		closeErr := dev.Close()
		if closeErr != nil {
			log.Error().Msgf("error closing cmd interface: %s", closeErr)
		}
	}(dev)

	_, err = fmt.Fprintf(dev, "%s %s\n", cmd, args)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}
