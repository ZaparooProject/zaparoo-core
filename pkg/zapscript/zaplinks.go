package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/installer"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	zapScriptModels "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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

var zapFetchTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   1 * time.Second,
		KeepAlive: 10 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   1 * time.Second,
	ResponseHeaderTimeout: 1 * time.Second,
	ExpectContinueTimeout: 500 * time.Millisecond,
}

var zapFetchClient = &http.Client{
	Transport: &installer.AuthTransport{
		Base: zapFetchTransport,
	},
	Timeout: 2 * time.Second,
}

func queryZapLinkSupport(u *url.URL) (int, error) {
	baseURL := u.Scheme + "://" + u.Host
	wellKnownURL := baseURL + WellKnownPath
	log.Debug().Msgf("querying zap link support at %s", wellKnownURL)

	req, err := http.NewRequest("GET", wellKnownURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		return 0, errors.New("invalid status code")
	}

	var wellKnown WellKnown
	err = json.NewDecoder(resp.Body).Decode(&wellKnown)
	if err != nil {
		return 0, err
	}

	log.Debug().Msgf("zap link well known result for %s: %v", wellKnownURL, wellKnown)
	return wellKnown.ZapScript, nil
}

func isZapLink(link string, db *database.Database) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	if !(strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		return false
	}

	supported, ok, err := db.UserDB.GetZapLinkHost(u.Host)
	if err != nil {
		log.Error().Err(err).Msgf("error checking db for zap link support: %s", link)
		return false
	}
	if !ok {
		result, err := queryZapLinkSupport(u)
		if err != nil {
			log.Debug().Err(err).Msgf("error querying zap link support: %s", link)
			err := db.UserDB.UpdateZapLinkHost(u.Host, result)
			if err != nil {
				log.Error().Err(err).Msgf("error updating zap link support: %s", link)
			}
			return false
		}
		err = db.UserDB.UpdateZapLinkHost(u.Host, result)
		if err != nil {
			log.Error().Err(err).Msgf("error updating zap link support: %s", link)
		}
		supported = result > 0
	}

	if !supported {
		return false
	}

	return true
}

func getRemoteZapScript(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return nil, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return nil, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %w", err)
	}

	if content != MIMEZaparooZapScript {
		return nil, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	return body, nil
}

func checkZapLink(
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	cmd parser.Command,
) (string, error) {
	if len(cmd.Args) == 0 {
		return "", errors.New("no args")
	}
	value := cmd.Args[0]

	if !isZapLink(value, db) {
		return "", nil
	}

	log.Info().Msgf("checking zap link: %s", value)
	body, err := getRemoteZapScript(value)
	if err != nil {
		return "", err
	}

	if !utils.MaybeJSON(body) {
		return string(body), nil
	}

	var zl zapScriptModels.ZapScript
	err = json.Unmarshal(body, &zl)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling zap link body: %w", err)
	}

	if zl.ZapScript != 1 {
		return "", errors.New("invalid zapscript version")
	}

	if len(zl.Cmds) == 0 {
		return "", errors.New("no commands")
	} else if len(zl.Cmds) > 1 {
		log.Warn().Msgf("multiple commands in json link, using first: %v", zl.Cmds[0])
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
