package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	MimeZaparooZapLink = "application/vnd.zaparoo.link"
)

var AcceptedMimeTypes = []string{
	MimeZaparooZapLink,
}

var MediaSafeList = []string{
	"https://cdn.zaparoo.com",
}

const (
	ZapLinkActionZapScript = "ZAPSCRIPT"
	ZapLinkActionMedia     = "MEDIA"
)

type ZapLinkAction struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
}

type ZapLink struct {
	Version int             `json:"version"`
	Actions []ZapLinkAction `json:"actions"`
}

type ZapScriptParams struct {
	ZapScript string `json:"zapscript"`
}

type MediaParams struct {
	Name   string  `json:"name"`
	System string  `json:"system"`
	Url    *string `json:"url"`
}

func isLink(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	} else {
		return false
	}
}

func checkLink(_ *config.Instance, pl platforms.Platform, value string) (string, error) {
	if !isLink(value) {
		return "", nil
	}

	log.Info().Msgf("checking link: %s", value)

	req, err := http.NewRequest("GET", value, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return "", errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return "", errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return "", errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body: %w", err)
	}

	if content != MimeZaparooZapLink {
		return "", errors.New("invalid content type")
	}

	var zl ZapLink
	err = json.Unmarshal(body, &zl)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling body: %w", err)
	}

	if len(zl.Actions) == 0 {
		return "", errors.New("no actions in zap link")
	}

	// just process the first action for now
	action := zl.Actions[0]

	if strings.EqualFold(action.Action, ZapLinkActionZapScript) {
		var zsp ZapScriptParams
		err = json.Unmarshal(action.Params, &zsp)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling zap script params: %w", err)
		}
		return zsp.ZapScript, nil
	} else if strings.EqualFold(action.Action, ZapLinkActionMedia) {
		var mp MediaParams
		err = json.Unmarshal(action.Params, &mp)
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

		system, err := gamesdb.GetSystem(mp.System)
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
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("error checking file: %w", err)
		}

		// download the file
		log.Info().Msgf("downloading media: %s", *mp.Url)

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

		return path, nil
	} else {
		return "", fmt.Errorf("unknown action: %s", action.Action)
	}
}
