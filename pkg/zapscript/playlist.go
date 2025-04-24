package zapscript

import (
	"encoding/json"
	"fmt"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"github.com/rs/zerolog/log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readPlaylistFolder(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("no playlist path specified")
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	media := make([]string, 0)
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) == "" {
			continue
		}

		media = append(media, filepath.Join(path, file.Name()))
	}

	if len(media) == 0 {
		return nil, fmt.Errorf("no media found in: %s", path)
	}

	return media, nil
}

func loadPlaylist(env platforms.CmdEnv) (*playlists.Playlist, error) {
	media, err := readPlaylistFolder(env.Args)
	if err != nil {
		return nil, err
	}

	if v, ok := env.NamedArgs["mode"]; ok && strings.EqualFold(v, "random") {
		log.Info().Msgf("shuffling playlist: %s", env.Args)
		rand.Shuffle(len(media), func(i, j int) {
			media[i], media[j] = media[j], media[i]
		})
	}

	return playlists.NewPlaylist(env.Args, media), nil
}

func cmdPlaylistPlay(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Playlist.Active != nil && env.Args == "" {
		log.Info().Msg("starting paused playlist")
		pls := playlists.Play(*env.Playlist.Active)
		env.Playlist.Queue <- pls
		return platforms.CmdResult{
			PlaylistChanged: true,
			Playlist:        pls,
		}, nil
	}

	pls, err := loadPlaylist(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("media", pls.Media).Msgf("play playlist: %s", env.Args)
	pls = playlists.Play(*pls)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistLoad(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Any("media", pls.Media).Msgf("load playlist: %s", env.Args)
	env.Playlist.Queue <- pls

	return platforms.CmdResult{
		PlaylistChanged: true,
		Playlist:        pls,
	}, nil
}

func cmdPlaylistOpen(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	pls, err := loadPlaylist(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if env.Playlist.Active != nil && env.Playlist.Active.ID == pls.ID {
		log.Debug().Msg("opening active playlist")
		pls.Index = env.Playlist.Active.Index
	}

	log.Info().Any("media", pls.Media).Msgf("open playlist: %s", env.Args)
	env.Playlist.Queue <- pls

	var items []models.ZapScript
	for i, m := range pls.Media {
		name := filepath.Base(m)
		name = strings.TrimSuffix(name, filepath.Ext(name))
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
			Items: items,
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

	index, err := strconv.Atoi(env.Args)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	pls := playlists.Goto(*env.Playlist.Active, index-1)
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
	}, pl.KillLauncher()
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
	}, pl.KillLauncher()
}
