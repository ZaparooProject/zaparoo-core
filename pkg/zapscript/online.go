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

func getRemoteZapScript(url string) (zapScriptModels.ZapScript, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return zapScriptModels.ZapScript{}, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zapScriptModels.ZapScript{}, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return zapScriptModels.ZapScript{}, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return zapScriptModels.ZapScript{}, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return zapScriptModels.ZapScript{}, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zapScriptModels.ZapScript{}, fmt.Errorf("error reading body: %w", err)
	}

	if content != MIMEZaparooZapScript {
		return zapScriptModels.ZapScript{}, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	var zl zapScriptModels.ZapScript
	err = json.Unmarshal(body, &zl)
	if err != nil {
		return zl, fmt.Errorf("error unmarshalling body: %w", err)
	}

	if zl.ZapScript != 1 {
		return zl, errors.New("invalid zapscript version")
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

	if len(zl.Cmds) == 0 {
		return "", errors.New("no commands")
	} else if len(zl.Cmds) > 1 {
		log.Warn().Msgf("multiple commands in link, using first: %v", zl.Cmds[0])
	}

	cmd := zl.Cmds[0]
	cmdName := strings.ToLower(cmd.Cmd)

	switch cmdName {
	case zapScriptModels.ZapScriptCmdEvaluate:
		var args zapScriptModels.CmdEvaluateArgs
		err = json.Unmarshal(cmd.Args, &args)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling evaluate params: %w", err)
		}
		return args.ZapScript, nil
	case zapScriptModels.ZapScriptCmdLaunch:
		var args zapScriptModels.CmdLaunchArgs
		err = json.Unmarshal(cmd.Args, &args)
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
		err = json.Unmarshal(cmd.Args, &cmdArgs)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling picker args: %w", err)
		}
		pickerArgs := widgetModels.PickerArgs{
			Items: cmdArgs.Items,
		}
		if cmd.Name != nil {
			pickerArgs.Title = *cmd.Name
		}
		err := pl.ShowPicker(cfg, pickerArgs)
		if err != nil {
			return "", fmt.Errorf("error showing picker: %w", err)
		} else {
			// TODO: this results in an error even though it's valid
			return "", nil
		}
	default:
		return "", fmt.Errorf("unknown cmdName: %s", cmdName)
	}
}
