package methods

import (
	"encoding/json"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

func HandleReaderWrite(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received reader write request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.ReaderWriteParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	rs := env.State.ListReaders()
	if len(rs) == 0 {
		return nil, errors.New("no readers connected")
	}

	rid := rs[0]
	lt := env.State.GetLastScanned()

	if !lt.ScanTime.IsZero() && !lt.FromAPI {
		rid = lt.Source
	}

	reader, ok := env.State.GetReader(rid)
	if !ok || reader == nil {
		return nil, errors.New("reader not connected: " + rs[0])
	}

	t, err := reader.Write(params.Text)
	if err != nil {
		log.Error().Err(err).Msg("error writing to reader")
		return nil, errors.New("error writing to reader")
	}

	if t != nil {
		env.State.SetWroteToken(t)
	}

	return nil, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleReaderWriteCancel(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received reader write cancel request")

	rs := env.State.ListReaders()
	if len(rs) == 0 {
		return nil, errors.New("no readers connected")
	}

	rid := rs[0]
	reader, ok := env.State.GetReader(rid)
	if !ok || reader == nil {
		return nil, errors.New("reader not connected: " + rs[0])
	}

	reader.CancelWrite()

	return nil, nil
}
