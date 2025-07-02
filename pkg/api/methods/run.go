package methods

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

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
	default:
		return "", fmt.Errorf("unsupported cmd: %s", cmdName)
	}

	t.ScanTime = time.Now()
	t.FromAPI = true

	env.State.SetActiveCard(t)
	env.TokenQueue <- t

	return nil, nil
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}

func HandleRunRest(
	cfg *config.Instance,
	st *state.State,
	itq chan<- tokens.Token,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info().Msg("received REST run request")

		text := chi.URLParam(r, "*")

		if !isLocalRequest(r) && !cfg.IsRunAllowed(text) {
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
