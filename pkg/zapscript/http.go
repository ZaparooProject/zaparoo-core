package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

const (
	MimeZaparooZapScript = "application/vnd.zaparoo.zapscript"
	MimeZaparooPlaylist  = "application/vnd.zaparoo.playlist"
)

var AcceptedMimeTypes = []string{
	MimeZaparooZapScript,
	MimeZaparooPlaylist,
}

type LinkZapScriptResponse struct {
	Value string `json:"value"`
}

func isLink(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	} else {
		return false
	}
}

func checkLink(value string) (string, error) {
	if !isLink(value) {
		return "", nil
	}

	log.Info().Msgf("checking link: %s", value)

	req, err := http.NewRequest("GET", value, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return "", errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return "", errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return "", errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body: %w", err)
	}

	var zl LinkZapScriptResponse
	err = json.Unmarshal(body, &zl)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling body: %w", err)
	}

	if zl.Value == "" {
		return "", errors.New("link value is empty")
	}

	if content == MimeZaparooZapScript {
		newText := zl.Value
		return newText, nil
	}

	return "", nil
}

func cmdHttpGet(_ platforms.Platform, env platforms.CmdEnv) error {
	go func() {
		resp, err := http.Get(env.Args)
		if err != nil {
			log.Error().Err(err).Msgf("getting url: %s", env.Args)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return nil
}

func cmdHttpPost(pl platforms.Platform, env platforms.CmdEnv) error {
	parts := strings.SplitN(env.Args, ",", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid post format: %s", env.Args)
	}

	url, format, data := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])

	go func() {
		resp, err := http.Post(url, format, strings.NewReader(data))
		if err != nil {
			log.Error().Err(err).Msgf("error posting to url: %s", env.Args)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return nil
}
