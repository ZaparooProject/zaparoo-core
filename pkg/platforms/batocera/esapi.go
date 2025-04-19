package batocera

import (
	"bytes"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
)

func apiRequest(_ *config.Instance, path string, body string) ([]byte, error) {
	kodiURL := "http://localhost:1234" + path
	var kodiReq *http.Request
	var err error

	if body != "" {
		kodiReq, err = http.NewRequest("POST", kodiURL, bytes.NewBuffer([]byte(body)))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	} else {
		kodiReq, err = http.NewRequest("GET", kodiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	}

	//kodiReq.Header.Set("Content-Type", "application/json")
	//kodiReq.Header.Set("Accept", "application/json")

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, nil
}
