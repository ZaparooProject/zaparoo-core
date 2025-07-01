package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	zapScriptModels "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	MIMEZaparooZapScript = "application/vnd.zaparoo.zapscript"
	WellKnownPath        = "/.well-known/zaparoo"
)

var AcceptedMimeTypes = []string{
	MIMEZaparooZapScript,
}

type WellKnown struct {
	ZapScript int `json:"zapscript"`
}

var zapLinkHost sync.Map

func queryZapLinkSupport(u *url.URL) (bool, error) {
	baseURL := u.Scheme + "://" + u.Host
	wellKnownURL := baseURL + WellKnownPath
	log.Debug().Msgf("querying zap link support at %s", wellKnownURL)

	resp, err := http.Get(wellKnownURL)
	if err != nil {
		return false, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		return false, errors.New("invalid status code")
	}

	var wellKnown WellKnown
	err = json.NewDecoder(resp.Body).Decode(&wellKnown)
	if err != nil {
		return false, err
	}

	log.Debug().Msgf("zap link well known result for %s: %v", wellKnownURL, wellKnown)
	return wellKnown.ZapScript == 1, nil
}

func isZapLink(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	if !(strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		return false
	}

	supported, ok := zapLinkHost.Load(u.Host)
	if !ok {
		result, err := queryZapLinkSupport(u)
		if err != nil {
			log.Debug().Err(err).Msgf("error querying zap link support: %s", link)
			zapLinkHost.Store(u.Host, false)
			return false
		}
		zapLinkHost.Store(u.Host, result)
		supported = result
	}

	if !supported.(bool) {
		return false
	}

	return true
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

func checkZapLink(
	cfg *config.Instance,
	pl platforms.Platform,
	cmd parser.Command,
) (string, error) {
	if len(cmd.Args) == 0 {
		return "", errors.New("no args")
	}
	value := cmd.Args[0]

	if !isZapLink(value) {
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

	newCmd := zl.Cmds[0]
	cmdName := strings.ToLower(newCmd.Cmd)

	switch cmdName {
	case zapScriptModels.ZapScriptCmdEvaluate:
		var args zapScriptModels.CmdEvaluateArgs
		err = json.Unmarshal(newCmd.Args, &args)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling evaluate params: %w", err)
		}
		return args.ZapScript, nil
	case zapScriptModels.ZapScriptCmdLaunch:
		var args zapScriptModels.CmdLaunchArgs
		err = json.Unmarshal(newCmd.Args, &args)
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
		err = json.Unmarshal(newCmd.Args, &cmdArgs)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling picker args: %w", err)
		}
		pickerArgs := widgetModels.PickerArgs{
			Items:  cmdArgs.Items,
			Unsafe: true,
		}
		if newCmd.Name != nil {
			pickerArgs.Title = *newCmd.Name
		}
		err := pl.ShowPicker(cfg, pickerArgs)
		if err != nil {
			return "", fmt.Errorf("error showing picker: %w", err)
		} else {
			return "", nil
		}
	default:
		return "", fmt.Errorf("unknown cmdName: %s", cmdName)
	}
}
