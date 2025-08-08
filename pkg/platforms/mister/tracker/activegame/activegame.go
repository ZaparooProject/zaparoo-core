package activegame

import (
	"os"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func ActiveGameEnabled() bool {
	_, err := os.Stat(config.ActiveGameFile)
	return err == nil
}

func SetActiveGame(path string) error {
	file, err := os.Create(config.ActiveGameFile)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close active game file")
		}
	}()

	_, err = file.WriteString(path)
	if err != nil {
		return err
	}

	return nil
}

func GetActiveGame() (string, error) {
	data, err := os.ReadFile(config.ActiveGameFile)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
