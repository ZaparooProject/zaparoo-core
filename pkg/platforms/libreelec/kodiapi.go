package libreelec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"strings"
)

type KodiAPIMethod string

const (
	KodiAPIMethodPlayerOpen KodiAPIMethod = "Player.Open"
)

type KodiPlayerOpenItemParams struct {
	File string `json:"file"`
}

type KodiPlayerOpenParams struct {
	Item KodiPlayerOpenItemParams `json:"item"`
}

type KodiAPIPayload struct {
	JsonRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  KodiAPIMethod `json:"method"`
	Params  any           `json:"params"`
}

func apiRequest(_ *config.Instance, method KodiAPIMethod, params any) ([]byte, error) {
	req := KodiAPIPayload{
		JsonRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	reqJson, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

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

	return body, nil
}

func kodiLaunchRequest(cfg *config.Instance, path string) error {
	params := KodiPlayerOpenParams{
		Item: KodiPlayerOpenItemParams{
			File: path,
		},
	}

	_, err := apiRequest(cfg, KodiAPIMethodPlayerOpen, params)
	return err
}

func kodiLaunchMovieRequest(cfg *config.Instance, path string) error {
	id := strings.TrimPrefix("kodi.movie://", path)

	_, err := apiRequest(
		cfg,
		"", // TODO: replace with the correct method
		id, // TODO: replace with your own params
	)
	return err
}

type KodiMovieScanResults struct {
	// TODO: just using a fake response for the example, change this
	Results []string `json:"results"`
}

func kodiScanMovies(
	cfg *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	resp, err := apiRequest(
		cfg,
		"",  // TODO: replace with the correct method
		nil, // TODO: replace with your own params
	)
	if err != nil {
		return nil, err
	}

	var scanResults KodiMovieScanResults
	err = json.Unmarshal(resp, &scanResults)
	if err != nil {
		return nil, err
	}

	for _, movie := range scanResults.Results {
		results = append(results, platforms.ScanResult{
			Name: movie,
			Path: SchemeKodiMovie + "://" + movie,
		})
	}

	return results, nil
}
