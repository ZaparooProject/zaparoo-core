package utils

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"strings"
)

// PathIsLauncher returns true if a given path matches against any of the
// criteria defined in a launcher.
func PathIsLauncher(
	cfg *config.Instance,
	pl platforms.Platform,
	l platforms.Launcher,
	path string,
) bool {
	if len(path) == 0 {
		return false
	}

	lp := strings.ToLower(path)

	// ignore dot files
	if strings.HasPrefix(filepath.Base(lp), ".") {
		return false
	}

	// check uri scheme
	for _, scheme := range l.Schemes {
		if strings.HasPrefix(lp, scheme+":") {
			return true
		}
	}

	// check root folder if it's not a generic launcher
	if len(l.Folders) > 0 {
		inRoot := false
		for _, root := range pl.RootDirs(cfg) {
			for _, folder := range l.Folders {
				if strings.HasPrefix(lp, strings.ToLower(filepath.Join(root, folder))) {
					log.Debug().Msgf("path is in root folder: %s", folder)
					inRoot = true
					break
				}
			}
		}

		if !inRoot {
			return false
		} else if filepath.Ext(lp) == "" {
			return true
		}
	}

	// check file extension
	for _, ext := range l.Extensions {
		if strings.HasSuffix(lp, ext) {
			return true
		}
	}

	// finally, launcher's test func
	if l.Test != nil {
		return l.Test(cfg, lp)
	} else {
		return false
	}
}

// MatchSystemFile returns true if a given path is for a given system.
func MatchSystemFile(
	cfg *config.Instance,
	pl platforms.Platform,
	systemId string,
	path string,
) bool {
	for _, l := range pl.Launchers() {
		if l.SystemId == systemId {
			if PathIsLauncher(cfg, pl, l, path) {
				return true
			}
		}
	}
	return false
}

// PathToLaunchers is a reverse lookup to match a given path against all
// possible launchers in a platform. Returns all matched launchers.
func PathToLaunchers(
	cfg *config.Instance,
	pl platforms.Platform,
	path string,
) []platforms.Launcher {
	var launchers []platforms.Launcher
	for _, l := range pl.Launchers() {
		if PathIsLauncher(cfg, pl, l, path) {
			launchers = append(launchers, l)
		}
	}
	return launchers
}

func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	return filepath.Dir(exe)
}

func ScanSteamApps(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult

	f, err := os.Open(filepath.Join(steamDir, "libraryfolders.vdf"))
	if err != nil {
		log.Error().Err(err).Msg("error opening libraryfolders.vdf")
		return results, nil
	}

	p := vdf.NewParser(f)
	m, err := p.Parse()
	if err != nil {
		log.Error().Err(err).Msg("error parsing libraryfolders.vdf")
		return results, nil
	}

	lfs := m["libraryfolders"].(map[string]interface{})
	for l, v := range lfs {
		log.Debug().Msgf("library id: %s", l)
		ls := v.(map[string]interface{})

		libraryPath := ls["path"].(string)
		steamApps, err := os.ReadDir(filepath.Join(libraryPath, "steamapps"))
		if err != nil {
			log.Error().Err(err).Msg("error listing steamapps folder")
			continue
		}

		var manifestFiles []string
		for _, mf := range steamApps {
			if strings.HasPrefix(mf.Name(), "appmanifest_") {
				manifestFiles = append(manifestFiles, filepath.Join(libraryPath, "steamapps", mf.Name()))
			}
		}

		for _, mf := range manifestFiles {
			log.Debug().Msgf("manifest file: %s", mf)

			af, err := os.Open(mf)
			if err != nil {
				log.Error().Err(err).Msgf("error opening manifest: %s", mf)
				return results, nil
			}

			ap := vdf.NewParser(af)
			am, err := ap.Parse()
			if err != nil {
				log.Error().Err(err).Msgf("error parsing manifest: %s", mf)
				return results, nil
			}

			appState := am["AppState"].(map[string]interface{})
			log.Debug().Msgf("app name: %v", appState["name"])

			results = append(results, platforms.ScanResult{
				Path: "steam://" + appState["appid"].(string),
				Name: appState["name"].(string),
			})
		}
	}

	return results, nil
}
