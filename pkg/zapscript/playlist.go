// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"context"
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
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	plsHeader                        = "[playlist]"
	maxLoggedPlaylistItems           = 10
	playlistPreviousRestartThreshold = 3 * time.Second
)

func isPlsFile(path string) bool {
	return filepath.Ext(strings.ToLower(path)) == ".pls"
}

// ErrNoPlaylistActive is returned by playlist control commands when no playlist
// is active for the requested slot. This is an expected user condition (firing a
// playlist command with nothing playing), so callers log it at Warn, not Error.
var ErrNoPlaylistActive = errors.New("no playlist active")

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

type playlistItemsLog struct {
	Items     []playlists.PlaylistItem `json:"items"`
	Total     int                      `json:"total"`
	Showing   int                      `json:"showing"`
	Truncated int                      `json:"truncated,omitempty"`
}

func playlistItemsForLog(items []playlists.PlaylistItem) any {
	if len(items) <= maxLoggedPlaylistItems {
		return items
	}
	return playlistItemsLog{
		Total:     len(items),
		Showing:   maxLoggedPlaylistItems,
		Truncated: len(items) - maxLoggedPlaylistItems,
		Items:     items[:maxLoggedPlaylistItems],
	}
}

func activePlaylistForSlot(env *platforms.CmdEnv, slot string) *playlists.Playlist {
	if slot == mediaslot.Background {
		return env.Playlist.Background
	}
	return env.Playlist.Active
}

func commandSlot(env *platforms.CmdEnv) (string, error) {
	if slot, explicit, err := explicitCommandSlot(env); err != nil || explicit {
		return slot, err
	}
	if env.Playlist.Current != nil && env.Playlist.Current.Slot != "" {
		slot, err := mediaslot.Normalize(env.Playlist.Current.Slot)
		if err != nil {
			return "", fmt.Errorf("normalize media slot: %w", err)
		}
		return slot, nil
	}
	if env.Playlist.Active != nil && env.Playlist.Active.Slot != "" {
		slot, err := mediaslot.Normalize(env.Playlist.Active.Slot)
		if err != nil {
			return "", fmt.Errorf("normalize media slot: %w", err)
		}
		return slot, nil
	}
	return mediaslot.Primary, nil
}

func explicitCommandSlot(env *platforms.CmdEnv) (slot string, explicit bool, err error) {
	rawSlot, ok := env.Cmd.AdvArgs.Raw()[string(zapscript.KeySlot)]
	if !ok {
		return "", false, nil
	}
	normalizedSlot, err := mediaslot.Normalize(rawSlot)
	if err != nil {
		return "", true, fmt.Errorf("normalize media slot: %w", err)
	}
	return normalizedSlot, true, nil
}

func commandSlotOrActiveFallback(env *platforms.CmdEnv) (string, error) {
	slot, explicit, err := explicitCommandSlot(env)
	if err != nil || explicit {
		return slot, err
	}
	if env.Playlist.Current != nil && env.Playlist.Current.Slot != "" {
		slot, err := mediaslot.Normalize(env.Playlist.Current.Slot)
		if err != nil {
			return "", fmt.Errorf("normalize media slot: %w", err)
		}
		return slot, nil
	}
	if env.Playlist.Active == nil && env.Playlist.Background != nil {
		return mediaslot.Background, nil
	}
	return mediaslot.Primary, nil
}

func restartCurrentPlaylistTrack(env *platforms.CmdEnv, slot string) (bool, error) {
	if env.PlaybackManager == nil {
		return false, nil
	}
	state := env.PlaybackManager.State(slot)
	if state.Path == "" || state.Position <= playlistPreviousRestartThreshold {
		return false, nil
	}
	if err := env.PlaybackManager.Seek(slot, -state.Position); err != nil {
		return false, fmt.Errorf("restart playlist track: %w", err)
	}
	log.Info().Str("slot", slot).Dur("position", state.Position).Msg("restarting current playlist track")
	return true, nil
}

