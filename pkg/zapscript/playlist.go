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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/autoadvance"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	servicestate "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	plsHeader                        = "[playlist]"
	maxLoggedPlaylistItems           = 10
	playlistPreviousRestartThreshold = 3 * time.Second
)

// maxRotation caps an advance, idle or countdown value.
const maxRotation = 24 * time.Hour

// ErrNoPlaylistActive is returned by playlist control commands when no playlist
// is active for the requested slot. This is an expected user condition (firing a
// playlist command with nothing playing), so callers log it at Warn, not Error.
var ErrNoPlaylistActive = errors.New("no playlist active")

var (
	// ErrRotationConflict is returned when advance and idle are both given.
	ErrRotationConflict = errors.New("advance and idle cannot be used together")
	// ErrBackgroundRotation is returned when advance or idle is asked of the
	// background slot, whose tracks already move on when they finish.
	ErrBackgroundRotation = errors.New("advance and idle are only available on the primary slot")
	// ErrInvalidRotation is returned for an advance, idle or countdown value
	// that is not a usable duration.
	ErrInvalidRotation = errors.New("invalid rotation duration")
	// ErrInvalidPlaylistStart is returned for a start that is not an item
	// number.
	ErrInvalidPlaylistStart = errors.New("invalid playlist start")
	// ErrCountdownWithoutRotation is returned when countdown is given to a
	// launch.random that does not rotate.
	ErrCountdownWithoutRotation = errors.New("countdown needs advance or idle")
)

