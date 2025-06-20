package libreelec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"strconv"
	"strings"
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

type KodiPlayerOpenParams struct {
	Item KodiItem `json:"item"`
}

type KodiPlayerStopParams struct {
	PlayerID int `json:"playerid"`
}

type KodiPlayer struct {
	ID   int    `json:"playerid"`
	Type string `json:"type"`
}

type KodiPlayerGetActivePlayersResponse []KodiPlayer

type KodiVideoLibraryGetMoviesResponse struct {
	Movies []KodiItem `json:"movies"`
}

type KodiAPIPayload struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  KodiAPIMethod `json:"method"`
	Params  any           `json:"params,omitempty"`
}

type KodiAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type KodiAPIResponse struct {
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *KodiAPIError   `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
}

func apiRequest(
	_ *config.Instance,
	method KodiAPIMethod,
	params any,
) (json.RawMessage, error) {
	req := KodiAPIPayload{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  method,
		Params:  params,
	}

	reqJson, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	log.Debug().Msgf("request: %s", string(reqJson))

	kodiURL := "http://localhost:8080/jsonrpc" // TODO: allow setting from config
	kodiReq, err := http.NewRequest("POST", kodiURL, bytes.NewBuffer(reqJson))
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
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}(resp.Body)

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
	})
	return err
}

func kodiLaunchMovieRequest(cfg *config.Instance, path string) error {
	pathID := strings.TrimPrefix(path, SchemeKodiMovie+"://")
	pathID = strings.SplitN(pathID, "/", 2)[0]

	movieID, err := strconv.Atoi(pathID)
	if err != nil {
		return err
	}

	params := KodiPlayerOpenParams{
		Item: KodiItem{
			MovieID: movieID,
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
		return nil, err
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
