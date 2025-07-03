package zapscript

import (
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"github.com/rs/zerolog/log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const plsHeader = "[playlist]"

func isPlsFile(path string) bool {
	return filepath.Ext(strings.ToLower(path)) == ".pls"
}

var plsFileRe = regexp.MustCompile("^File([1-9]\\d*)\\s*=\\s*(.*)$")
var plsTitleRe = regexp.MustCompile("^Title([1-9]\\d*)\\s*=\\s*(.*)$")

type plsEntry struct {
	file  string
	title string
}

type ArgPlaylistEntry struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type ArgPlaylist struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Entries []ArgPlaylistEntry `json:"entries"`
}

func readPlsFile(path string) ([]playlists.PlaylistEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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
	entries := make(map[int]plsEntry)

	updateEntry := func(idx int, file, title string) {
		entry := entries[idx]
		if file != "" {
			entry.file = file
		}
		if title != "" {
			entry.title = title
		}
		entries[idx] = entry
	}

	for _, line := range lines {
		if !hasHeader {
			if line == plsHeader {
				hasHeader = true
			}
			continue
		}

		if matches := plsFileRe.FindStringSubmatch(line); len(matches) == 3 {
			entryID := matches[1]
			file := matches[2]

			id, err := strconv.Atoi(entryID)
			if err != nil {
				log.Warn().Msgf("invalid file id in pls file: %s", path)
				continue
			}

			updateEntry(id, file, "")
			continue
		}

		if matches := plsTitleRe.FindStringSubmatch(line); len(matches) == 3 {
			entryID := matches[1]
			title := matches[2]

			id, err := strconv.Atoi(entryID)
			if err != nil {
				log.Warn().Msgf("invalid title id in pls file: %s", path)
				continue
			}

			updateEntry(id, "", title)
			continue
		}

		log.Warn().Msgf("unrecognized line in pls file: %s", line)
	}

	if !hasHeader {
		log.Warn().Msgf("no header found in pls file: %s", path)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no entries found in pls file: %s", path)
	}

	media := make([]playlists.PlaylistEntry, 0)

	// sort items by number in fileX/titleX
	sorted := make([]int, 0, len(entries))
	for k := range entries {
		sorted = append(sorted, k)
	}
	sort.Ints(sorted)

	for _, k := range sorted {
		entry := entries[k]
		if entry.file == "" {
			continue
		}

		if !filepath.IsAbs(entry.file) {
			// if it's a relative path, do a basic check to see if it
			// exists, and expand it to an absolute path
			exists := false
			// just the current dir
			testFile := filepath.Base(entry.file)

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
				entry.file = absPath
			}
		}

		media = append(media, playlists.PlaylistEntry{
			Name:      entry.title,
			ZapScript: entry.file,
		})
	}

	return media, nil
}

func readPlaylistFolder(path string) ([]playlists.PlaylistEntry, error) {
	if path == "" {
		return nil, fmt.Errorf("no playlist path specified")
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	dir, err := os.ReadDir(path)
	if err != nil {
		return nil, err
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

	media := make([]playlists.PlaylistEntry, 0)
	for _, file := range files {
		name := filepath.Base(file)
		name = strings.TrimSuffix(name, filepath.Ext(name))
		media = append(media, playlists.PlaylistEntry{
			Name:      name,
			ZapScript: file,
		})
	}

	return media, nil
}

func loadPlaylist(pl platforms.Platform, env platforms.CmdEnv) (*playlists.Playlist, error) {
	if len(env.Cmd.Args) == 0 {
		return nil, ErrArgCount
	} else if env.Cmd.Args[0] == "" {
		return nil, ErrRequiredArgs
	}

	if utils.MaybeJSON([]byte(env.Cmd.Args[0])) {
		var plsArg ArgPlaylist
		if err := json.Unmarshal([]byte(env.Cmd.Args[0]), &plsArg); err != nil {
			return nil, fmt.Errorf("invalid playlist json: %w", err)
		}

		var media []playlists.PlaylistEntry
		for _, entry := range plsArg.Entries {
			media = append(media, playlists.PlaylistEntry{
				Name:      entry.Name,
				ZapScript: entry.ZapScript,
			})
		}

		return playlists.NewPlaylist(plsArg.ID, plsArg.Name, media), nil
	}

	path, err := findFile(pl, env.Cfg, env.Cmd.Args[0])
	if err != nil {
		return nil, err
	}

	var media []playlists.PlaylistEntry
	if isPlsFile(path) {
		media, err = readPlsFile(path)
		if err != nil {
			return nil, err
		}
	} else {
		media, err = readPlaylistFolder(path)
		if err != nil {
			return nil, err
		}
	}

	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	if v, ok := env.Cmd.AdvArgs["mode"]; ok && strings.EqualFold(v, "shuffle") {
		log.Info().Msgf("shuffling playlist: %s", env.Cmd.Args[0])
		if len(media) == 0 {
			log.Warn().Msgf("playlist is empty: %s", path)
		} else {
			rand.Shuffle(len(media), func(i, j int) {
				media[i], media[j] = media[j], media[i]
			})
		}
	}

	return playlists.NewPlaylist(env.Cmd.Args[0], name, media), nil
}

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

	log.Info().Any("media", pls.Entries).Msgf("play playlist: %v", env.Cmd.Args)
	pls = playlists.Play(*pls)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistLoad(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("media", pls.Entries).Msgf("load playlist: %s", env.Cmd.Args)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistOpen(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if env.Playlist.Active != nil && env.Playlist.Active.ID == pls.ID {
		log.Debug().Msg("opening active playlist")
		pls.Index = env.Playlist.Active.Index
	}

	log.Info().Any("media", pls.Entries).Msgf("open playlist: %s", env.Cmd.Args)
	env.Playlist.Queue <- pls

	var items []models.ZapScript
	for i, m := range pls.Entries {
		var name string

		if m.Name == "" {
			name = filepath.Base(m.ZapScript)
			name = strings.TrimSuffix(name, filepath.Ext(name))
		} else {
			name = m.Name
		}

		if i == pls.Index {
			name = fmt.Sprintf("* %s", name)
		}

		args := models.CmdEvaluateArgs{
			ZapScript: "**playlist.goto:" + strconv.Itoa(i+1) + "||**playlist.play",
		}
		rawArgs, err := json.Marshal(args)
		if err != nil {
			log.Error().Err(err).Msgf("marshaling playlist picker launch args")
			continue
		}

		items = append(items, models.ZapScript{
			ZapScript: 1,
			Name:      &name,
			Cmds: []models.ZapScriptCmd{
				{
					Cmd:  models.ZapScriptCmdEvaluate,
					Args: rawArgs,
				},
			},
		})
	}

	return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, pl.ShowPicker(env.Cfg, widgetModels.PickerArgs{
			Title:    pls.Name,
			Items:    items,
			Selected: pls.Index,
		})
}

func cmdPlaylistNext(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, fmt.Errorf("no playlist active")
	}

	pls := playlists.Next(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistPrevious(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, fmt.Errorf("no playlist active")
	}

	pls := playlists.Previous(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistGoto(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, fmt.Errorf("no playlist active")
	}

	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	indexArg, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, err
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

func cmdPlaylistStop(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- nil

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        nil,
	}, pl.StopActiveLauncher()
}

func cmdPlaylistPause(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active == nil {
		return platforms.CmdResult{}, fmt.Errorf("no playlist active")
	}

	pls := playlists.Pause(*env.Playlist.Active)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, pl.StopActiveLauncher()
}