var (
	plsFileRe  = regexp.MustCompile(`(?i)^File([1-9]\d*)\s*=\s*(.*)$`)
	plsTitleRe = regexp.MustCompile(`(?i)^Title([1-9]\d*)\s*=\s*(.*)$`)
	// plsKnownKeyRe matches the pls keys that carry nothing a playlist uses.
	plsKnownKeyRe = regexp.MustCompile(`(?i)^(NumberOfEntries|Version|Length[1-9]\d*)\s*=`)
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

// updateWouldLaunch reports whether queueing pls for slot starts a launch:
// a playing playlist that replaces the open one, moves to another item,
// starts playing from pause, or relaunches its item. An update that leaves
// a playing playlist where it is, such as the play the picker sends after its
// goto has already started the item, launches nothing.
func updateWouldLaunch(env *platforms.CmdEnv, slot string, pls *playlists.Playlist) bool {
	if pls == nil || !pls.Playing || pls.Clear || pls.Refresh {
		return false
	}
	active := activePlaylistForSlot(env, slot)
	return active == nil || active.ID != pls.ID || !active.Playing ||
		active.Index != pls.Index || pls.ForceRelaunch
}

func queuePlaylistUpdate(env *platforms.CmdEnv, pls *playlists.Playlist) error {
	slot := mediaslot.Primary
	if pls != nil && pls.Slot != "" {
		slot = pls.Slot
	} else if cmdSlot, err := commandSlot(env); err == nil {
		slot = cmdSlot
	}
	if updateWouldLaunch(env, slot, pls) && env.LaunchInProgress != nil && env.LaunchInProgress() {
		// The launch this update starts would be refused, but the playlist
		// would move anyway and end up claiming an item that never
		// launched while another item's media plays. Refuse the update
		// instead, so the playlist moves only when its item can launch.
		return fmt.Errorf("playlist %s: %w", slot, servicestate.ErrLaunchInProgress)
	}
	if pls != nil {
		if env.StopSeq != nil && !pls.StopStamped {
			// Taken as the command is issued, so a stop that comes before the
			// queue worker launches the item cancels it.
			pls.StopSeq = env.StopSeq()
			pls.StopStamped = true
		}
		pls.Slot = slot
		pls.FromPlaylistItem = env.Source == tokens.SourcePlaylist
		if slot == mediaslot.Primary && env.Playlist.HoldToken != nil {
			holdToken := *env.Playlist.HoldToken
			pls.HoldToken = &holdToken
		} else {
			pls.HoldToken = nil
		}
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
	content, err := readBoundedFile(path)
	if err != nil {
		return nil, err
	}

	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
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
			if strings.EqualFold(line, plsHeader) {
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

		if plsKnownKeyRe.MatchString(line) {
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

		// A URL is a stream or ZapLink location, so its query string is part
		// of it rather than launch arguments.
		if strings.Contains(item.file, "://") {
			item.file = literalLaunch(item.file)
		}

		playlistItems = append(playlistItems, playlists.PlaylistItem{
			Name:      item.title,
			ZapScript: item.file,
		})
	}

	return playlistItems, nil
}

// launchersInclude reports whether one of launchers belongs to systemID.
func launchersInclude(launchers []platforms.Launcher, systemID string) bool {
	for i := range launchers {
		if strings.EqualFold(launchers[i].SystemID, systemID) {
			return true
		}
	}
	return false
}

// folderPageSize is how many rows a folder playlist reads from the media
// index at a time.
const folderPageSize = 500

// folderPlaylistItems lists a folder as a playlist exactly as media.browse
// lists it: the folder's indexed media, in filename order, without hidden
// media, named as browse names them, and with system set only that system's
// media, as a browse filtered by it. So item n of the playlist is entry n of
// the folder's listing, which a client counting from a browse can rely on
// for the start argument. The index is preferred, not required: a folder it
// does not cover is read from disk instead, keeping to system's launchers.
func folderPlaylistItems(
	env *platforms.CmdEnv, pl platforms.Platform, path string, system *systemdefs.System,
) ([]playlists.PlaylistItem, error) {
	items, indexed, err := indexedFolderItems(env, path, system)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("folder playlist: index unavailable, reading the folder")
	}
	if len(items) > 0 || indexed {
		return items, nil
	}
	return readPlaylistFolder(env.Cfg, pl, path, system)
}

// indexedFolderItems reads a folder's direct media from the index the way
// a filename-sorted browse of it does, a page at a time. indexed reports
// whether the index covers the folder at all, so a folder whose media is all
// hidden or of another system stays empty instead of being read from disk.
func indexedFolderItems(
	env *platforms.CmdEnv, path string, system *systemdefs.System,
) (items []playlists.PlaylistItem, indexed bool, err error) {
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil, false, nil
	}
	var systems []systemdefs.System
	if system != nil {
		systems = []systemdefs.System{*system}
	}
	ctx := env.ServiceCtx
	if ctx == nil {
		ctx = context.Background()
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(path), "/") + "/"
	var cursor *database.BrowseCursor
	for {
		rows, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			Cursor:        cursor,
			ExcludeHidden: true,
			PathPrefix:    prefix,
			Limit:         folderPageSize,
			Sort:          "filename-asc",
			Systems:       systems,
		})
		if err != nil {
			return nil, false, fmt.Errorf("browse folder %s: %w", path, err)
		}
		for i := range rows {
			// A playlist file is a playlist of its own, not a track.
			if rows[i].SystemID == systemdefs.SystemAudio && isAudioPlaylistExt(rows[i].Path) {
				continue
			}
			items = append(items, playlists.PlaylistItem{
				Name:      mediatags.StripStructuralSetMarkers(rows[i].Name),
				ZapScript: rows[i].Path,
			})
		}
		if len(rows) < folderPageSize {
			if len(items) > 0 {
				return items, true, nil
			}
			covered, err := folderIndexed(ctx, env, prefix)
			return items, covered, err
		}
		last := rows[len(rows)-1]
		cursor = &database.BrowseCursor{
			SortValue: last.SortValue,
			SortMode:  last.SortMode,
			LastID:    last.MediaID,
		}
	}
}

// folderIndexed reports whether the index holds any media, hidden or of any
// system, under prefix.
func folderIndexed(ctx context.Context, env *platforms.CmdEnv, prefix string) (bool, error) {
	rows, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: prefix,
		Limit:      1,
		Sort:       "filename-asc",
	})
	if err != nil {
		return false, fmt.Errorf("probe folder %s: %w", prefix, err)
	}
	return len(rows) > 0, nil
}

