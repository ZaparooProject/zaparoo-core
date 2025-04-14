package libreelec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
	"net/http"
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

func apiRequest(cfg *config.Instance, method KodiAPIMethod, params any) error {
	req := KodiAPIPayload{
		JsonRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	reqJson, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	kodiURL := "http://localhost:8080/jsonrpc" // TODO: allow setting from config
	kodiReq, err := http.NewRequest("POST", kodiURL, bytes.NewBuffer(reqJson))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	kodiReq.Header.Set("Content-Type", "application/json")
	kodiReq.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(kodiReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	log.Debug().Any("response", resp).Msg("kodi response")
	// TODO: parse the response

	return nil
}

func kodiLaunchRequest(_ *config.Instance, path string) error {
	params := KodiPlayerOpenParams{
		Item: KodiPlayerOpenItemParams{
			File: path,
		},
	}

	return apiRequest(nil, KodiAPIMethodPlayerOpen, params)
}
