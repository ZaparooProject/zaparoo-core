package methods

import (
	"errors"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

func HandleTokens(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received tokens request")

	resp := models.TokensResponse{
		Active: make([]models.TokenResponse, 0),
	}

	active := env.State.GetActiveCard()
	if !active.ScanTime.IsZero() {
		resp.Active = append(resp.Active, models.TokenResponse{
			Type:     active.Type,
			UID:      active.UID,
			Text:     active.Text,
			Data:     active.Data,
			ScanTime: active.ScanTime,
		})
	}

	last := env.State.GetLastScanned()
	if !last.ScanTime.IsZero() {
		resp.Last = &models.TokenResponse{
			Type:     last.Type,
			UID:      last.UID,
			Text:     last.Text,
			Data:     last.Data,
			ScanTime: last.ScanTime,
		}
	}

	return resp, nil
}

func HandleHistory(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received history request")

	entries, err := env.Database.UserDB.GetHistory(0)
	if err != nil {
		log.Error().Err(err).Msgf("error getting history")
		return nil, errors.New("error getting history")
	}

	resp := models.HistoryResponse{
		Entries: make([]models.HistoryResponseEntry, len(entries)),
	}

	for i, e := range entries {
		resp.Entries[i] = models.HistoryResponseEntry{
			Time:    e.Time,
			Type:    e.Type,
			UID:     e.TokenID,
			Text:    e.TokenValue,
			Data:    e.TokenData,
			Success: e.Success,
		}
	}

	return resp, nil
}
