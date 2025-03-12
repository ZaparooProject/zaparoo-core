package methods

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"golang.org/x/text/unicode/norm"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
)

var (
	ErrMissingParams = errors.New("missing params")
	ErrInvalidParams = errors.New("invalid params")
	ErrNotAllowed    = errors.New("not allowed")
)

var MediaSafeList = []string{
	"https://cdn.zaparoo.com",
	"https://secure.cdn.zaparoo.com",
}

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
	return nil, env.Platform.KillLauncher()
}

func HandleRunLinkAction(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received run link action request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var t tokens.Token

	var params models.ZapLinkAction
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	method := strings.ToLower(params.Method)
	switch method {
	case models.ZapLinkActionZapScript:
		var zsp models.ZapScriptParams
		err = json.Unmarshal(params.Params, &zsp)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling zapscript params: %w", err)
		}
		t.Text = zsp.ZapScript
	case models.ZapLinkActionMedia:
		// TODO: this will timeout on large downloads
		t.Text, err = InstallRunMedia(env.Config, env.Platform, params)
		if err != nil {
			return nil, fmt.Errorf("error installing and running media: %w", err)
		}
	default:
		return "", fmt.Errorf("unknown link action: %s", method)
	}

	t.ScanTime = time.Now()
	t.FromAPI = true

	env.State.SetActiveCard(t)
	env.TokenQueue <- t

	return nil, nil
}

const preNoticeTime = 2 * time.Second

func showNotice(
	cfg *config.Instance,
	pl platforms.Platform,
	text string,
) error {
	argsPath := filepath.Join(pl.TempDir(), "loader.json")
	completePath := filepath.Join(pl.TempDir(), ".loader-complete")

	if mister.MainHasFeature(mister.MainFeatureNotice) {
		err := mister.RunDevCmd("show_notice", text)
		if err != nil {
			return fmt.Errorf("error running dev cmd: %w", err)
		}
	} else {
		// fall back on script
		args := widgetModels.NoticeArgs{
			Text:     text,
			Complete: completePath,
		}
		argsJson, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("error marshalling notice args: %w", err)
		}
		err = os.WriteFile(argsPath, argsJson, 0644)
		if err != nil {
			return fmt.Errorf("error writing notice args: %w", err)
		}
		text := fmt.Sprintf("**mister.script:zaparoo.sh -show-notice %s", argsPath)
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
	}

	return nil
}

func hideNotice(
	pl platforms.Platform,
) error {
	argsPath := filepath.Join(pl.TempDir(), "loader.json")
	completePath := filepath.Join(pl.TempDir(), ".loader-complete")

	if !mister.MainHasFeature(mister.MainFeatureNotice) {
		err := os.Remove(argsPath)
		if err != nil {
			return fmt.Errorf("error removing loader args: %w", err)
		}
		err = os.WriteFile(completePath, []byte{}, 0644)
		if err != nil {
			return fmt.Errorf("error writing loader complete: %w", err)
		}
	}

	return nil
}

func InstallRunMedia(
	cfg *config.Instance,
	pl platforms.Platform,
	action models.ZapLinkAction,
) (string, error) {
	if pl.Id() != "mister" {
		return "", errors.New("media install only supported for mister")
	}

	var mp models.MediaParams
	err := json.Unmarshal(action.Params, &mp)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling media params: %w", err)
	}

	isSafe := false
	if mp.Url != nil {
		log.Debug().Msgf("checking media download url: %s", *mp.Url)

		for _, safe := range MediaSafeList {
			if strings.HasPrefix(*mp.Url, safe) {
				isSafe = true
				break
			}
		}

		if !isSafe {
			return "", errors.New("media download not in safe list")
		}
	}

	if mp.Url == nil {
		return "", errors.New("media download url is empty")
	}

	system, err := systemdefs.GetSystem(mp.System)
	if err != nil {
		return "", fmt.Errorf("error getting system: %w", err)
	}

	var launchers []platforms.Launcher
	for _, l := range pl.Launchers() {
		if l.SystemId == system.Id {
			launchers = append(launchers, l)
		}
	}

	if len(launchers) == 0 {
		return "", fmt.Errorf("no launchers for system: %s", system.Id)
	}

	// just use the first launcher for now
	launcher := launchers[0]

	if launcher.Folders == nil {
		return "", errors.New("no folders for launcher")
	}

	// just use the first folder for now
	folder := launcher.Folders[0]

	name := filepath.Base(*mp.Url)

	// roots := pl.RootDirs(cfg)

	// if len(roots) == 0 {
	// 	return "", errors.New("no root dirs")
	// }

	// root := roots[0]

	root := "/media/fat/games" // TODO: this is hardcoded for now

	path := filepath.Join(root, folder, name)

	log.Debug().Msgf("media path: %s", path)

	// check if the file already exists
	if _, err := os.Stat(path); err == nil {
		if mp.PreNotice != nil && *mp.PreNotice != "" {
			err = showNotice(cfg, pl, *mp.PreNotice)
			if err != nil {
				return "", fmt.Errorf("error showing pre-notice: %w", err)
			}
			time.Sleep(preNoticeTime)
			err = hideNotice(pl)
			if err != nil {
				return "", fmt.Errorf("error hiding pre-notice: %w", err)
			}
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking file: %w", err)
	}

	// download the file
	log.Info().Msgf("downloading media: %s", *mp.Url)

	loadingText := fmt.Sprintf("Downloading %s...", mp.Name)

	// display loading dialog
	err = showNotice(cfg, pl, loadingText)
	if err != nil {
		return "", fmt.Errorf("error showing loading dialog: %w", err)
	}

	resp, err := http.Get(*mp.Url)
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

	file, err := os.Create(path)
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

	err = hideNotice(pl)
	if err != nil {
		return "", fmt.Errorf("error hiding loading dialog: %w", err)
	}

	if mp.PreNotice != nil && *mp.PreNotice != "" {
		err = showNotice(cfg, pl, *mp.PreNotice)
		if err != nil {
			return "", fmt.Errorf("error showing pre-notice: %w", err)
		}
		time.Sleep(preNoticeTime)
		err = hideNotice(pl)
		if err != nil {
			return "", fmt.Errorf("error hiding pre-notice: %w", err)
		}
	}

	return path, nil
}
