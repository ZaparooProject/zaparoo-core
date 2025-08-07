/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package mister

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

// GetSystem looks up an exact system definition by ID.
func GetSystem(id string) (*Core, error) {
	if system, ok := Systems[id]; ok {
		return &system, nil
	} else {
		return nil, fmt.Errorf("unknown system: %s", id)
	}
}

func GetGroup(groupId string) (Core, error) {
	var merged Core
	if _, ok := CoreGroups[groupId]; !ok {
		return merged, fmt.Errorf("no system group found for %s", groupId)
	}

	if len(CoreGroups[groupId]) < 1 {
		return merged, fmt.Errorf("no systems in %s", groupId)
	} else if len(CoreGroups[groupId]) == 1 {
		return CoreGroups[groupId][0], nil
	}

	merged = CoreGroups[groupId][0]
	merged.Slots = make([]Slot, 0)
	for _, s := range CoreGroups[groupId] {
		merged.Slots = append(merged.Slots, s.Slots...)
	}

	return merged, nil
}

// LookupCore case-insensitively looks up system ID definition.
func LookupCore(id string) (*Core, error) {
	if system, err := GetGroup(id); err == nil {
		return &system, nil
	}

	for k, v := range Systems {
		if strings.EqualFold(k, id) {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("unknown system: %s", id)
}

// MatchSystemFile returns true if a given file's extension is valid for a system.
func MatchSystemFile(system *Core, path string) bool {
	// ignore dot files
	if strings.HasPrefix(filepath.Base(path), ".") {
		return false
	}

	for _, args := range system.Slots {
		for _, ext := range args.Exts {
			if strings.HasSuffix(strings.ToLower(path), ext) {
				return true
			}
		}
	}

	return false
}

func AllSystems() []Core {
	var systems []Core

	keys := helpers.AlphaMapKeys(Systems)
	for _, k := range keys {
		systems = append(systems, Systems[k])
	}

	return systems
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
// TODO: get rid of this
func GetFiles(systemId string, path string) ([]string, error) {
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

			realPath, evalErr := filepath.EvalSymlinks(path)
			if evalErr != nil {
				return evalErr
			}

			file, statErr := os.Stat(realPath)
			if statErr != nil {
				return fmt.Errorf("failed to stat file %s: %w", realPath, statErr)
			}

			if file.IsDir() {
				err = os.Chdir(path)
				if err != nil {
					return fmt.Errorf("failed to change directory to %s: %w", path, err)
				}

				stack.new()
				defer stack.pop()

				err = filepath.WalkDir(realPath, scanner)
				if err != nil {
					return fmt.Errorf("failed to walk directory %s: %w", realPath, err)
				}

				results, stackErr := stack.get()
				if stackErr != nil {
					return stackErr
				}

				for i := range *results {
					allResults = append(allResults, strings.Replace((*results)[i], realPath, path, 1))
				}

				return nil
			}
		}

		results, stackErr := stack.get()
		if stackErr != nil {
			return stackErr
		}

		if strings.HasSuffix(strings.ToLower(path), ".zip") {
			// zip files
			zipFiles, zipErr := helpers.ListZip(path)
			if zipErr != nil {
				// skip invalid zip files
				return nil
			}

			for i := range zipFiles {
				if MatchSystemFile(system, zipFiles[i]) {
					abs := filepath.Join(path, zipFiles[i])
					*results = append(*results, abs)
				}
			}
		} else if MatchSystemFile(system, path) {
			// regular files
			*results = append(*results, path)
		}

		return nil
	}

	stack.new()
	defer stack.pop()

	root, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to lstat path %s: %w", path, err)
	}

	err = os.Chdir(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("failed to change directory to %s: %w", filepath.Dir(path), err)
	}

	// handle symlinks on root game folder because WalkDir fails silently on them
	var realPath string
	if root.Mode()&os.ModeSymlink == 0 {
		realPath = path
	} else {
		realPath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate symlinks for %s: %w", path, err)
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
		return nil, fmt.Errorf("failed to walk directory %s: %w", realPath, err)
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

func GetAllFiles(systemPaths map[string][]string, statusFn func(systemId string, path string)) ([][2]string, error) {
	var allFiles [][2]string

	for systemId, paths := range systemPaths {
		for i := range paths {
			statusFn(systemId, paths[i])

			files, err := GetFiles(systemId, paths[i])
			if err != nil {
				return nil, err
			}

			for i := range files {
				allFiles = append(allFiles, [2]string{systemId, files[i]})
			}
		}
	}

	return allFiles, nil
}

func FilterUniqueFilenames(files []string) []string {
	var filtered []string
	filenames := make(map[string]struct{})
	for i := range files {
		fn := filepath.Base(files[i])
		if _, ok := filenames[fn]; ok {
			continue
		} else {
			filenames[fn] = struct{}{}
			filtered = append(filtered, files[i])
		}
	}
	return filtered
}

var zipRe = regexp.MustCompile(`^(.*\.zip)/(.+)$`)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	zipMatch := zipRe.FindStringSubmatch(path)
	if zipMatch != nil {
		zipPath := zipMatch[1]
		file := zipMatch[2]

		zipFiles, err := helpers.ListZip(zipPath)
		if err != nil {
			return false
		}

		for i := range zipFiles {
			if zipFiles[i] == file {
				return true
			}
		}
	}

	return false
}

type RbfInfo struct {
	Path      string // full path to RBF file
	Filename  string // base filename of RBF file
	ShortName string // base filename without date or extension
	MglName   string // relative path launch-able from MGL file
}

func ParseRbf(path string) RbfInfo {
	info := RbfInfo{
		Path:     path,
		Filename: filepath.Base(path),
	}

	if strings.Contains(info.Filename, "_") {
		info.ShortName = info.Filename[0:strings.LastIndex(info.Filename, "_")]
	} else {
		info.ShortName = strings.TrimSuffix(info.Filename, filepath.Ext(info.Filename))
	}

	if strings.HasPrefix(path, misterconfig.SDRootDir) {
		relDir := strings.TrimPrefix(filepath.Dir(path), misterconfig.SDRootDir+"/")
		info.MglName = filepath.Join(relDir, info.ShortName)
	} else {
		info.MglName = path
	}

	return info
}

// Find all rbf files in the top 2 menu levels of the SD card.
func shallowScanRbf() ([]RbfInfo, error) {
	results := make([]RbfInfo, 0)

	isRbf := func(file os.DirEntry) bool {
		return filepath.Ext(strings.ToLower(file.Name())) == ".rbf"
	}

	infoSymlink := func(path string) (RbfInfo, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return RbfInfo{}, fmt.Errorf("failed to lstat %s: %w", path, err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			newPath, err := os.Readlink(path)
			if err != nil {
				return RbfInfo{}, fmt.Errorf("failed to readlink %s: %w", path, err)
			}

			return ParseRbf(newPath), nil
		} else {
			return ParseRbf(path), nil
		}
	}

	files, err := os.ReadDir(misterconfig.SDRootDir)
	if err != nil {
		return results, fmt.Errorf("failed to read SD root directory %s: %w", misterconfig.SDRootDir, err)
	}

	for _, file := range files {
		if file.IsDir() && strings.HasPrefix(file.Name(), "_") {
			subFiles, err := os.ReadDir(filepath.Join(misterconfig.SDRootDir, file.Name()))
			if err != nil {
				continue
			}

			for _, subFile := range subFiles {
				if isRbf(subFile) {
					path := filepath.Join(misterconfig.SDRootDir, file.Name(), subFile.Name())
					info, err := infoSymlink(path)
					if err != nil {
						continue
					}
					results = append(results, info)
				}
			}
		} else if isRbf(file) {
			path := filepath.Join(misterconfig.SDRootDir, file.Name())
			info, err := infoSymlink(path)
			if err != nil {
				continue
			}
			results = append(results, info)
		}
	}

	return results, nil
}

// SystemsWithRbf returns a map of all system IDs which have an existing rbf file.
func SystemsWithRbf() map[string]RbfInfo {
	// TODO: include alt rbfs somehow?
	results := make(map[string]RbfInfo)

	rbfFiles, err := shallowScanRbf()
	if err != nil {
		return results
	}

	for _, rbfFile := range rbfFiles {
		for _, system := range Systems {
			shortName := system.RBF

			if strings.Contains(shortName, "/") {
				shortName = shortName[strings.LastIndex(shortName, "/")+1:]
			}

			if strings.EqualFold(rbfFile.ShortName, shortName) {
				results[system.ID] = rbfFile
			}
		}
	}

	return results
}
