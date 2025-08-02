package zapscript

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func cmdHTTPGet(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	} else if env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	url := env.Cmd.Args[0]

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log.Error().Err(err).Msgf("creating request for url: %s", url)
			return
		}
		
		resp, err := http.DefaultClient.Do(req)
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

func cmdHTTPPost(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 3 {
		return platforms.CmdResult{}, ErrArgCount
	}

	url := env.Cmd.Args[0]
	mime := env.Cmd.Args[1]
	payload := env.Cmd.Args[2]

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
		if err != nil {
			log.Error().Err(err).Msgf("creating request for url: %s", url)
			return
		}
		req.Header.Set("Content-Type", mime)
		
		resp, err := http.DefaultClient.Do(req)
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
