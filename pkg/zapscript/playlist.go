// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package zapscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

const plsHeader = "[playlist]"

func isPlsFile(path string) bool {
	return filepath.Ext(strings.ToLower(path)) == ".pls"
}

var (
	plsFileRe  = regexp.MustCompile(`^File([1-9]\d*)\s*=\s*(.*)$`)
	plsTitleRe = regexp.MustCompile(`^Title([1-9]\d*)\s*=\s*(.*)$`)
)

type plsItem struct {
	file  string
	title string
}

type ArgPlaylistItem struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type ArgPlaylist struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Items []ArgPlaylistItem `json:"items"`
}

func readPlsFile(path string) ([]playlists.PlaylistItem, error) {
	//nolint:gosec // Safe: reads playlist files for media management
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read playlist file '%s': %w", path, err)
	}

	lines := strings.Split(string(content), "\n")
	filteredLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			filteredLines = append(filteredLines, trimmed)
		}
	}
	lines = filteredLines

	hasHeader := false
	items := make(map[int]plsItem)

	updateItem := func(idx int, file, title string) {
		item := items[idx]
		if file != "" {
			item.file = file
		}
		if title != "" {
			item.title = title
		}
		items[idx] = item
	}

	for _, line := range lines {
		if !hasHeader {
			if line == plsHeader {
				hasHeader = true
			}
			continue
		}

		if matches := plsFileRe.FindStringSubmatch(line); len(matches) == 3 {
			itemID := matches[1]
			file := matches[2]

			id, err := strconv.Atoi(itemID)
			if err != nil {
				log.Warn().Msgf("invalid file id in pls file: %s", path)
				continue
			}

			updateItem(id, file, "")
			continue
		}

		if matches := plsTitleRe.FindStringSubmatch(line); len(matches) == 3 {
			itemID := matches[1]
			title := matches[2]

			id, err := strconv.Atoi(itemID)
			if err != nil {
				log.Warn().Msgf("invalid title id in pls file: %s", path)
				continue
			}

			updateItem(id, "", title)
			continue
		}

		log.Warn().Msgf("unrecognized line in pls file: %s", line)
	}

	if !hasHeader {
		log.Warn().Msgf("no header found in pls file: %s", path)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("no items found in pls file: %s", path)
	}

	playlistItems := make([]playlists.PlaylistItem, 0)

	// sort items by number in fileX/titleX
	sorted := make([]int, 0, len(items))
	for k := range items {
		sorted = append(sorted, k)
	}
	sort.Ints(sorted)

	for _, k := range sorted {
		item := items[k]
		if item.file == "" {
			continue
		}

		if !filepath.IsAbs(item.file) {
			// if it's a relative path, do a basic check to see if it
			// exists, and expand it to an absolute path
			exists := false
			// just the current dir
			testFile := filepath.Base(item.file)

			// check name without advanced args if they're there
			// TODO: use parser
			if strings.Contains(testFile, "?") {
				last := strings.LastIndex(testFile, "?")
				noArgs := testFile[:last]
				absNoArgs := filepath.Join(filepath.Dir(path), noArgs)
				if _, err := os.Stat(absNoArgs); err == nil {
					exists = true
				}
			}

			absPath := filepath.Join(filepath.Dir(path), testFile)

			if !exists {
				if _, err := os.Stat(absPath); err == nil {
					exists = true
				}
			}

			if exists {
				item.file = absPath
			}
		}

		playlistItems = append(playlistItems, playlists.PlaylistItem{
			Name:      item.title,
			ZapScript: item.file,
		})
	}

	return playlistItems, nil
}

func readPlaylistFolder(path string) ([]playlists.PlaylistItem, error) {
	if path == "" {
		return nil, errors.New("no playlist path specified")
	}

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("failed to stat path '%s': %w", path, err)
	}

	dir, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory '%s': %w", path, err)
	}

	files := make([]string, 0)
	for _, file := range dir {
		if file.IsDir() || filepath.Ext(file.Name()) == "" {
			continue
		}
		if strings.HasPrefix(file.Name(), ".") {
			continue
		}
		files = append(files, filepath.Join(path, file.Name()))
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no valid files found in: %s", path)
	}

	items := make([]playlists.PlaylistItem, 0)
	for _, file := range files {
		name := filepath.Base(file)
		name = strings.TrimSuffix(name, filepath.Ext(name))
		items = append(items, playlists.PlaylistItem{
			Name:      name,
			ZapScript: file,
		})
	}

	return items, nil
}

