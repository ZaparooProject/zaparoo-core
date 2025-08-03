package methods

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const defaultMaxResults = 250

type indexingStatusVals struct {
	currentDesc string
	totalSteps  int
	currentStep int
	totalFiles  int
	indexing    bool
}

type indexingStatus struct {
	currentDesc string
	totalSteps  int
	currentStep int
	totalFiles  int
	mu          sync.RWMutex
	indexing    bool
}

func (s *indexingStatus) get() indexingStatusVals {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return indexingStatusVals{
		indexing:    s.indexing,
		totalSteps:  s.totalSteps,
		currentStep: s.currentStep,
		currentDesc: s.currentDesc,
		totalFiles:  s.totalFiles,
	}
}

func (s *indexingStatus) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = true
	s.totalSteps = 0
	s.currentStep = 0
	s.currentDesc = ""
	s.totalFiles = 0
}

func (s *indexingStatus) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = false
	s.totalSteps = 0
	s.currentStep = 0
	s.currentDesc = ""
	s.totalFiles = 0
}

func (s *indexingStatus) set(vals indexingStatusVals) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = vals.indexing
	s.totalSteps = vals.totalSteps
	s.currentStep = vals.currentStep
	s.currentDesc = vals.currentDesc
	s.totalFiles = vals.totalFiles
}

func newIndexingStatus() *indexingStatus {
	return &indexingStatus{}
}

var statusInstance = newIndexingStatus()

func generateMediaDB(
	pl platforms.Platform,
	cfg *config.Instance,
	ns chan<- models.Notification,
	systems []systemdefs.System,
	db *database.Database,
) error {
	if statusInstance.get().indexing {
		return errors.New("indexing already in progress")
	}

	statusInstance.start()
	startTime := time.Now()

	log.Info().Msg("generating media db")
	notifications.MediaIndexing(ns, models.IndexingStatusResponse{
		Exists:   false,
		Indexing: true,
	})

	go func() {
		total, err := mediascanner.NewNamesIndex(pl, cfg, systems, db, func(status mediascanner.IndexStatus) {
			var desc string
			switch status.Step {
			case 1:
				desc = "Finding media folders"
			case status.Total:
				desc = "Writing database"
			default:
				system, err := systemdefs.GetSystem(status.SystemID)
				if err != nil {
					desc = status.SystemID
				} else {
					md, err := assets.GetSystemMetadata(system.ID)
					if err != nil {
						desc = system.ID
					} else {
						desc = md.Name
					}
				}
			}
			statusInstance.set(indexingStatusVals{
				indexing:    true,
				totalSteps:  status.Total,
				currentStep: status.Step,
				currentDesc: desc,
				totalFiles:  status.Files,
			})

			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:             false,
				Indexing:           true,
				TotalSteps:         &status.Total,
				CurrentStep:        &status.Step,
				CurrentStepDisplay: &desc,
				TotalFiles:         &status.Files,
			})

			log.Debug().Msgf("indexing status: %v", indexingStatusVals{
				indexing:    true,
				totalSteps:  status.Total,
				currentStep: status.Step,
				currentDesc: desc,
				totalFiles:  status.Files,
			})
		})
		if err != nil {
			log.Error().Err(err).Msg("error generating media db")
			// TODO: error notification to client
			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:     false,
				Indexing:   false,
				TotalFiles: &total,
			})
			statusInstance.clear()
			return
		}
		log.Info().Msg("finished generating media db successfully")
		notifications.MediaIndexing(ns, models.IndexingStatusResponse{
			Exists:     true,
			Indexing:   false,
			TotalFiles: &total,
		})
		statusInstance.clear()
		log.Info().Msgf("finished generating media db in %v", time.Since(startTime))
		return
	}()

	return nil
}

func HandleGenerateMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
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

	err := generateMediaDB(
		env.Platform,
		env.Config,
		env.State.Notifications,
		systems,
		env.Database,
	)

	return nil, err
}

func HandleMediaSearch(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
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

	results := make([]models.SearchResultMedia, 0)
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
			sys, systemErr := systemdefs.GetSystem(s)
			if systemErr != nil {
				return nil, errors.New("error getting system: " + systemErr.Error())
			}

			systems = append(systems, *sys)
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

		resultSystem := models.System{
			ID: system.ID,
		}

		metadata, err := assets.GetSystemMetadata(system.ID)
		if err != nil {
			resultSystem.Name = system.ID
			log.Err(err).Msg("error getting system metadata")
		} else {
			resultSystem.Name = metadata.Name
		}

		results = append(results, models.SearchResultMedia{
			System: resultSystem,
			Name:   result.Name,
			Path:   env.Platform.NormalizePath(env.Config, result.Path),
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

func HandleMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received media request")

	resp := models.MediaResponse{
		Active: make([]models.ActiveMedia, 0),
	}

	activeMedia := env.State.ActiveMedia()
	if activeMedia != nil && activeMedia.Path != "" {
		system, err := assets.GetSystemMetadata(activeMedia.SystemID)
		if err != nil {
			return nil, fmt.Errorf("error getting system metadata: %w", err)
		}

		resp.Active = append(resp.Active, models.ActiveMedia{
			SystemID:   system.ID,
			SystemName: system.Name,
			Name:       activeMedia.Name,
			Path:       env.Platform.NormalizePath(env.Config, activeMedia.Path),
		})
	}

	status := statusInstance.get()
	resp.Database.Indexing = status.indexing

	if resp.Database.Indexing {
		resp.Database.Exists = false
		resp.Database.TotalSteps = &status.totalSteps
		resp.Database.CurrentStep = &status.currentStep
		resp.Database.CurrentStepDisplay = &status.currentDesc
		resp.Database.TotalFiles = &status.totalFiles
	} else {
		lastGenerated, err := env.Database.MediaDB.GetLastGenerated()
		if err != nil {
			return nil, fmt.Errorf("error getting last generated time: %w", err)
		}

		resp.Database.Exists = !time.Unix(0, 0).Equal(lastGenerated) && !status.indexing
	}

	return resp, nil
}

func HandleUpdateActiveMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
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
		SystemID:   system.ID,
		SystemName: systemMeta.Name,
		Name:       params.MediaName,
		Path:       env.Platform.NormalizePath(env.Config, params.MediaPath),
	}

	env.State.SetActiveMedia(&activeMedia)
	return nil, nil
}

func HandleActiveMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received active media request")

	media := env.State.ActiveMedia()
	if media == nil {
		return nil, nil
	}

	return models.ActiveMedia{
		SystemID:   media.SystemID,
		SystemName: media.SystemName,
		Name:       media.Name,
		Path:       media.Path,
	}, nil
}
