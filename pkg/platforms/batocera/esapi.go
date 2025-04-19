package batocera

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
)

const apiURL = "http://localhost:1234"

func apiRequest(path string, body string) ([]byte, error) {
	var kodiReq *http.Request
	var err error
	if body != "" {
		kodiReq, err = http.NewRequest("POST", apiURL+path, bytes.NewBuffer([]byte(body)))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	} else {
		kodiReq, err = http.NewRequest("GET", apiURL+path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	}

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

	log.Debug().Msgf("response body %s: %s", path, string(respBody))

	return respBody, nil
}

func apiEmuKill() error {
	_, err := apiRequest("/emukill", "")
	return err
}

func apiLaunch(path string) error {
	_, err := apiRequest("/launch", path)
	return err
}

func apiNotify(msg string) error {
	_, err := apiRequest("/notify", msg)
	return err
}

const noGameRunning = "{\"msg\":\"NO GAME RUNNING\"}"

type APIRunningGameResponse struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	SystemName  string `json:"systemName"`
	Desc        string `json:"desc"`
	Image       string `json:"image"`
	Video       string `json:"video"`
	Marquee     string `json:"marquee"`
	Thumbnail   string `json:"thumbnail"`
	Rating      string `json:"rating"`
	ReleaseDate string `json:"releaseDate"`
	Developer   string `json:"developer"`
	Genre       string `json:"genre"`
	Genres      string `json:"genres"`
	Players     string `json:"players"`
	Favorite    string `json:"favorite"`
	KidGame     string `json:"kidgame"`
	LastPlayed  string `json:"lastplayed"`
	CRC32       string `json:"crc32"`
	MD5         string `json:"md5"`
	GameTime    string `json:"gametime"`
	Lang        string `json:"lang"`
	CheevosHash string `json:"cheevosHash"`
}

func apiRunningGame() (APIRunningGameResponse, bool, error) {
	resp, err := apiRequest("/runningGame", "")
	if err != nil {
		return APIRunningGameResponse{}, false, err
	}

	if string(resp) == noGameRunning {
		return APIRunningGameResponse{}, false, nil
	}

	var game APIRunningGameResponse
	err = json.Unmarshal(resp, &game)
	if err != nil {
		return APIRunningGameResponse{}, false, err
	}

	return game, true, nil
}
