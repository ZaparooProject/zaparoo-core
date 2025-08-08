package mistermain

import (
	"os"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func GetActiveCoreName() string {
	data, err := os.ReadFile(config.CoreNameFile)
	if err != nil {
		log.Error().Msgf("error trying to get the core name: %s", err)
		return ""
	}

	return string(data)
}
