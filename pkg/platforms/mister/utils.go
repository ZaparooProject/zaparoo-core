//go:build linux

package mister

import (
	"fmt"
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func GetActiveCoreName() string {
	data, err := os.ReadFile(misterconfig.CoreNameFile)
	if err != nil {
		log.Error().Msgf("error trying to get the core name: %s", err)
		return ""
	}

	return string(data)
}

func NormalizePath(cfg *config.Instance, pl platforms.Platform, path string) string {
	launchers := helpers.PathToLaunchers(cfg, pl, path)
	if len(launchers) == 0 {
		return path
	}

	// TODO: something smarter than first match
	launcher := launchers[0]

	lowerPath := strings.ToLower(path)
	var match string
	for _, parent := range pl.RootDirs(cfg) {
		if strings.HasPrefix(lowerPath, strings.ToLower(parent)) {
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

	return launcher.SystemID + "/" + strings.Join(parts[1:], "/")
}

func RunDevCmd(cmd, args string) error {
	_, err := os.Stat(misterconfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	dev, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
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
