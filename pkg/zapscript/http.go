package zapscript

import (
	"net/http"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func cmdHttpGet(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	} else if env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	url := env.Cmd.Args[0]

	go func() {
		resp, err := http.Get(url)
		if err != nil {
			log.Error().Err(err).Msgf("getting url: %s", url)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return platforms.CmdResult{}, nil
}

func cmdHttpPost(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 3 {
		return platforms.CmdResult{}, ErrArgCount
	}

	url := env.Cmd.Args[0]
	mime := env.Cmd.Args[1]
	payload := env.Cmd.Args[2]

	go func() {
		resp, err := http.Post(url, mime, strings.NewReader(payload))
		if err != nil {
			log.Error().Err(err).Msgf("error posting to url: %s", url)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return platforms.CmdResult{}, nil
}
