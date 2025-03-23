package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"io"
	"net/http"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	MimeZaparooZapLink = "application/vnd.zaparoo.link"
)

var AcceptedMimeTypes = []string{
	MimeZaparooZapLink,
}

func maybeZapLink(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	} else {
		return false
	}
}

func getZapLink(url string) (api.ZapLink, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.ZapLink{}, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return api.ZapLink{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return api.ZapLink{}, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return api.ZapLink{}, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return api.ZapLink{}, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.ZapLink{}, fmt.Errorf("error reading body: %w", err)
	}

	if content != MimeZaparooZapLink {
		return api.ZapLink{}, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	var zl api.ZapLink
	err = json.Unmarshal(body, &zl)
	if err != nil {
		return zl, fmt.Errorf("error unmarshalling body: %w", err)
	}

	return zl, nil
}

func checkLink(
	cfg *config.Instance,
	pl platforms.Platform,
	value string,
) (string, error) {
	if !maybeZapLink(value) {
		return "", nil
	}

	log.Info().Msgf("checking link: %s", value)
	zl, err := getZapLink(value)
	if err != nil {
		return "", err
	}

	if len(zl.Actions) == 0 {
		return "", errors.New("no actions in zap link")
	}

	// multiple actions get forward to a picker menu
	if len(zl.Actions) > 1 {
		err := pl.ShowPicker(cfg, widgetModels.PickerArgs{
			Title:   zl.Name,
			Actions: zl.Actions,
		})
		if err != nil {
			return "", fmt.Errorf("error showing picker: %w", err)
		} else {
			return "", nil
		}
	}

	action := zl.Actions[0]
	method := strings.ToLower(action.Method)

	switch method {
	case api.ZapLinkActionZapScript:
		var zsp api.ZapScriptParams
		err = json.Unmarshal(action.Params, &zsp)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling zap script params: %w", err)
		}
		return zsp.ZapScript, nil
	case api.ZapLinkActionMedia:
		return methods.InstallRunMedia(cfg, pl, action)
	default:
		return "", fmt.Errorf("unknown action: %s", action.Method)
	}
}
