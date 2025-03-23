package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	zapScriptModels "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"io"
	"net/http"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	MIMEZaparooZapLink   = "application/vnd.zaparoo.link" // not in use
	MIMEZaparooZapScript = "application/vnd.zaparoo.zapscript"
)

var AcceptedMimeTypes = []string{
	MIMEZaparooZapLink,
	MIMEZaparooZapScript,
}

func maybeRemoteZapScript(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	} else {
		return false
	}
}

func getRemoteZapScript(url string) (zapScriptModels.ZapScriptCmd, error) {
	// TODO: this should return a list and handle receiving a raw list of
	// 		 zapscript objects
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return zapScriptModels.ZapScriptCmd{}, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zapScriptModels.ZapScriptCmd{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return zapScriptModels.ZapScriptCmd{}, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return zapScriptModels.ZapScriptCmd{}, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return zapScriptModels.ZapScriptCmd{}, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zapScriptModels.ZapScriptCmd{}, fmt.Errorf("error reading body: %w", err)
	}

	if content != MIMEZaparooZapScript {
		return zapScriptModels.ZapScriptCmd{}, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	var zl zapScriptModels.ZapScriptCmd
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
	if !maybeRemoteZapScript(value) {
		return "", nil
	}

	log.Info().Msgf("checking link: %s", value)
	zl, err := getRemoteZapScript(value)
	if err != nil {
		return "", err
	}

	cmd := strings.ToLower(zl.Cmd)

	switch cmd {
	case zapScriptModels.ZapScriptCmdEvaluate:
		var args zapScriptModels.CmdEvaluateArgs
		err = json.Unmarshal(zl.Args, &args)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling evaluate params: %w", err)
		}
		return args.ZapScript, nil
	case zapScriptModels.ZapScriptCmdLaunch:
		var args zapScriptModels.CmdLaunchArgs
		err = json.Unmarshal(zl.Args, &args)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling launch args: %w", err)
		}
		if args.URL != nil && *args.URL != "" {
			return methods.InstallRunMedia(cfg, pl, args)
		} else {
			// TODO: missing stuff like launcher arg
			return args.Path, nil
		}
	case zapScriptModels.ZapScriptCmdUIPicker:
		var cmdArgs zapScriptModels.CmdPicker
		err = json.Unmarshal(zl.Args, &cmdArgs)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling picker args: %w", err)
		}
		pickerArgs := widgetModels.PickerArgs{
			Cmds: cmdArgs.Items,
		}
		if zl.Name != nil {
			pickerArgs.Title = *zl.Name
		}
		err := pl.ShowPicker(cfg, pickerArgs)
		if err != nil {
			return "", fmt.Errorf("error showing picker: %w", err)
		} else {
			// TODO: this results in an error even though it's valid
			return "", nil
		}
	default:
		return "", fmt.Errorf("unknown cmd: %s", cmd)
	}
}
