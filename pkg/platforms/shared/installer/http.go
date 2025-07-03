package installer

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func DownloadHTTPFile(opts DownloaderArgs) error {
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
	}

	client := &http.Client{
		Transport: tr,
	}

	resp, err := client.Get(opts.url)
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
