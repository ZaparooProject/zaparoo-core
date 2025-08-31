//go:build linux

package libreelec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type KodiAPIMethod string

const (
	KodiAPIMethodPlayerOpen              KodiAPIMethod = "Player.Open"
	KodiAPIMethodPlayerStop              KodiAPIMethod = "Player.Stop"
	KodiAPIMethodPlayerGetActivePlayers  KodiAPIMethod = "Player.GetActivePlayers"
	KodiAPIMethodVideoLibraryGetMovies   KodiAPIMethod = "VideoLibrary.GetMovies"
	KodiAPIMethodVideoLibraryGetTVShows  KodiAPIMethod = "VideoLibrary.GetTVShows"
	KodiAPIMethodVideoLibraryGetEpisodes KodiAPIMethod = "VideoLibrary.GetEpisodes"
)

type KodiItem struct {
	Label     string `json:"label,omitempty"`
	File      string `json:"file,omitempty"`
	MovieID   int    `json:"movieid,omitempty"`
	TVShowID  int    `json:"tvshowid,omitempty"`
	EpisodeID int    `json:"episodeid,omitempty"`
}

type KodiItemOptions struct {
	Resume bool `json:"resume"`
}

type KodiPlayerOpenParams struct {
	Item    KodiItem        `json:"item"`
	Options KodiItemOptions `json:"options,omitempty"`
}

type KodiPlayerStopParams struct {
	PlayerID int `json:"playerid"`
}

type KodiPlayer struct {
	Type string `json:"type"`
	ID   int    `json:"playerid"`
}

type KodiPlayerGetActivePlayersResponse []KodiPlayer

type KodiVideoLibraryGetMoviesResponse struct {
	Movies []KodiItem `json:"movies"`
}

type KodiVideoLibraryGetTVShowsResponse struct {
	TVShows []KodiItem `json:"tvshows"`
}

type KodiVideoLibraryGetEpisodesParams struct {
	TVShowID int `json:"tvshowid"`
}

type KodiVideoLibraryGetEpisodesResponse struct {
	Episodes []KodiItem `json:"episodes"`
}

type KodiAPIPayload struct {
	Params  any           `json:"params,omitempty"`
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  KodiAPIMethod `json:"method"`
}

