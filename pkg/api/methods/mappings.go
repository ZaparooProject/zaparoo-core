package methods

import (
	"encoding/json"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

func HandleMappings(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received mappings request")

	resp := models.AllMappingsResponse{
		Mappings: make([]models.MappingResponse, 0),
	}

	mappings, err := env.Database.UserDB.GetAllMappings()
	if err != nil {
		log.Error().Err(err).Msg("error getting mappings")
		return nil, errors.New("error getting mappings")
	}

	mrs := make([]models.MappingResponse, 0)

	for _, m := range mappings {
		t := time.Unix(0, m.Added*int64(time.Millisecond))

		// keep compatibility for v0.1 api
		if m.Type == userdb.MappingTypeID {
			m.Type = userdb.LegacyMappingTypeUID
		} else if m.Type == userdb.MappingTypeValue {
			m.Type = userdb.LegacyMappingTypeText
		}

		mr := models.MappingResponse{
			ID:       strconv.FormatInt(m.DBID, 10),
			Added:    t.Format(time.RFC3339),
			Label:    m.Label,
			Enabled:  m.Enabled,
			Type:     m.Type,
			Match:    m.Match,
			Pattern:  m.Pattern,
			Override: m.Override,
		}

		mrs = append(mrs, mr)
	}

	resp.Mappings = mrs

	return resp, nil
}

func validateAddMappingParams(amr *models.AddMappingParams) error {
	if !utils.Contains(userdb.AllowedMappingTypes, amr.Type) {
		return errors.New("invalid type")
	}

	if !utils.Contains(userdb.AllowedMatchTypes, amr.Match) {
		return errors.New("invalid match")
	}

	if amr.Pattern == "" {
		return errors.New("missing pattern")
	}

	if amr.Match == userdb.MatchTypeRegex {
		_, err := regexp.Compile(amr.Pattern)
		if err != nil {
			return err
		}
	}

	return nil
}

func HandleAddMapping(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received add mapping request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.AddMappingParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	// convert old type names
	if params.Type == userdb.LegacyMappingTypeUID {
		params.Type = userdb.MappingTypeID
	} else if params.Type == userdb.LegacyMappingTypeText {
		params.Type = userdb.MappingTypeValue
	}

	err = validateAddMappingParams(&params)
	if err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, ErrInvalidParams
	}

	m := database.Mapping{
		Label:    params.Label,
		Enabled:  params.Enabled,
		Type:     params.Type,
		Match:    params.Match,
		Pattern:  params.Pattern,
		Override: params.Override,
	}

	err = env.Database.UserDB.AddMapping(m)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func HandleDeleteMapping(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received delete mapping request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.DeleteMappingParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	err = env.Database.UserDB.DeleteMapping(int64(params.Id))
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func validateUpdateMappingParams(umr *models.UpdateMappingParams) error {
	if umr.Label == nil && umr.Enabled == nil && umr.Type == nil && umr.Match == nil && umr.Pattern == nil && umr.Override == nil {
		return errors.New("missing fields")
	}

	if umr.Type != nil && !utils.Contains(userdb.AllowedMappingTypes, *umr.Type) {
		return errors.New("invalid type")
	}

	if umr.Match != nil && !utils.Contains(userdb.AllowedMatchTypes, *umr.Match) {
		return errors.New("invalid match")
	}

	if umr.Pattern != nil && *umr.Pattern == "" {
		return errors.New("missing pattern")
	}

	if umr.Match != nil && *umr.Match == userdb.MatchTypeRegex {
		_, err := regexp.Compile(*umr.Pattern)
		if err != nil {
			return err
		}
	}

	return nil
}

func HandleUpdateMapping(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received update mapping request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.UpdateMappingParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	// convert old type names
	if params.Type != nil {
		if *params.Type == userdb.LegacyMappingTypeUID {
			*params.Type = userdb.MappingTypeID
		} else if *params.Type == userdb.LegacyMappingTypeText {
			*params.Type = userdb.MappingTypeValue
		}
	}

	err = validateUpdateMappingParams(&params)
	if err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, ErrInvalidParams
	}

	oldMapping, err := env.Database.UserDB.GetMapping(int64(params.Id))
	if err != nil {
		return nil, err
	}

	newMapping := oldMapping

	if params.Label != nil {
		newMapping.Label = *params.Label
	}

	if params.Enabled != nil {
		newMapping.Enabled = *params.Enabled
	}

	if params.Type != nil {
		newMapping.Type = *params.Type
	}

	if params.Match != nil {
		newMapping.Match = *params.Match
	}

	if params.Pattern != nil {
		newMapping.Pattern = *params.Pattern
	}

	if params.Override != nil {
		newMapping.Override = *params.Override
	}

	err = env.Database.UserDB.UpdateMapping(int64(params.Id), newMapping)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func HandleReloadMappings(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received reload mappings request")

	mapDir := filepath.Join(utils.DataDir(env.Platform), config.MappingsDir)
	err := env.Config.LoadMappings(mapDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading mappings")
		return nil, errors.New("error loading mappings")
	}

	return nil, nil
}