func readPlaylistFolder(
	cfg *config.Instance, pl platforms.Platform, path string, system *systemdefs.System,
) ([]playlists.PlaylistItem, error) {
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
		if isAudioPlaylistExt(fullPath) &&
			launchersInclude(matcher.PathToLaunchers(fullPath), systemdefs.SystemAudio) {
			continue
		}
		if system != nil {
			if !launchersInclude(matcher.PathToLaunchers(fullPath), system.ID) {
				continue
			}
		} else if _, err := matcher.FindLauncher(fullPath); err != nil {
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

	coll, isCollection, collErr := resolveCollection(pl, &env, env.Cmd.Args[0])
	if collErr != nil {
		return nil, collErr
	}
	if isCollection {
		return collectionPlaylist(pl, &env, coll, &args)
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
		shufflePlaylistIfAsked(pls, &args)
		// A playlist written into a script inherits the trust of that
		// script: one fetched from a ZapLink, or nested in an untrusted
		// playlist's item, stays untrusted.
		pls.Unsafe = env.Unsafe
		pls.AllowedCommands = env.AllowedCommands
		if err := applyPlaylistArgs(pl, &env, pls, &args); err != nil {
			return nil, err
		}
		return pls, nil
	}

	path, err := findFile(afero.NewOsFs(), pl, env.Cfg, env.Launchers, env.Cmd.Args[0], env.PathRoot)
	if err != nil {
		return nil, err
	}

	var items []playlists.PlaylistItem
	if isAudioPlaylistExt(path) {
		items, err = readPlaylistFile(path)
		if err != nil {
			return nil, err
		}
	} else {
		var system *systemdefs.System
		if args.System != "" {
			system, err = systemdefs.LookupSystem(args.System)
			if err != nil {
				return nil, fmt.Errorf("invalid playlist system: %w", err)
			}
		}
		items, err = folderPlaylistItems(&env, pl, path, system)
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
		}
	}

	pls := playlists.NewPlaylist(env.Cmd.Args[0], name, items)
	shufflePlaylistIfAsked(pls, &args)
	pls.Unsafe = env.Unsafe
	pls.AllowedCommands = env.AllowedCommands
	if err := applyPlaylistArgs(pl, &env, pls, &args); err != nil {
		return nil, err
	}
	return pls, nil
}

// collectionPlaylist makes a playlist from the items of a deck or launcher
// collection.
func collectionPlaylist(
	pl platforms.Platform, env *platforms.CmdEnv, coll *collection, args *zapscript.PlaylistArgs,
) (*playlists.Playlist, error) {
	pls := playlists.NewPlaylist(coll.id, coll.name, slices.Clone(coll.items))
	shufflePlaylistIfAsked(pls, args)
	pls.DeckID = coll.deckID
	pls.Unsafe = coll.unsafe
	pls.AllowedCommands = env.AllowedCommands
	if err := applyPlaylistArgs(pl, env, pls, args); err != nil {
		return nil, err
	}
	return pls, nil
}

// shufflePlaylistIfAsked puts a playlist that was just loaded in a random
// order when its command asks for mode=shuffle, keeping the order it loaded
// in so shuffle can be turned off later.
func shufflePlaylistIfAsked(pls *playlists.Playlist, args *zapscript.PlaylistArgs) {
	if zapscript.IsModeShuffle(args.Mode) {
		playlists.ShuffleItems(pls)
	}
}