type KodiAPIError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type KodiAPIResponse struct {
	Error   *KodiAPIError   `json:"error,omitempty"`
	ID      string          `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
}

func apiRequest(
	cfg *config.Instance,
	method KodiAPIMethod,
	params any,
) (json.RawMessage, error) {
	return apiRequestWithURL(cfg, method, params, "")
}

func apiRequestWithURL(
	_ *config.Instance,
	method KodiAPIMethod,
	params any,
	customURL string,
) (json.RawMessage, error) {
	req := KodiAPIPayload{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  method,
		Params:  params,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	log.Debug().Msgf("request: %s", string(reqJSON))

	kodiURL := "http://localhost:8080/jsonrpc" // TODO: allow setting from config
	if customURL != "" {
		kodiURL = customURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kodiReq, err := http.NewRequestWithContext(ctx, http.MethodPost, kodiURL, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	kodiReq.Header.Set("Content-Type", "application/json")
	kodiReq.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(kodiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp == nil {
		return nil, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResp KodiAPIResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("error from kodi api: %s", apiResp.Error.Message)
	}

	return apiResp.Result, nil
}

func kodiLaunchFileRequest(cfg *config.Instance, path string) error {
	_, err := apiRequest(cfg, KodiAPIMethodPlayerOpen, KodiPlayerOpenParams{
		Item: KodiItem{
			File: path,
		},
		Options: KodiItemOptions{
			Resume: true,
		},
	})
	return err
}

func kodiLaunchFileRequestWithURL(cfg *config.Instance, path, customURL string) error {
	_, err := apiRequestWithURL(cfg, KodiAPIMethodPlayerOpen, KodiPlayerOpenParams{
		Item: KodiItem{
			File: path,
		},
		Options: KodiItemOptions{
			Resume: true,
		},
	}, customURL)
	return err
}

func kodiLaunchMovieRequest(cfg *config.Instance, path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiMovie+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	movieID, err := strconv.Atoi(pathID)
	if err != nil {
		return fmt.Errorf("failed to parse movie ID %q: %w", pathID, err)
	}

	params := KodiPlayerOpenParams{
		Item: KodiItem{
			MovieID: movieID,
		},
		Options: KodiItemOptions{
			Resume: true,
		},
	}

	_, err = apiRequest(cfg, KodiAPIMethodPlayerOpen, params)

	return err
}

func kodiLaunchTVRequest(cfg *config.Instance, path string) error {
	var params KodiPlayerOpenParams
	if !strings.HasPrefix(path, SchemeKodiEpisode+"://") {
		return fmt.Errorf("invalid path: %s", path)
	}
	id := strings.TrimPrefix(path, SchemeKodiEpisode+"://")
	id = strings.SplitN(id, "/", 2)[0]
	intID, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("failed to parse episode ID %q: %w", id, err)
	}
	params = KodiPlayerOpenParams{
		Item: KodiItem{
			EpisodeID: intID,
		},
		Options: KodiItemOptions{
			Resume: true,
		},
	}

	_, err = apiRequest(cfg, KodiAPIMethodPlayerOpen, params)

	return err
}

func kodiScanMovies(
	cfg *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	resp, err := apiRequest(cfg, KodiAPIMethodVideoLibraryGetMovies, nil)
	if err != nil {
		return nil, err
	}

	var scanResults KodiVideoLibraryGetMoviesResponse
	err = json.Unmarshal(resp, &scanResults)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal movies response: %w", err)
	}

	for _, movie := range scanResults.Movies {
		results = append(results, platforms.ScanResult{
			Name: movie.Label,
			Path: fmt.Sprintf(
				"%s://%d/%s",
				SchemeKodiMovie,
				movie.MovieID,
				movie.Label,
			),
		})
	}

	return results, nil
}

func kodiScanTV(
	cfg *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	resp, err := apiRequest(cfg, KodiAPIMethodVideoLibraryGetTVShows, nil)
	if err != nil {
		return nil, err
	}

	var scanResults KodiVideoLibraryGetTVShowsResponse
	err = json.Unmarshal(resp, &scanResults)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal TV shows response: %w", err)
	}

	for _, show := range scanResults.TVShows {
		epsResp, err := apiRequest(cfg, KodiAPIMethodVideoLibraryGetEpisodes,
			KodiVideoLibraryGetEpisodesParams{
				TVShowID: show.TVShowID,
			})
		if err != nil {
			return nil, err
		}

		var epsResults KodiVideoLibraryGetEpisodesResponse
		err = json.Unmarshal(epsResp, &epsResults)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal episodes response: %w", err)
		}

		for _, ep := range epsResults.Episodes {
			label := show.Label + " - " + ep.Label
			results = append(results, platforms.ScanResult{
				Name: label,
				Path: fmt.Sprintf(
					"%s://%d/%s",
					SchemeKodiEpisode,
					ep.EpisodeID,
					label,
				),
			})
		}
	}

	return results, nil
}

func kodiStop(cfg *config.Instance) error {
	playersResp, err := apiRequest(cfg, KodiAPIMethodPlayerGetActivePlayers, nil)
	if err != nil {
		return err
	}

	var players KodiPlayerGetActivePlayersResponse
	err = json.Unmarshal(playersResp, &players)
	if err != nil {
		return fmt.Errorf("failed to unmarshal players response: %w", err)
	}

	if len(players) == 0 {
		return nil
	}

	playerID := players[0].ID

	_, err = apiRequest(cfg, KodiAPIMethodPlayerStop, KodiPlayerStopParams{
		PlayerID: playerID,
	})
	return err
}