func queuePlaylistUpdate(env *platforms.CmdEnv, pls *playlists.Playlist) error {
	slot := mediaslot.Primary
	if pls != nil && pls.Slot != "" {
		slot = pls.Slot
	} else if cmdSlot, err := commandSlot(env); err == nil {
		slot = cmdSlot
	}
	if pls != nil {
		pls.Slot = slot
	}
	if env.LauncherCtx == nil && env.ServiceCtx == nil {
		env.Playlist.Queue <- pls
		return nil
	}

	var launcherDone <-chan struct{}
	if env.LauncherCtx != nil {
		launcherDone = env.LauncherCtx.Done()
	}

	var serviceDone <-chan struct{}
	if env.ServiceCtx != nil {
		serviceDone = env.ServiceCtx.Done()
	}

	select {
	case env.Playlist.Queue <- pls:
		return nil
	case <-launcherDone:
		return env.LauncherCtx.Err()
	case <-serviceDone:
		return env.ServiceCtx.Err()
	}
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

func readPlaylistFolder(cfg *config.Instance, pl platforms.Platform, path string) ([]playlists.PlaylistItem, error) {
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
	var matcher *helpers.LauncherMatcher
	for _, file := range dir {
		if file.IsDir() || filepath.Ext(file.Name()) == "" {
			continue
		}
		if strings.HasPrefix(file.Name(), ".") {
			continue
		}
		fullPath := filepath.Join(path, file.Name())
		if matcher == nil {
			matcher = helpers.NewLauncherMatcher(cfg, pl)
		}
		if _, err := matcher.FindLauncher(fullPath); err != nil {
			continue
		}
		files = append(files, fullPath)
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

	var args zapscript.PlaylistArgs
	if err := ParseAdvArgs(pl, &env, &args); err != nil {
		return nil, fmt.Errorf("invalid advanced arguments: %w", err)
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

		pls := playlists.NewPlaylist(plsArg.ID, plsArg.Name, items)
		slot, slotErr := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
		if slotErr != nil {
			return nil, fmt.Errorf("normalize media slot: %w", slotErr)
		}
		pls.Slot = slot
		pls.Loop = zapscript.IsRepeatAll(args.Repeat)
		pls.LoopOne = zapscript.IsRepeatOne(args.Repeat)
		return pls, nil
	}

	path, err := findFile(afero.NewOsFs(), pl, env.Cfg, env.Cmd.Args[0])
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
		items, err = readPlaylistFolder(env.Cfg, pl, path)
		if err != nil {
			return nil, err
		}
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	if zapscript.IsModeShuffle(args.Mode) {
		log.Info().Msgf("shuffling playlist: %s", env.Cmd.Args[0])
		if len(items) == 0 {
			log.Warn().Msgf("playlist is empty: %s", path)
		} else {
			rand.Shuffle(len(items), func(i, j int) {
				items[i], items[j] = items[j], items[i]
			})
		}
	}

	pls := playlists.NewPlaylist(env.Cmd.Args[0], name, items)
	slot, slotErr := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
	if slotErr != nil {
		return nil, fmt.Errorf("normalize media slot: %w", slotErr)
	}
	pls.Slot = slot
	pls.Loop = zapscript.IsRepeatAll(args.Repeat)
	pls.LoopOne = zapscript.IsRepeatOne(args.Repeat)
	return pls, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistPlay(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	hasPlaylistArg := len(env.Cmd.Args) > 0 && env.Cmd.Args[0] != ""
	slot, err := commandSlot(&env)
	if !hasPlaylistArg {
		slot, err = commandSlotOrActiveFallback(&env)
	}
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active != nil && !hasPlaylistArg {
		log.Info().Msg("starting paused playlist")
		pls := playlists.Play(*active)
		if queueErr := queuePlaylistUpdate(&env, pls); queueErr != nil {
			return platforms.CmdResult{}, queueErr
		}
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, nil
	}

	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("items", playlistItemsForLog(pls.Items)).Msgf("play playlist: %v", env.Cmd.Args)
	pls = playlists.Play(*pls)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

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

	log.Info().Any("items", playlistItemsForLog(pls.Items)).Msgf("load playlist: %s", env.Cmd.Args)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistOpen(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	var pls *playlists.Playlist
	slot, err := commandSlot(&env)
	if len(env.Cmd.Args) == 0 {
		slot, err = commandSlotOrActiveFallback(&env)
	}
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)

	// If no args provided, use the currently active playlist
	if len(env.Cmd.Args) == 0 {
		if active == nil {
			return platforms.CmdResult{}, ErrNoPlaylistActive
		}
		log.Debug().Msg("opening active playlist (no args)")
		// Use active playlist as-is (preserves current Index and state)
		pls = active
	} else {
		// Load playlist from argument
		var err error
		pls, err = loadPlaylist(pl, env)
		if err != nil {
			return platforms.CmdResult{}, err
		}

		// If loaded playlist matches active, preserve current position
		if active != nil && active.ID == pls.ID {
			log.Debug().Msg("opening active playlist")
			pls.Index = active.Index
			// Validate index bounds
			if pls.Index >= len(pls.Items) {
				pls.Index = len(pls.Items) - 1
			}
			if pls.Index < 0 {
				pls.Index = 0
			}
		}
	}

	choices := make([]uievents.Choice, 0, len(pls.Items))
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
			name = "> " + name
		}

		zapscript := "**playlist.goto:" + strconv.Itoa(i+1) + "?slot=" + slot + "||**playlist.play?slot=" + slot

		choices = append(choices, uievents.Choice{
			Label: name,
			Value: zapscript,
		})
	}

	log.Info().Any("items", playlistItemsForLog(pls.Items)).Msgf("open playlist: %s", env.Cmd.Args)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	if env.UI == nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, errors.New("UI event service is unavailable")
	}
	handle, openErr := env.UI.Open(env.ServiceCtx, &uievents.Request{
		Kind:           apimodels.UIEventKindPicker,
		Title:          pls.Name,
		Choices:        choices,
		SelectedChoice: pls.Index,
		Timeout:        30 * time.Second,
		Dismissible:    true,
	})
	if openErr != nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, fmt.Errorf("failed to open picker: %w", openErr)
	}
	go runPickerResult(env.Cfg, handle, client.LocalClient)
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func runPickerResult(
	cfg *config.Instance,
	handle *uievents.Handle,
	run func(context.Context, *config.Instance, string, string) (string, error),
) {
	result, ok := <-handle.Results
	if !ok || result.Resolution.Outcome != apimodels.UIOutcomeSelected {
		return
	}
	script, ok := result.Value.(string)
	if !ok || script == "" {
		log.Error().Str("event_id", handle.ID).Msg("picker returned invalid private action")
		return
	}
	params, err := json.Marshal(apimodels.RunParams{Text: &script})
	if err != nil {
		log.Error().Err(err).Str("event_id", handle.ID).Msg("failed to marshal picker action")
		return
	}
	if _, err = run(context.Background(), cfg, apimodels.MethodRun, string(params)); err != nil {
		log.Error().Err(err).Str("event_id", handle.ID).Msg("failed to run picker action")
	}
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistNext(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	slot, err := commandSlotOrActiveFallback(&env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active == nil {
		return platforms.CmdResult{}, ErrNoPlaylistActive
	}

	pls := playlists.Next(*active)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistPrevious(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	slot, err := commandSlotOrActiveFallback(&env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active == nil {
		return platforms.CmdResult{}, ErrNoPlaylistActive
	}
	if restarted, restartErr := restartCurrentPlaylistTrack(&env, slot); restartErr != nil {
		return platforms.CmdResult{}, restartErr
	} else if restarted {
		return platforms.CmdResult{}, nil
	}

	pls := playlists.Previous(*active)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistGoto(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	slot, err := commandSlotOrActiveFallback(&env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active == nil {
		return platforms.CmdResult{}, ErrNoPlaylistActive
	}

	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	indexArg, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid index '%s': %w", env.Cmd.Args[0], err)
	}

	newIndex := indexArg - 1

	if active.Index == newIndex {
		log.Warn().Msgf("playlist is already at index %d, not changing", indexArg)
		return platforms.CmdResult{}, nil
	}

	pls := playlists.Goto(*active, newIndex)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdPlaylistStop(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	slot, err := commandSlotOrActiveFallback(&env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active == nil {
		return platforms.CmdResult{}, ErrNoPlaylistActive
	}

	if err := queuePlaylistUpdate(&env, &playlists.Playlist{Slot: slot, Clear: true}); err != nil {
		return platforms.CmdResult{}, err
	}

	if slot == mediaslot.Background {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        nil,
		}, nil
	}
	if err := pl.StopActiveLauncher(platforms.StopForMenu); err != nil {
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
	slot, err := commandSlotOrActiveFallback(&env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	active := activePlaylistForSlot(&env, slot)
	if active == nil {
		return platforms.CmdResult{}, ErrNoPlaylistActive
	}

	pls := playlists.Pause(*active)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	if slot == mediaslot.Background {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, nil
	}
	if err := pl.StopActiveLauncher(platforms.StopForMenu); err != nil {
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
