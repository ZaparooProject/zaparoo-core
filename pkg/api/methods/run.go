package methods

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	zapScriptModels "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"golang.org/x/text/unicode/norm"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

var (
	ErrMissingParams = errors.New("missing params")
	ErrInvalidParams = errors.New("invalid params")
	ErrNotAllowed    = errors.New("not allowed")
)

func HandleRun(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received run request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var t tokens.Token

	var params models.RunParams
	err := json.Unmarshal(env.Params, &params)
	if err == nil {
		log.Debug().Msgf("unmarshalled run params: %+v", params)

		if params.Type != nil {
			t.Type = *params.Type
		}

		hasArg := false

		if params.UID != nil {
			t.UID = *params.UID
			hasArg = true
		}

		if params.Text != nil {
			t.Text = norm.NFC.String(*params.Text)
			hasArg = true
		}

		if params.Data != nil {
			t.Data = strings.ToLower(*params.Data)
			t.Data = strings.ReplaceAll(t.Data, " ", "")

			if _, err := hex.DecodeString(t.Data); err != nil {
				return nil, ErrInvalidParams
			}

			hasArg = true
		}

		if !hasArg {
			return nil, ErrInvalidParams
		}

		if params.Unsafe {
			t.Unsafe = true
		}
	} else {
		log.Debug().Msgf("could not unmarshal run params, trying string: %s", env.Params)

		var text string
		err := json.Unmarshal(env.Params, &text)
		if err != nil {
			return nil, ErrInvalidParams
		}

		if text == "" {
			return nil, ErrMissingParams
		}

		t.Text = norm.NFC.String(text)
	}

	t.ScanTime = time.Now()
	t.FromAPI = true

	// TODO: how do we report back errors? put channel in queue
	env.State.SetActiveCard(t)
	env.TokenQueue <- t

	return nil, nil
}

func HandleRunScript(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received run script request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var t tokens.Token

	var zsrp models.RunScriptParams
	err := json.Unmarshal(env.Params, &zsrp)
	if err != nil {
		log.Error().Msgf("error unmarshalling run zapscript: %s", err)
		return nil, ErrInvalidParams
	}

	if zsrp.Unsafe {
		t.Unsafe = true
	}

	zs := zapScriptModels.ZapScript{
		ZapScript: zsrp.ZapScript,
		Name:      zsrp.Name,
		Cmds:      zsrp.Cmds,
	}

	if zs.ZapScript != 1 {
		log.Error().Msgf("invalid zapscript version: %d", zs.ZapScript)
		return nil, ErrInvalidParams
	}

	if len(zs.Cmds) == 0 {
		log.Error().Msg("no commands in zapscript")
		return nil, ErrInvalidParams
	} else if len(zs.Cmds) > 1 {
		log.Warn().Msg("too many commands in zapscript, using first")
	}

	cmd := zs.Cmds[0]

	cmdName := strings.ToLower(cmd.Cmd)
	switch cmdName {
	case zapScriptModels.ZapScriptCmdEvaluate:
		var args zapScriptModels.CmdEvaluateArgs
		err = json.Unmarshal(cmd.Args, &args)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling evaluate params: %w", err)
		}
		t.Text = args.ZapScript
	case zapScriptModels.ZapScriptCmdLaunch:
		var args zapScriptModels.CmdLaunchArgs
		err = json.Unmarshal(cmd.Args, &args)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling evaluate params: %w", err)
		}
		// TODO: this will timeout on large downloads
		t.Text, err = InstallRunMedia(env.Config, env.Platform, args)
		if err != nil {
			return nil, fmt.Errorf("error installing and running media: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported cmd: %s", cmdName)
	}

	t.ScanTime = time.Now()
	t.FromAPI = true

	env.State.SetActiveCard(t)
	env.TokenQueue <- t

	return nil, nil
}

