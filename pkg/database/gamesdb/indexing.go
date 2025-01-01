package gamesdb

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

type PathResult struct {
	System System
	Path   string
}

// FindPath case-insensitively finds a file/folder at a path.
func FindPath(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)

	files, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		target := file.Name()

		if len(target) != len(name) {
			continue
		} else if strings.EqualFold(target, name) {
			return filepath.Join(parent, target), nil
		}
	}

	return "", fmt.Errorf("file match not found: %s", path)
}

func GetSystemPaths(pl platforms.Platform, rootFolders []string, systems []System) []PathResult {
	var matches []PathResult

	for _, system := range systems {
		var launchers []platforms.Launcher
		for _, l := range pl.Launchers() {
			if l.SystemId == system.Id {
				launchers = append(launchers, l)
			}
		}

		var folders []string
		for _, l := range launchers {
			for _, folder := range l.Folders {
				if !utils.Contains(folders, folder) {
					folders = append(folders, folder)
				}
			}
		}

		for _, folder := range rootFolders {
			gf, err := FindPath(folder)
			if err != nil {
				continue
			}

			for _, folder := range folders {
				systemFolder := filepath.Join(gf, folder)
				path, err := FindPath(systemFolder)
				if err != nil {
					continue
				}

				matches = append(matches, PathResult{system, path})
			}
		}
	}

	return matches
}

type resultsStack [][]string

func (r *resultsStack) new() {
	*r = append(*r, []string{})
}

func (r *resultsStack) pop() {
	if len(*r) == 0 {
		return
	}
	*r = (*r)[:len(*r)-1]
}

func (r *resultsStack) get() (*[]string, error) {
	if len(*r) == 0 {
		return nil, fmt.Errorf("nothing on stack")
	}
	return &(*r)[len(*r)-1], nil
}

// GetFiles searches for all valid games in a given path and return a list of
// files. This function deep searches .zip files and handles symlinks at all
// levels.
func GetFiles(
	cfg *config.Instance,
	platform platforms.Platform,
	systemId string,
	path string,
) ([]string, error) {
	var allResults []string
	var stack resultsStack
	visited := make(map[string]struct{})

	system, err := GetSystem(systemId)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var scanner func(path string, file fs.DirEntry, err error) error
	scanner = func(path string, file fs.DirEntry, _ error) error {
		// avoid recursive symlinks
		if file.IsDir() {
			if _, ok := visited[path]; ok {
				return filepath.SkipDir
			} else {
				visited[path] = struct{}{}
			}
		}

		// handle symlinked directories
		if file.Type()&os.ModeSymlink != 0 {
			err = os.Chdir(filepath.Dir(path))
			if err != nil {
				return err
			}

			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}

			file, err := os.Stat(realPath)
			if err != nil {
				return err
			}

			if file.IsDir() {
				err = os.Chdir(path)
				if err != nil {
					return err
				}

				stack.new()
				defer stack.pop()

				err = filepath.WalkDir(realPath, scanner)
				if err != nil {
					return err
				}

				results, err := stack.get()
				if err != nil {
					return err
				}

				for i := range *results {
					allResults = append(allResults, strings.Replace((*results)[i], realPath, path, 1))
				}

				return nil
			}
		}

		results, err := stack.get()
		if err != nil {
			return err
		}

		if utils.IsZip(path) && platform.ZipsAsDirs() {
			// zip files
			zipFiles, err := utils.ListZip(path)
			if err != nil {
				// skip invalid zip files
				return nil
			}

			for i := range zipFiles {
				abs := filepath.Join(path, zipFiles[i])
				if utils.MatchSystemFile(cfg, platform, (*system).Id, abs) {
					*results = append(*results, abs)
				}
			}
		} else {
			// regular files
			if utils.MatchSystemFile(cfg, platform, (*system).Id, path) {
				*results = append(*results, path)
			}
		}

		return nil
	}

	stack.new()
	defer stack.pop()

	root, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	err = os.Chdir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	// handle symlinks on root game folder because WalkDir fails silently on them
	var realPath string
	if root.Mode()&os.ModeSymlink == 0 {
		realPath = path
	} else {
		realPath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
	}

	realRoot, err := os.Stat(realPath)
	if err != nil {
		return nil, err
	}

	if !realRoot.IsDir() {
		return nil, fmt.Errorf("root is not a directory")
	}

	err = filepath.WalkDir(realPath, scanner)
	if err != nil {
		return nil, err
	}

	results, err := stack.get()
	if err != nil {
		return nil, err
	}

	allResults = append(allResults, *results...)

	// change root back to symlink
	if realPath != path {
		for i := range allResults {
			allResults[i] = strings.Replace(allResults[i], realPath, path, 1)
		}
	}

	err = os.Chdir(cwd)
	if err != nil {
		return nil, err
	}

	return allResults, nil
}
