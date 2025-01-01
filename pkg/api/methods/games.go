package methods

import (
	"encoding/json"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const defaultMaxResults = 250

type Index struct {
	mu          sync.Mutex
	Indexing    bool
	TotalSteps  int
	CurrentStep int
	CurrentDesc string
	TotalFiles  int
}

func (s *Index) Exists(platform platforms.Platform) bool {
	return gamesdb.Exists(platform)
}

func (s *Index) GenerateIndex(
	pl platforms.Platform,
	cfg *config.Instance,
	ns chan<- models.Notification,
	systems []gamesdb.System,
) {
	// TODO: this function should block until index is complete
	// confirm that concurrent requests is working

	if s.Indexing {
		// TODO: return an error to client
		return
	}

	s.mu.Lock()
	s.Indexing = true
	s.TotalFiles = 0

	log.Info().Msg("generating media index")
	ns <- models.Notification{
		Method: models.NotificationMediaIndexing,
		Params: models.IndexStatusResponse{
			Exists:      false,
			Indexing:    true,
			TotalSteps:  0,
			CurrentStep: 0,
			CurrentDesc: "",
			TotalFiles:  0,
		},
	}

	go func() {
		defer s.mu.Unlock()

		total, err := gamesdb.NewNamesIndex(pl, cfg, systems, func(status gamesdb.IndexStatus) {
			s.TotalSteps = status.Total
			s.CurrentStep = status.Step
			s.TotalFiles = status.Files
			if status.Step == 1 {
				s.CurrentDesc = "Finding media folders"
			} else if status.Step == status.Total {
				s.CurrentDesc = "Writing database"
			} else {
				system, err := gamesdb.GetSystem(status.SystemId)
				if err != nil {
					s.CurrentDesc = status.SystemId
				} else {
					md, err := assets.GetSystemMetadata(system.Id)
					if err != nil {
						s.CurrentDesc = system.Id
					} else {
						s.CurrentDesc = md.Name
					}
				}
			}
			log.Debug().Msgf("indexing status: %v", s)
			ns <- models.Notification{
				Method: models.NotificationMediaIndexing,
				Params: models.IndexStatusResponse{
					Exists:      true,
					Indexing:    true,
					TotalSteps:  s.TotalSteps,
					CurrentStep: s.CurrentStep,
					CurrentDesc: s.CurrentDesc,
					TotalFiles:  s.TotalFiles,
				},
			}
		})
		if err != nil {
			log.Error().Err(err).Msg("error generating media index")
		}

		s.Indexing = false
		s.TotalSteps = 0
		s.CurrentStep = 0
		s.CurrentDesc = ""
		s.TotalFiles = 0

		log.Info().Msg("finished generating media index")
		ns <- models.Notification{
			Method: models.NotificationMediaIndexing,
			Params: models.IndexStatusResponse{
				Exists:      true,
				Indexing:    false,
				TotalSteps:  0,
				CurrentStep: 0,
				CurrentDesc: "",
				TotalFiles:  total,
			},
		}
	}()
}

func NewIndex() *Index {
	return &Index{}
}

var IndexInstance = NewIndex()

func HandleIndexMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received index media request")

	var systems []gamesdb.System
	if len(env.Params) > 0 {
		var params models.MediaIndexParams
		err := json.Unmarshal(env.Params, &params)
		if err != nil {
			return nil, ErrInvalidParams
		}

		if params.Systems == nil || len(*params.Systems) == 0 {
			systems = gamesdb.AllSystems()
		}

		for _, s := range *params.Systems {
			system, err := gamesdb.GetSystem(s)
			if err != nil {
				return nil, errors.New("error getting system: " + err.Error())
			}

			systems = append(systems, *system)
		}
	} else {
		systems = gamesdb.AllSystems()
	}

	IndexInstance.GenerateIndex(
		env.Platform,
		env.Config,
		env.State.Notifications,
		systems,
	)
	return nil, nil
}

func HandleGames(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received media search request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.SearchParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	maxResults := defaultMaxResults
	if params.MaxResults != nil && *params.MaxResults > 0 {
		maxResults = *params.MaxResults
	}

	if params.Query == "" && (params.Systems == nil || len(*params.Systems) == 0) {
		return nil, errors.New("query or system is required")
	}

	var results = make([]models.SearchResultMedia, 0)
	var search []gamesdb.SearchResult
	system := params.Systems
	query := params.Query

	if system == nil || len(*system) == 0 {
		search, err = gamesdb.SearchNamesWords(env.Platform, gamesdb.AllSystems(), query)
		if err != nil {
			return nil, errors.New("error searching all media: " + err.Error())
		}
	} else {
		systems := make([]gamesdb.System, 0)
		for _, s := range *system {
			system, err := gamesdb.GetSystem(s)
			if err != nil {
				return nil, errors.New("error getting system: " + err.Error())
			}

			systems = append(systems, *system)
		}

		search, err = gamesdb.SearchNamesWords(env.Platform, systems, query)
		if err != nil {
			return nil, errors.New("error searching media: " + err.Error())
		}
	}

	for _, result := range search {
		system, err := gamesdb.GetSystem(result.SystemId)
		if err != nil {
			continue
		}

		results = append(results, models.SearchResultMedia{
			System: models.System{
				Id:   system.Id,
				Name: system.Id,
			},
			Name: result.Name,
			Path: env.Platform.NormalizePath(env.Config, result.Path),
		})
	}

	total := len(results)

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return models.SearchResults{
		Results: results,
		Total:   total,
	}, nil
}

func HandleMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received media request")

	resp := models.MediaResponse{
		Active: make([]models.PlayingResponse, 0),
	}

	if env.Platform.ActiveGamePath() != "" {
		system, err := assets.GetSystemMetadata(env.Platform.ActiveSystem())
		if err != nil {
			return nil, errors.New("error getting system metadata: " + err.Error())
		}

		resp.Active = append(resp.Active, models.PlayingResponse{
			SystemId:   system.Id,
			SystemName: system.Name,
			MediaName:  env.Platform.ActiveGameName(),
			MediaPath:  env.Platform.NormalizePath(env.Config, env.Platform.ActiveGamePath()),
		})
	}

	resp.Database.Exists = IndexInstance.Exists(env.Platform)
	resp.Database.Indexing = IndexInstance.Indexing

	if resp.Database.Indexing {
		resp.Database.TotalSteps = &IndexInstance.TotalSteps
		resp.Database.CurrentStep = &IndexInstance.CurrentStep
		resp.Database.CurrentStepDisplay = &IndexInstance.CurrentDesc
		resp.Database.TotalFiles = &IndexInstance.TotalFiles
	}

	return resp, nil
}