func HandleRunRest(
	cfg *config.Instance,
	st *state.State,
	itq chan<- tokens.Token,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info().Msg("received REST run request")

		text := chi.URLParam(r, "*")
		text, err := url.QueryUnescape(text)
		if err != nil {
			log.Error().Msgf("error decoding request: %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if !cfg.IsRunAllowed(text) {
			log.Error().Msgf("run not allowed: %s", text)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		log.Info().Msgf("running token: %s", text)

		t := tokens.Token{
			Text:     norm.NFC.String(text),
			ScanTime: time.Now(),
			FromAPI:  true,
		}

		st.SetActiveCard(t)
		itq <- t
	}
}

func HandleStop(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received stop request")
	return nil, env.Platform.StopActiveLauncher()
}

func displayNameFromURL(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || u.Path == "" {
		return rawurl
	}
	file := path.Base(u.Path)
	decoded, err := url.PathUnescape(file)
	if err != nil {
		decoded = file
	}
	ext := path.Ext(decoded)
	name := strings.TrimSuffix(decoded, ext)
	return name
}

func InstallRunMedia(
	cfg *config.Instance,
	pl platforms.Platform,
	launchArgs zapScriptModels.CmdLaunchArgs,
) (string, error) {
	if pl.ID() != platforms.PlatformIDMister {
		return "", errors.New("media install only supported for mister")
	}

	if launchArgs.URL == nil {
		return "", errors.New("media download url is empty")
	} else if launchArgs.System == nil {
		return "", errors.New("media system is empty")
	}

	system, err := systemdefs.LookupSystem(*launchArgs.System)
	if err != nil {
		return "", fmt.Errorf("error getting system: %w", err)
	}

	var launchers []platforms.Launcher
	for _, l := range pl.Launchers(cfg) {
		if l.SystemID == system.ID {
			launchers = append(launchers, l)
		}
	}

	if len(launchers) == 0 {
		return "", fmt.Errorf("no launchers for system: %s", system.ID)
	}

	// just use the first launcher for now
	launcher := launchers[0]

	if launcher.Folders == nil {
		return "", errors.New("no folders for launcher")
	}

	// just use the first folder for now
	folder := launcher.Folders[0]

	name := filepath.Base(*launchArgs.URL)

	// roots := pl.RootDirs(cfg)

	// if len(roots) == 0 {
	// 	return "", errors.New("no root dirs")
	// }

	// root := roots[0]

	root := "/media/fat/games" // TODO: this is hardcoded for now

	localPath := filepath.Join(root, folder, name)

	log.Debug().Msgf("media localPath: %s", localPath)

	// check if the file already exists
	if _, err := os.Stat(localPath); err == nil {
		if launchArgs.PreNotice != nil && *launchArgs.PreNotice != "" {
			hide, delay, err := pl.ShowNotice(cfg, widgetModels.NoticeArgs{
				Text: *launchArgs.PreNotice,
			})
			if err != nil {
				return "", fmt.Errorf("error showing pre-notice: %w", err)
			}

			if delay > 0 {
				log.Debug().Msgf("delaying pre-notice: %d", delay)
				time.Sleep(delay)
			}

			err = hide()
			if err != nil {
				return "", fmt.Errorf("error hiding pre-notice: %w", err)
			}
		}
		return localPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking file: %w", err)
	}

	// download the file
	log.Info().Msgf("downloading media: %s", *launchArgs.URL)

	itemDisplay := displayNameFromURL(*launchArgs.URL)
	if launchArgs.Name != nil && *launchArgs.Name != "" {
		itemDisplay = *launchArgs.Name
	}
	loadingText := fmt.Sprintf("Downloading %s...", itemDisplay)

	hideLoader, err := pl.ShowLoader(cfg, widgetModels.NoticeArgs{
		Text: loadingText,
	})
	if err != nil {
		return "", fmt.Errorf("error showing loading dialog: %w", err)
	}

	resp, err := http.Get(*launchArgs.URL)
	if err != nil {
		return "", fmt.Errorf("error getting url: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	file, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("error creating file: %w", err)
	}
	defer func(File *os.File) {
		err := File.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing file")
		}
	}(file)

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", fmt.Errorf("error copying file: %w", err)
	}

	err = hideLoader()
	if err != nil {
		return "", fmt.Errorf("error hiding loading dialog: %w", err)
	}

	if launchArgs.PreNotice != nil && *launchArgs.PreNotice != "" {
		hide, delay, err := pl.ShowNotice(cfg, widgetModels.NoticeArgs{
			Text: *launchArgs.PreNotice,
		})
		if err != nil {
			return "", fmt.Errorf("error showing pre-notice: %w", err)
		}

		if delay > 0 {
			log.Debug().Msgf("delaying pre-notice: %d", delay)
			time.Sleep(delay)
		}

		err = hide()
		if err != nil {
			return "", fmt.Errorf("error hiding pre-notice: %w", err)
		}
	}

	return localPath, nil
}
