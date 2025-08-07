package games

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/config"
)

func GetGamesFolders(cfg *config.UserConfig) []string {
	var folders []string
	for _, folder := range cfg.Systems.GamesFolder {
		folder = filepath.Clean(folder)
		if !strings.HasSuffix(folder, "/games") {
			folders = append(folders, filepath.Join(folder, "games"))
		}
		folders = append(folders, folder)
	}
	folders = append(folders, config.GamesFolders...)
	return folders
}

// FolderToSystems returns what systems a path could be for.
func FolderToSystems(cfg *config.UserConfig, path string) []System {
	path = strings.ToLower(path)
	validGamesFolder := false

	for _, folder := range GetGamesFolders(cfg) {
		if strings.HasPrefix(path, strings.ToLower(folder)) {
			validGamesFolder = true
			break
		}
	}

	if !validGamesFolder {
		return nil
	}

	// Since System.Folder was removed, match systems by file extension only
	var validSystems []System
	for _, system := range Systems {
		if MatchSystemFile(system, path) {
			validSystems = append(validSystems, system)
		}
	}

	if strings.HasSuffix(path, "/") {
		return validSystems
	}

	var matchedExtensions []System
	for _, system := range validSystems {
		if MatchSystemFile(system, path) {
			matchedExtensions = append(matchedExtensions, system)
		}
	}

	if len(matchedExtensions) == 0 {
		// fall back to just the folder match
		return validSystems
	}

	return matchedExtensions
}

func BestSystemMatch(cfg *config.UserConfig, path string) (System, error) {
	systems := FolderToSystems(cfg, path)

	if len(systems) == 0 {
		return System{}, fmt.Errorf("no systems found for %s", path)
	}

	if len(systems) == 1 {
		return systems[0], nil
	}

	// check for system matches by file extension if possible
	if filepath.Ext(path) != "" {
		filtered := []System{}
		for _, system := range systems {
			if MatchSystemFile(system, path) {
				filtered = append(filtered, system)
			}
		}

		if len(filtered) > 0 {
			systems = filtered
		}
	}

	// prefer the system with a setname
	for _, system := range systems {
		if system.SetName != "" {
			return system, nil
		}
	}

	// otherwise just return the first one
	return systems[0], nil
}

type PathResult struct {
	System System
	Path   string
}

func GetPopulatedGamesFolders(cfg *config.UserConfig, systems []System) map[string][]string {
	results := GetSystemPaths(cfg, systems)
	if len(results) == 0 {
		return nil
	}

	populated := make(map[string][]string)

	for _, folder := range results {
		files, err := os.ReadDir(folder.Path)

		if err != nil {
			continue
		}

		if len(files) > 0 {
			populated[folder.System.ID] = append(populated[folder.System.ID], folder.Path)
		}
	}

	return populated
}
