package methods

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

func HandleSystems(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received systems request")

	indexed, err := env.Database.MediaDB.IndexedSystems()
	if err != nil {
		log.Error().Err(err).Msgf("error getting indexed systems")
		indexed = []string{}
	}

	if len(indexed) == 0 {
		log.Warn().Msg("no indexed systems found")
	}

	respSystems := make([]models.System, 0)

	for _, id := range indexed {
		system, err := systemdefs.GetSystem(id)
		if err != nil {
			log.Error().Err(err).Msgf("error getting system: %s", id)
			continue
		}

		sr := models.System{
			ID: system.ID,
		}

		sm, err := assets.GetSystemMetadata(id)
		if err != nil {
			log.Error().Err(err).Msgf("error getting system metadata: %s", id)
		}

		sr.Name = sm.Name
		sr.Category = sm.Category

		respSystems = append(respSystems, sr)
	}

	return models.SystemsResponse{
		Systems: respSystems,
	}, nil
}