// applyPlaylistArgs sets the slot, repeat and rotation of a playlist from the
// arguments of the command that opens it, and the item it opens on: the one
// start names, else where the active profile left it.
func applyPlaylistArgs(
	pl platforms.Platform, env *platforms.CmdEnv, pls *playlists.Playlist, args *zapscript.PlaylistArgs,
) error {
	slot, err := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
	if err != nil {
		return fmt.Errorf("normalize media slot: %w", err)
	}
	start, err := parsePlaylistStart(args.Start)
	if err != nil {
		return err
	}
	pls.Slot = slot
	pls.Loop = zapscript.IsRepeatAll(args.Repeat)
	pls.LoopOne = zapscript.IsRepeatOne(args.Repeat)
	if err := applyRotation(pl, pls, &rotationArgs{
		advance: args.Advance, idle: args.Idle, countdown: args.Countdown,
	}); err != nil {
		return err
	}
	preparePlaylist(env, pls, start)
	return nil
}

// parsePlaylistStart reads the start argument: the item to open on, counted
// from 1, or 0 when it is not given.
func parsePlaylistStart(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	start, err := strconv.Atoi(raw)
	if err != nil || start < 1 {
		return 0, fmt.Errorf("%w: start must be an item number from 1", ErrInvalidPlaylistStart)
	}
	return start, nil
}

// commandPlaylistStart is the start argument of the running command, already
// checked by applyPlaylistArgs when the playlist loaded.
func commandPlaylistStart(env *platforms.CmdEnv) int {
	start, err := parsePlaylistStart(env.Cmd.AdvArgs.Get(zapscript.KeyStart))
	if err != nil {
		return 0
	}
	return start
}

// preparePlaylist opens a loaded playlist on its start item, or where it was
// left last time.
func preparePlaylist(env *platforms.CmdEnv, pls *playlists.Playlist, start int) {
	if env.PlaylistMemory != nil {
		env.PlaylistMemory.Prepare(pls, start)
		return
	}
	if start > 0 {
		pls.Index = min(start-1, max(len(pls.Items)-1, 0))
	}
}

// rotationArgs are the raw advance, idle and countdown values of a command.
type rotationArgs struct {
	advance   string
	idle      string
	countdown string
}

func (r *rotationArgs) any() bool {
	return r.advance != "" || r.idle != "" || r.countdown != ""
}

// rotation is rotationArgs once parsed.
type rotation struct {
	advance      time.Duration
	idle         time.Duration
	countdown    time.Duration
	advanceSet   bool
	idleSet      bool
	countdownSet bool
}

// parseRotationDuration reads one rotation value. Advance and idle must be at
// least a second; a countdown may be zero, which switches without a notice.
func parseRotationDuration(name, raw string, allowZero bool) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %q is not a duration such as 5m", ErrInvalidRotation, name, raw)
	}
	lowest := time.Second
	if allowZero {
		lowest = 0
	}
	if d < lowest || d > maxRotation {
		return 0, fmt.Errorf("%w: %s must be between %s and %s",
			ErrInvalidRotation, name, lowest, maxRotation)
	}
	return d, nil
}

func parseRotation(r *rotationArgs) (rotation, error) {
	var out rotation
	var err error
	if r.advance != "" {
		out.advanceSet = true
		if out.advance, err = parseRotationDuration("advance", r.advance, false); err != nil {
			return rotation{}, err
		}
	}
	if r.idle != "" {
		out.idleSet = true
		if out.idle, err = parseRotationDuration("idle", r.idle, false); err != nil {
			return rotation{}, err
		}
	}
	if out.advanceSet && out.idleSet {
		return rotation{}, ErrRotationConflict
	}
	if r.countdown != "" {
		out.countdownSet = true
		if out.countdown, err = parseRotationDuration("countdown", r.countdown, true); err != nil {
			return rotation{}, err
		}
	}
	return out, nil
}

// applyRotation puts a command's advance, idle and countdown on pls, after
// checking them against the device and slot.
func applyRotation(pl platforms.Platform, pls *playlists.Playlist, r *rotationArgs) error {
	rot, err := parseRotation(r)
	if err != nil {
		return err
	}
	if (rot.advanceSet || rot.idleSet) && pls.Slot == mediaslot.Background {
		return ErrBackgroundRotation
	}
	if rot.advanceSet {
		pls.Advance = rot.advance
	}
	if rot.idleSet {
		pls.Idle = rot.idle
	}
	if rot.countdownSet {
		pls.Countdown = rot.countdown
		pls.CountdownSet = true
	}
	if pls.Idle > 0 && !autoadvance.IdleSupported(pl) {
		return autoadvance.ErrIdleUnsupported
	}
	return nil
}

