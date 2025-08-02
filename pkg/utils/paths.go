package utils

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
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

	// check for data dir media folder
	inDataDir := false
	if l.SystemID != "" {
		zaparooMedia := filepath.Join(DataDir(pl), config.MediaDir, l.SystemID)
		zaparooMedia = strings.ToLower(zaparooMedia)
		if strings.HasPrefix(lp, zaparooMedia) {
			inDataDir = true
		}
	}

	// check root folder if it's not a generic launcher
	if !inDataDir && len(l.Folders) > 0 {
		inRoot := false
		isAbs := false

		for _, root := range pl.RootDirs(cfg) {
			for _, folder := range l.Folders {
				if strings.HasPrefix(lp, strings.ToLower(filepath.Join(root, folder))) {
					inRoot = true
					break
				}
			}
		}

		if !inRoot {
			for _, folder := range l.Folders {
				if filepath.IsAbs(folder) && strings.HasPrefix(lp, strings.ToLower(folder)) {
					isAbs = true
					break
				}
			}
		}

		if !inRoot && !isAbs {
			return false
		}
	}

	// check file extension
	for _, ext := range l.Extensions {
		if strings.HasSuffix(lp, strings.ToLower(ext)) {
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
	for _, l := range pl.Launchers(cfg) {
		if l.SystemID == systemId {
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
	for _, l := range pl.Launchers(cfg) {
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

			results = append(results, platforms.ScanResult{
				Path: "steam://" + appState["appid"].(string) + "/" + appState["name"].(string),
				Name: appState["name"].(string),
			})
		}
	}

	return results, nil
}

type PathInfo struct {
	Path      string
	Base      string
	Filename  string
	Extension string
	Name      string
}

func GetPathInfo(path string) PathInfo {
	var info PathInfo
	info.Path = path
	info.Base = filepath.Base(path)
	info.Filename = filepath.Base(path)
	info.Extension = filepath.Ext(path)
	info.Name = strings.TrimSuffix(info.Filename, info.Extension)
	return info
}

// FindLauncher takes a path and tries to find the best possible match for a
// launcher, taking into account any allowlist restrictions. Returns the
// launcher to be used.
func FindLauncher(
	cfg *config.Instance,
	pl platforms.Platform,
	path string,
) (platforms.Launcher, error) {
	launchers := PathToLaunchers(cfg, pl, path)
	if len(launchers) == 0 {
		return platforms.Launcher{}, errors.New("no launcher found for: " + path)
	}

	// TODO: must be some better logic to picking this!
	launcher := launchers[0]

	if launcher.AllowListOnly && !cfg.IsLauncherFileAllowed(path) {
		return platforms.Launcher{}, errors.New("file not allowed: " + path)
	}

	return launcher, nil
}

// DoLaunch launches the given path and updates the active media with it if
// it was successful.
func DoLaunch(
	cfg *config.Instance,
	pl platforms.Platform,
	setActiveMedia func(*models.ActiveMedia),
	launcher platforms.Launcher,
	path string,
) error {
	log.Debug().Msgf("launching with: %v", launcher)

	err := launcher.Launch(cfg, path)
	if err != nil {
		return err
	}

	systemMeta, err := assets.GetSystemMetadata(launcher.SystemID)
	if err != nil {
		log.Warn().Err(err).Msgf("no system metadata for: %s", launcher.SystemID)
	}

	setActiveMedia(&models.ActiveMedia{
		LauncherID: launcher.ID,
		SystemID:   launcher.SystemID,
		SystemName: systemMeta.Name,
		Name:       GetPathInfo(path).Name,
		Path:       pl.NormalizePath(cfg, path),
	})

	return nil
}

// HasUserDir checks if a "user" directory exists next to the Zaparoo binary
// and returns true and the absolute path to it. This directory is used as a
// parent for all platform directories if it exists, for a portable install.
func HasUserDir() (string, bool) {
	exeDir := ""
	envExe := os.Getenv(config.AppEnv)
	var err error

	if envExe != "" {
		exeDir = envExe
	} else {
		exeDir, err = os.Executable()
		if err != nil {
			return "", false
		}
	}

	parent := filepath.Dir(exeDir)
	userDir := filepath.Join(parent, config.UserDir)

	if info, err := os.Stat(userDir); err == nil {
		if !info.IsDir() {
			return "", false
		} else {
			return userDir, true
		}
	} else {
		return "", false
	}
}

func ConfigDir(pl platforms.Platform) string {
	if v, ok := HasUserDir(); ok {
		return v
	} else {
		return pl.Settings().ConfigDir
	}
}

func DataDir(pl platforms.Platform) string {
	if v, ok := HasUserDir(); ok {
		return v
	} else {
		return pl.Settings().DataDir
	}
}

var ReURI = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://(.+)$`)
