package methods

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"

	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const defaultMaxResults = 250

type IndexingStatus struct {
	mu          sync.Mutex
	Indexing    bool
	TotalSteps  int
	CurrentStep int
	CurrentDesc string
	TotalFiles  int
}

func (s *IndexingStatus) GenerateMediaDB(
	pl platforms.Platform,
	cfg *config.Instance,
	ns chan<- models.Notification,
	systems []systemdefs.System,
	db *database.Database,
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

	log.Info().Msg("generating media db")
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Exists:   false,
		Indexing: true,
	})

	go func() {
		defer s.mu.Unlock()

		total, err := mediascanner.NewNamesIndex(pl, cfg, systems, db, func(status mediascanner.IndexStatus) {
			s.TotalSteps = status.Total
			s.CurrentStep = status.Step
			s.TotalFiles = status.Files
			if status.Step == 1 {
				s.CurrentDesc = "Finding media folders"
			} else if status.Step == status.Total {
				s.CurrentDesc = "Writing database"
			} else {
				system, err := systemdefs.GetSystem(status.SystemID)
				if err != nil {
					s.CurrentDesc = status.SystemID
				} else {
					md, err := assets.GetSystemMetadata(system.ID)
					if err != nil {
						s.CurrentDesc = system.ID
					} else {
						s.CurrentDesc = md.Name
					}
				}
			}
			log.Debug().Msgf("indexing status: %v", s)
			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:             true,
				Indexing:           true,
				TotalSteps:         &s.TotalSteps,
				CurrentStep:        &s.CurrentStep,
				CurrentStepDisplay: &s.CurrentDesc,
				TotalFiles:         &s.TotalFiles,
			})
		})
		if err != nil {
			log.Error().Err(err).Msg("error generating media db")
		}

		s.Indexing = false
		s.TotalSteps = 0
		s.CurrentStep = 0
		s.CurrentDesc = ""
		s.TotalFiles = 0

		log.Info().Msg("finished generating media db")
		notifications.MediaIndexing(ns, models.IndexingStatusResponse{
			Exists:     true,
			Indexing:   false,
			TotalFiles: &total,
		})
	}()
}

func NewIndexingStatus() *IndexingStatus {
	return &IndexingStatus{}
}

var IndexingStatusInstance = NewIndexingStatus()

func HandleGenerateMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received generate media request")

	var systems []systemdefs.System
	if len(env.Params) > 0 {
		var params models.MediaIndexParams
		err := json.Unmarshal(env.Params, &params)
		if err != nil {
			return nil, ErrInvalidParams
		}

		if params.Systems == nil || len(*params.Systems) == 0 {
			systems = systemdefs.AllSystems()
		}

		for _, s := range *params.Systems {
			system, err := systemdefs.GetSystem(s)
			if err != nil {
				return nil, errors.New("error getting system: " + err.Error())
			}

			systems = append(systems, *system)
		}
	} else {
		systems = systemdefs.AllSystems()
	}

	IndexingStatusInstance.GenerateMediaDB(
		env.Platform,
		env.Config,
		env.State.Notifications,
		systems,
		env.Database,
	)
	return nil, nil
}

func HandleMediaSearch(env requests.RequestEnv) (any, error) {
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
	var search []database.SearchResult
	system := params.Systems
	query := params.Query

	if system == nil || len(*system) == 0 {
		search, err = env.Database.MediaDB.SearchMediaPathWords(systemdefs.AllSystems(), query)
		if err != nil {
			return nil, errors.New("error searching all media: " + err.Error())
		}
	} else {
		systems := make([]systemdefs.System, 0)
		for _, s := range *system {
			system, err := systemdefs.GetSystem(s)
			if err != nil {
				return nil, errors.New("error getting system: " + err.Error())
			}

			systems = append(systems, *system)
		}

		search, err = env.Database.MediaDB.SearchMediaPathWords(systems, query)
		if err != nil {
			return nil, errors.New("error searching media: " + err.Error())
		}
	}

	for _, result := range search {
		system, err := systemdefs.GetSystem(result.SystemID)
		if err != nil {
			continue
		}

		results = append(results, models.SearchResultMedia{
			System: models.System{
				Id:   system.ID,
				Name: system.ID,
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
		Active: make([]models.ActiveMedia, 0),
	}

	if env.Platform.ActiveGamePath() != "" {
		system, err := assets.GetSystemMetadata(env.Platform.ActiveSystem())
		if err != nil {
			return nil, fmt.Errorf("error getting system metadata: %w", err)
		}

		resp.Active = append(resp.Active, models.ActiveMedia{
			SystemId:   system.Id,
			SystemName: system.Name,
			MediaName:  env.Platform.ActiveGameName(),
			MediaPath:  env.Platform.NormalizePath(env.Config, env.Platform.ActiveGamePath()),
		})
	}

	lastGenerated, err := env.Database.MediaDB.GetLastGenerated()
	if err != nil {
		return nil, fmt.Errorf("error getting last generated time: %w", err)
	}
	resp.Database.Exists = !time.Unix(0, 0).Equal(lastGenerated)
	resp.Database.Indexing = IndexingStatusInstance.Indexing

	if resp.Database.Indexing {
		resp.Database.TotalSteps = &IndexingStatusInstance.TotalSteps
		resp.Database.CurrentStep = &IndexingStatusInstance.CurrentStep
		resp.Database.CurrentStepDisplay = &IndexingStatusInstance.CurrentDesc
		resp.Database.TotalFiles = &IndexingStatusInstance.TotalFiles
	}

	return resp, nil
}

func HandleUpdateActiveMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received update active media request")

	if len(env.Params) == 0 {
		log.Info().Msg("clearing active media")
		env.State.SetActiveMedia(nil)
		return nil, nil
	}

	var params models.UpdateActiveMediaParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	system, err := systemdefs.LookupSystem(params.SystemID)
	if err != nil {
		return nil, fmt.Errorf("error looking up system: %w", err)
	}

	systemMeta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting system metadata: %w", err)
	}

	activeMedia := models.ActiveMedia{
		SystemId:   system.ID,
		SystemName: systemMeta.Name,
		MediaName:  params.MediaName,
		MediaPath:  env.Platform.NormalizePath(env.Config, params.MediaPath),
	}

	env.State.SetActiveMedia(&activeMedia)
	return nil, nil
}

func HandleActiveMedia(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received active media request")

	media := env.State.ActiveMedia()
	if media == nil {
		return nil, nil
	}

	return models.ActiveMedia{
		SystemId:   media.SystemId,
		SystemName: media.SystemName,
		MediaName:  media.MediaName,
		MediaPath:  media.MediaPath,
	}, nil
}
