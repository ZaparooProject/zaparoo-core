package methods

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
)

func HandleVersion(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received version request")
	return models.VersionResponse{
		Version:  config.AppVersion,
		Platform: env.Platform.Id(),
	}, nil
}
