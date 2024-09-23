package methods

import (
	"errors"
	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/tapto/pkg/api/models"
	"github.com/wizzomafizzo/tapto/pkg/api/models/requests"
)

func HandleHistory(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received history request")

	entries, err := env.Database.GetHistory()
	if err != nil {
		log.Error().Err(err).Msgf("error getting history")
		return nil, errors.New("error getting history")
	}

	resp := models.HistoryResponse{
		Entries: make([]models.HistoryReponseEntry, len(entries)),
	}

	for i, e := range entries {
		resp.Entries[i] = models.HistoryReponseEntry{
			Time:    e.Time,
			Type:    e.Type,
			UID:     e.UID,
			Text:    e.Text,
			Data:    e.Data,
			Success: e.Success,
		}
	}

	return resp, nil
}