// rotationOnlyArgs are the advanced arguments that turn launch.random into a
// playlist.
var rotationOnlyArgs = map[string]struct{}{
	string(zapscript.KeyAdvance):   {},
	string(zapscript.KeyIdle):      {},
	string(zapscript.KeyCountdown): {},
}

// isRandomRotation reports whether cmd is a launch.random that makes a
// rotating playlist instead of launching.
func isRandomRotation(cmd *zapscript.Command) bool {
	return cmd.Name == zapscript.ZapScriptCmdLaunchRandom &&
		(cmd.AdvArgs.Get(zapscript.KeyAdvance) != "" || cmd.AdvArgs.Get(zapscript.KeyIdle) != "" ||
			cmd.AdvArgs.Get(zapscript.KeyCountdown) != "")
}

// isQueuedRandom reports whether cmd is a launch.random that only queues a
// playlist, which then launches the item.
func isQueuedRandom(cmd *zapscript.Command) bool {
	return isRandomRotation(cmd) || isDeckRandom(cmd)
}

// randomItemScript is the launch.random command without its rotation, which
// is what each item of the playlist it makes runs.
func randomItemScript(cmd *zapscript.Command) string {
	kept := make(map[string]string, len(cmd.AdvArgs.Raw()))
	for key, value := range cmd.AdvArgs.Raw() {
		if _, drop := rotationOnlyArgs[key]; !drop {
			kept[key] = value
		}
	}
	item := *cmd
	item.AdvArgs = zapscript.NewAdvArgs(kept)
	return item.String()
}

// playRandomRotation runs launch.random with advance, idle or countdown: a
// one-item playlist that repeats, whose item rolls a new random launch each
// time it plays. It is queued the way playlist.play queues a playlist, so
// trust, the command bound and the hold token travel with it identically.
func playRandomRotation(
	pl platforms.Platform, env *platforms.CmdEnv, r *rotationArgs,
) (platforms.CmdResult, error) {
	slot, err := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("normalize media slot: %w", err)
	}
	if slot != mediaslot.Primary {
		return platforms.CmdResult{}, ErrBackgroundRotation
	}
	if r.advance == "" && r.idle == "" {
		return platforms.CmdResult{}, ErrCountdownWithoutRotation
	}

	name := "Random " + env.Cmd.Args[0]
	pls := playlists.NewPlaylist(uuid.NewString(), name, []playlists.PlaylistItem{{
		Name:      name,
		ZapScript: randomItemScript(&env.Cmd),
	}})
	pls.Slot = slot
	pls.Loop = true
	pls.Ephemeral = true
	pls.Unsafe = env.Unsafe
	pls.AllowedCommands = env.AllowedCommands
	if err := applyRotation(pl, pls, r); err != nil {
		return platforms.CmdResult{}, err
	}

	// A new roll every time the command runs, even when the playlist it
	// replaces would open on the same item.
	pls = playlists.Play(*pls)
	pls.ForceRelaunch = true
	if err := queuePlaylistUpdate(env, pls); err != nil {
		return platforms.CmdResult{}, err
	}
	return platforms.CmdResult{PlaylistChanged: true, Playlist: pls}, nil
}

// samePlaylistItem reports whether a move landed on the item already playing.
// A playlist of one wraps back onto it whichever way it moves, and a playlist
// is free to list the same item twice.
func samePlaylistItem(moved, active *playlists.Playlist) bool {
	// Current clamps the playlist it is called on, so the one that is open is
	// read through a copy.
	current := *active
	return len(moved.Items) > 0 && moved.Current() == current.Current()
}

