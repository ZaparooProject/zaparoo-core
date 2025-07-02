package installer

import (
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type mediaNames struct {
	display  string
	filename string
	ext      string
}

func namesFromURL(rawURL string, defaultName string) mediaNames {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		file := filepath.Base(rawURL)
		ext := filepath.Ext(file)
		name := defaultName
		if name == "" {
			name = strings.TrimSuffix(file, ext)
		}
		return mediaNames{
			display:  name,
			filename: file,
			ext:      ext,
		}
	}

	file := path.Base(u.Path)
	decoded, err := url.PathUnescape(file)
	if err != nil {
		decoded = file
	}
	ext := path.Ext(decoded)
	name := defaultName
	if name == "" {
		name = strings.TrimSuffix(decoded, ext)
	}
	return mediaNames{
		display:  name,
		filename: decoded,
		ext:      ext,
	}
}

func showPreNotice(cfg *config.Instance, pl platforms.Platform, text string) error {
	if text != "" {
		hide, delay, err := pl.ShowNotice(cfg, widgetModels.NoticeArgs{
			Text: text,
		})
		if err != nil {
			return fmt.Errorf("error showing pre-notice: %w", err)
		}

		if delay > 0 {
			log.Debug().Msgf("delaying pre-notice: %d", delay)
			time.Sleep(delay)
		}

		err = hide()
		if err != nil {
			return fmt.Errorf("error hiding pre-notice: %w", err)
		}
	}
	return nil
}

func findInstallDir(
	cfg *config.Instance,
	pl platforms.Platform,
	systemID string,
	names mediaNames,
) (string, error) {
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return "", fmt.Errorf("error getting system: %w", err)
	}

	fallbackDir := filepath.Join(utils.DataDir(pl), config.MediaDir, system.ID)

	// TODO: this would be better if it could auto-detect the existing preferred
	//       platform games folder, but there's currently no shared mechanism to
	//       work out the correct root folder for a platform
	// TODO: config override to set explicit fallback/default media folder

	localPath := filepath.Clean(filepath.Join(fallbackDir, names.filename))

	return localPath, nil
}

func HTTPMediaFile(
	cfg *config.Instance,
	pl platforms.Platform,
	fileURL string,
	systemID string,
	preNotice string,
	displayName string,
) (string, error) {
	if fileURL == "" {
		return "", errors.New("media download url is empty")
	}
	if systemID == "" {
		return "", errors.New("media system id is empty")
	}

	names := namesFromURL(fileURL, displayName)

	localPath, err := findInstallDir(cfg, pl, systemID, names)
	if err != nil {
		return "", fmt.Errorf("error finding install dir: %w", err)
	}

	tempPath := localPath + ".part"

	log.Debug().Msgf("media local path: %s", localPath)

	// check if the file already exists
	if _, err := os.Stat(localPath); err == nil {
		err := showPreNotice(cfg, pl, preNotice)
		if err != nil {
			log.Warn().Err(err).Msgf("error showing pre-notice")
		}
		return localPath, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("error checking file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return "", fmt.Errorf("cannot create directories: %w", err)
	}

	// download the file
	log.Info().Msgf("downloading media: %s", fileURL)

	itemDisplay := names.display
	loadingText := fmt.Sprintf("Downloading %s...", itemDisplay)

	hideLoader, err := pl.ShowLoader(cfg, widgetModels.NoticeArgs{
		Text: loadingText,
	})
	if err != nil {
		log.Warn().Err(err).Msgf("error showing loading dialog")
	}

	if _, err := os.Stat(tempPath); err == nil {
		log.Warn().Msgf("removing leftover temp file: %s", tempPath)
		err := os.Remove(tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing temp file: %s", tempPath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = hideLoader()
		return "", fmt.Errorf("error checking temp file: %w", err)
	}

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

	resp, err := client.Get(fileURL)
	if err != nil {
		_ = hideLoader()
		return "", fmt.Errorf("error getting url: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)
	if resp.StatusCode != 200 {
		_ = hideLoader()
		return "", fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	file, err := os.Create(tempPath)
	if err != nil {
		_ = hideLoader()
		return "", fmt.Errorf("error creating file: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		err = file.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing file: %s", tempPath)
		}
		err := os.Remove(tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing partial download: %s", tempPath)
		}
		_ = hideLoader()
		return "", fmt.Errorf("error downloading file: %w", err)
	}

	expected := resp.ContentLength
	if expected > 0 && written != expected {
		err = file.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing file: %s", tempPath)
		}
		err := os.Remove(tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing partial download: %s", tempPath)
		}
		_ = hideLoader()
		return "", fmt.Errorf("download incomplete: expected %d bytes, got %d", expected, written)
	}

	err = file.Close()
	if err != nil {
		_ = hideLoader()
		return "", fmt.Errorf("error closing file: %w", err)
	}

	if err := os.Rename(tempPath, localPath); err != nil {
		err := os.Remove(tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing temp file: %s", tempPath)
		}
		_ = hideLoader()
		return "", fmt.Errorf("error renaming temp file: %w", err)
	}

	err = hideLoader()
	if err != nil {
		log.Warn().Err(err).Msgf("error hiding loading dialog")
	}

	err = showPreNotice(cfg, pl, preNotice)
	if err != nil {
		log.Warn().Err(err).Msgf("error showing pre-notice")
	}

	return localPath, nil
}