//nolint:gocritic // single-use parameter in command handler
func loadPlaylist(pl platforms.Platform, env platforms.CmdEnv) (*playlists.Playlist, error) {
	if len(env.Cmd.Args) == 0 {
		return nil, ErrArgCount
	} else if env.Cmd.Args[0] == "" {
		return nil, ErrRequiredArgs
	}

	if helpers.MaybeJSON([]byte(env.Cmd.Args[0])) {
		var plsArg ArgPlaylist
		if err := json.Unmarshal([]byte(env.Cmd.Args[0]), &plsArg); err != nil {
			return nil, fmt.Errorf("invalid playlist json: %w", err)
		}

		var items []playlists.PlaylistItem
		for _, item := range plsArg.Items {
			items = append(items, playlists.PlaylistItem{
				Name:      item.Name,
				ZapScript: item.ZapScript,
			})
		}

		return playlists.NewPlaylist(plsArg.ID, plsArg.Name, items), nil
	}

	path, err := findFile(pl, env.Cfg, env.Cmd.Args[0])
	if err != nil {
		return nil, err
	}

	var items []playlists.PlaylistItem
	if isPlsFile(path) {
		items, err = readPlsFile(path)
		if err != nil {
			return nil, err
		}
	} else {
		items, err = readPlaylistFolder(path)
		if err != nil {
			return nil, err
		}
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	if v, ok := env.Cmd.AdvArgs["mode"]; ok && strings.EqualFold(v, "shuffle") {
		log.Info().Msgf("shuffling playlist: %s", env.Cmd.Args[0])
		if len(items) == 0 {
			log.Warn().Msgf("playlist is empty: %s", path)
		} else {
			rand.Shuffle(len(items), func(i, j int) {
				items[i], items[j] = items[j], items[i]
			})
		}
	}

	return playlists.NewPlaylist(env.Cmd.Args[0], name, items), nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistPlay(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active != nil &&
		(len(env.Cmd.Args) == 0 || env.Cmd.Args[0] == "") {
		log.Info().Msg("starting paused playlist")
		pls := playlists.Play(*env.Playlist.Active)
		env.Playlist.Queue <- pls
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, nil
	}

	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("items", pls.Items).Msgf("play playlist: %v", env.Cmd.Args)
	pls = playlists.Play(*pls)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistLoad(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("items", pls.Items).Msgf("load playlist: %s", env.Cmd.Args)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistOpen(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if env.Playlist.Active != nil && env.Playlist.Active.ID == pls.ID {
		log.Debug().Msg("opening active playlist")
		pls.Index = env.Playlist.Active.Index
		// Validate index bounds
		if pls.Index >= len(pls.Items) {
			pls.Index = len(pls.Items) - 1
		}
		if pls.Index < 0 {
			pls.Index = 0
		}
	}

	items := make([]widgetmodels.PickerItem, 0, len(pls.Items))
	for i, m := range pls.Items {
		var name string

		// TODO: this should actually parse the script and check if a name is possible
		if strings.TrimSpace(m.Name) == "" {
			if !strings.HasPrefix(m.ZapScript, "**") {
				name = filepath.Base(m.ZapScript)
				name = strings.TrimSuffix(name, filepath.Ext(name))
			} else {
				name = m.ZapScript
			}
		} else {
			name = m.Name
		}

		if i == pls.Index {
			name = fmt.Sprintf("> %s", name)
		}

		zapscript := "**playlist.goto:" + strconv.Itoa(i+1) + "||**playlist.play"

		items = append(items, widgetmodels.PickerItem{
			Name:      name,
			ZapScript: zapscript,
		})
	}

	log.Info().Any("items", pls.Items).Msgf("open playlist: %s", env.Cmd.Args)
	env.Playlist.Queue <- pls

	if err := pl.ShowPicker(env.Cfg, widgetmodels.PickerArgs{
		Title:    pls.Name,
		Items:    items,
		Selected: pls.Index,
	}); err != nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, fmt.Errorf("failed to show picker: %w", err)
	}
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistNext(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, errors.New("no playlist active")
	}

	pls := playlists.Next(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistPrevious(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, errors.New("no playlist active")
	}

	pls := playlists.Previous(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistGoto(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, errors.New("no playlist active")
	}

	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	indexArg, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid index '%s': %w", env.Cmd.Args[0], err)
	}

	newIndex := indexArg - 1

	if env.Playlist.Active.Index == newIndex {
		log.Warn().Msgf("playlist is already at index %d, not changing", indexArg)
		return platforms.CmdResult{}, nil
	}

	pls := playlists.Goto(*env.Playlist.Active, newIndex)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistStop(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, errors.New("no playlist active")
	}

	env.Playlist.Queue <- nil

	if err := pl.StopActiveLauncher(); err != nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        nil,
		}, fmt.Errorf("failed to stop active launcher: %w", err)
	}
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        nil,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistPause(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, errors.New("no playlist active")
	}

	pls := playlists.Pause(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	if err := pl.StopActiveLauncher(); err != nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, fmt.Errorf("failed to stop active launcher: %w", err)
	}
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}