// narrowToCaller limits a playlist that a command moves onto an item to the
// trust and command bound of that command. The item launches with the
// playlist's trust, so without this a caller could pick an item of a playlist
// more trusted than itself and have it run with rights the caller lacks. The
// narrowing sticks to the open playlist: trust only narrows, and owning the
// playlist does not restore what the caller had already lost.
func narrowToCaller(env *platforms.CmdEnv, pls *playlists.Playlist) {
	pls.Unsafe = pls.Unsafe || env.Unsafe
	pls.AllowedCommands = pls.AllowedCommands.Intersect(env.AllowedCommands)
}

// advancePlaylist moves a playlist on to its next item. The queue drops an
// update whose current item and playing state are unchanged, so a move that
// lands where it started has to say outright that it relaunches, or the
// playlist never moves at all.
func advancePlaylist(env *platforms.CmdEnv, active *playlists.Playlist) *playlists.Playlist {
	pls := playlists.Next(*active)
	pls.ForceRelaunch = samePlaylistItem(pls, active)
	narrowToCaller(env, pls)
	return pls
}

// rewindPlaylist moves a playlist back to the item before it, relaunching for
// the same reason advancePlaylist does.
func rewindPlaylist(env *platforms.CmdEnv, active *playlists.Playlist) *playlists.Playlist {
	pls := playlists.Previous(*active)
	pls.ForceRelaunch = samePlaylistItem(pls, active)
	narrowToCaller(env, pls)
	return pls
}

// isActivePlaylist reports whether a freshly loaded playlist is the one
// already open for its slot. A playlist written inline carries whatever ID it
// was given and none at all when it was given none, so an empty ID matches
// nothing: two unnamed playlists are two playlists, not one.
func isActivePlaylist(active, pls *playlists.Playlist) bool {
	return active != nil && pls != nil && pls.ID != "" && active.ID == pls.ID
}

