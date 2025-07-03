package installer

import (
	"encoding/base64"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

type AuthTransport struct {
	Base http.RoundTripper
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Base == nil {
		t.Base = http.DefaultTransport
	}

	creds := config.LookupAuth(config.GetAuthCfg(), req.URL.String())
	if creds != nil {
		if creds.Bearer != "" {
			req.Header.Set("Authorization", "Bearer "+creds.Bearer)
		} else if creds.Username != "" {
			user := creds.Username
			pass := creds.Password
			auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			req.Header.Set("Authorization", "Basic "+auth)
		}
	}

	return t.Base.RoundTrip(req)
}

var timeoutTr = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ResponseHeaderTimeout: 30 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
}

var httpClient = &http.Client{
	Transport: &AuthTransport{
		Base: timeoutTr,
	},
}

func DownloadHTTPFile(opts DownloaderArgs) error {
	resp, err := httpClient.Get(opts.url)
	if err != nil {
		return fmt.Errorf("error getting url: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	file, err := os.Create(opts.tempPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		err = file.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing file: %s", opts.tempPath)
		}
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing partial download: %s", opts.tempPath)
		}
		return fmt.Errorf("error downloading file: %w", err)
	}

	expected := resp.ContentLength
	if expected > 0 && written != expected {
		err = file.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing file: %s", opts.tempPath)
		}
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing partial download: %s", opts.tempPath)
		}
		return fmt.Errorf("download incomplete: expected %d bytes, got %d", expected, written)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	if err := os.Rename(opts.tempPath, opts.finalPath); err != nil {
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing temp file: %s", opts.tempPath)
		}
		return fmt.Errorf("error renaming temp file: %w", err)
	}

	return nil
}
