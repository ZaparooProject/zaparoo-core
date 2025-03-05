package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
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

func getZapLink(url string) (models.ZapLink, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return models.ZapLink{}, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.ZapLink{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return models.ZapLink{}, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return models.ZapLink{}, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return models.ZapLink{}, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.ZapLink{}, fmt.Errorf("error reading body: %w", err)
	}

	if content != MimeZaparooZapLink {
		return models.ZapLink{}, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	var zl models.ZapLink
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

	if len(zl.Actions) > 1 {
		if pl.Id() != "mister" {
			return "", errors.New("multiple actions is not supported on this platform")
		}

		// forward multiple link actions to picker ui
		argsPath := filepath.Join(pl.TempDir(), "picker.json")
		args := widgetModels.PickerArgs{
			Title: zl.Name,
		}
		args.Actions = zl.Actions

		argsJson, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		err = os.WriteFile(argsPath, argsJson, 0644)
		if err != nil {
			return "", err
		}

		text := fmt.Sprintf("**mister.script:zaparoo.sh -show-picker %s", argsPath)
		apiArgs := models.RunParams{
			Text: &text,
		}
		ps, err := json.Marshal(apiArgs)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}

		_, err = client.LocalClient(cfg, models.MethodRun, string(ps))
		if err != nil {
			log.Error().Err(err).Msg("error running local client")
		}

		return "", nil
	}

	action := zl.Actions[0]
	method := strings.ToLower(action.Method)

	switch method {
	case models.ZapLinkActionZapScript:
		var zsp models.ZapScriptParams
		err = json.Unmarshal(action.Params, &zsp)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling zap script params: %w", err)
		}
		return zsp.ZapScript, nil
	case models.ZapLinkActionMedia:
		return methods.InstallRunMedia(cfg, pl, action)
	default:
		return "", fmt.Errorf("unknown action: %s", action.Method)
	}
}