// resumeOrAdvanceActivePlaylist runs playlist.play against the playlist it
// already names. One that is not playing picks up where it left off; one that
// is already playing moves on to the next item and wraps at the end, so a
// token scanned repeatedly walks through its playlist.
//
// The copy just loaded is discarded rather than installed. Its items may be in
// another order, since a shuffled playlist reshuffles every time it loads, and
// "next" only means something against the order the playlist is playing in.
func resumeOrAdvanceActivePlaylist(
	env *platforms.CmdEnv, active *playlists.Playlist,
) (platforms.CmdResult, error) {
	var pls *playlists.Playlist
	if active.Playing {
		pls = advancePlaylist(env, active)
		log.Info().Str("playlist", active.ID).Int("index", pls.Index).
			Msg("advancing active playlist")
	} else {
		pls = playlists.Play(*active)
		narrowToCaller(env, pls)
		log.Info().Str("playlist", active.ID).Int("index", pls.Index).
			Msg("resuming active playlist")
	}

	if err := queuePlaylistUpdate(env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

// playActivePlaylistFrom runs playlist.play with start against the playlist
// it already names: it plays from that item rather than resuming or moving on,
// so a card that always starts from the top does so even while it is open.
func playActivePlaylistFrom(
	env *platforms.CmdEnv, active *playlists.Playlist, start int,
) (platforms.CmdResult, error) {
	pls := playlists.Play(*playlists.Goto(*active, start-1))
	narrowToCaller(env, pls)
	log.Info().Str("playlist", active.ID).Int("index", pls.Index).Msg("playing active playlist from start item")
	if err := queuePlaylistUpdate(env, pls); err != nil {
		return platforms.CmdResult{}, err
	}
	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
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
		narrowToCaller(&env, pls)
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

	// Naming the playlist that is already open acts on it instead of starting
	// it over. Scanning a token again is how you move through its playlist,
	// and it must not throw away the position the playlist is at.
	if isActivePlaylist(active, pls) {
		if start := commandPlaylistStart(&env); start > 0 {
			return playActivePlaylistFrom(&env, active, start)
		}
		return resumeOrAdvanceActivePlaylist(&env, active)
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

	// An idle playlist that is loaded waits for the device to go quiet.
	pls.Armed = pls.Idle > 0
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

		pls.Armed = pls.Idle > 0

		// If loaded playlist matches active, preserve current position
		// unless the command names the item to open on.
		if isActivePlaylist(active, pls) && commandPlaylistStart(&env) == 0 {
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
	var media []*apimodels.UIChoiceMedia
	if env.UI != nil {
		media = playlistChoiceMedia(&env, pls.Items)
	}
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

		zapscript := "**playlist.goto:" + strconv.Itoa(i+1) + "?slot=" + slot + "||**playlist.play?slot=" + slot

		choice := uievents.Choice{
			Label:   name,
			Value:   zapscript,
			Current: i == pls.Index,
		}
		if i < len(media) {
			choice.Media = media[i]
		}
		choices = append(choices, choice)
	}

	log.Info().Any("items", playlistItemsForLog(pls.Items)).Msgf("open playlist: %s", env.Cmd.Args)
	if err := queuePlaylistUpdate(&env, pls); err != nil {
		return platforms.CmdResult{}, err
	}

	result := platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}
	if env.UI == nil {
		log.Warn().Msg("UI event service unavailable, skipping playlist picker")
		return result, nil
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
		log.Warn().Err(openErr).Msg("failed to open playlist picker")
		return result, nil
	}
	go runPickerResult(env.Cfg, handle, client.LocalClient)
	return result, nil
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

	pls := advancePlaylist(&env, active)
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

	pls := rewindPlaylist(&env, active)
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
		log.Debug().Msgf("playlist is already at index %d, not changing", indexArg)
		return platforms.CmdResult{}, nil
	}

	pls := playlists.Goto(*active, newIndex)
	narrowToCaller(&env, pls)
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
		// Stopping a playlist that is not running already has the outcome
		// asked for, so the rest of the script carries on.
		log.Info().Str("slot", slot).Msg("playlist stop: no playlist active")
		return platforms.CmdResult{}, nil
	}

	clearUpdate := &playlists.Playlist{Slot: slot, Clear: true}
	if slot == mediaslot.Background {
		if err := queuePlaylistUpdate(&env, clearUpdate); err != nil {
			return platforms.CmdResult{}, err
		}
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        nil,
		}, nil
	}

	// The launcher is stopped before the playlist is cleared. A clear that
	// was queued first could not be taken back if the stop then reported the
	// media still running, leaving the game up with no playlist attached.
	cancelStop := env.RecordStop(apimodels.MediaStopReasonStopped)
	stopErr := pl.StopActiveLauncher(platforms.StopForMenu)
	if stopErr != nil {
		cancelStop()
	}
	if errors.Is(stopErr, platforms.ErrStopFailed) {
		// The media is still playing, so the playlist that belongs to it
		// stays put rather than being cleared as though it had ended.
		return platforms.CmdResult{}, fmt.Errorf("failed to stop active launcher: %w", stopErr)
	}
	// Stopping the launcher cancels the launcher context, so it can no longer
	// gate the clear: a queue send that watched it would give up on the
	// cancellation this command just caused, report a stop that worked as
	// failed, and could leave the clear unqueued. Only a service shutdown may
	// abandon it now.
	clearEnv := env
	clearEnv.LauncherCtx = nil
	if err := queuePlaylistUpdate(&clearEnv, clearUpdate); err != nil {
		return platforms.CmdResult{}, err
	}
	if stopErr != nil {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        nil,
		}, fmt.Errorf("failed to stop active launcher: %w", stopErr)
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

	if slot == mediaslot.Background ||
		(env.PlaybackManager != nil && env.PlaybackManager.State(slot).Path != "") {
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, nil
	}
	cancelStop := env.RecordStop(apimodels.MediaStopReasonStopped)
	if err := pl.StopActiveLauncher(platforms.StopForMenu); err != nil {
		cancelStop()
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
