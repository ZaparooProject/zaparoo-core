package zapscript

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
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

	return playlists.NewPlaylist(media), nil
}

func cmdPlaylistPlay(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active != nil && env.Args == "" {
		log.Info().Msg("starting paused playlist")
		env.Playlist.Queue <- playlists.Play(*env.Playlist.Active)
		return nil
	}

	pls, err := loadPlaylist(env)
	if err != nil {
		return err
	}

	log.Info().Any("media", pls.Media).Msgf("play playlist: %s", env.Args)
	env.Playlist.Queue <- playlists.Play(*pls)

	return nil
}

func cmdPlaylistLoad(_ platforms.Platform, env platforms.CmdEnv) error {
	pls, err := loadPlaylist(env)
	if err != nil {
		return err
	}

	log.Info().Any("media", pls.Media).Msgf("load playlist: %s", env.Args)
	env.Playlist.Queue <- pls

	return nil
}

func cmdPlaylistNext(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- playlists.Next(*env.Playlist.Active)

	return nil
}

func cmdPlaylistPrevious(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- playlists.Previous(*env.Playlist.Active)

	return nil
}

func cmdPlaylistGoto(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	index, err := strconv.Atoi(env.Args)
	if err != nil {
		return err
	}

	env.Playlist.Queue <- playlists.Goto(*env.Playlist.Active, index-1)

	return nil
}

func cmdPlaylistStop(pl platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- nil
	return pl.KillLauncher()
}

func cmdPlaylistPause(pl platforms.Platform, env platforms.CmdEnv) error {
	if env.Playlist.Active == nil {
		return fmt.Errorf("no playlist active")
	}

	env.Playlist.Queue <- playlists.Pause(*env.Playlist.Active)
	return pl.KillLauncher()
}
