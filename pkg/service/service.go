/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023, 2024 Callan Barrett

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

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/userdb/boltmigration"
	"github.com/ZaparooProject/zaparoo-core/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

func setupEnvironment(pl platforms.Platform) error {
	if _, ok := utils.HasUserDir(); ok {
		log.Info().Msg("using 'user' directory for storage")
	}

	log.Info().Msg("creating platform directories")
	dirs := []string{
		utils.ConfigDir(pl),
		pl.Settings().TempDir,
		utils.DataDir(pl),
		filepath.Join(utils.DataDir(pl), config.MappingsDir),
		filepath.Join(utils.DataDir(pl), config.AssetsDir),
		filepath.Join(utils.DataDir(pl), config.LaunchersDir),
		filepath.Join(utils.DataDir(pl), config.MediaDir),
	}
	for _, dir := range dirs {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			return err
		}
	}

	successSoundPath := filepath.Join(
		utils.DataDir(pl),
		config.AssetsDir,
		config.SuccessSoundFilename,
	)
	if _, err := os.Stat(successSoundPath); err != nil {
		// copy success sound to temp
		sf, err := os.Create(successSoundPath)
		if err != nil {
			log.Error().Msgf("error creating success sound file: %s", err)
		}
		_, err = sf.Write(assets.SuccessSound)
		if err != nil {
			log.Error().Msgf("error writing success sound file: %s", err)
		}
		_ = sf.Close()
	}

	failSoundPath := filepath.Join(
		utils.DataDir(pl),
		config.AssetsDir,
		config.FailSoundFilename,
	)
	if _, err := os.Stat(failSoundPath); err != nil {
		// copy fail sound to temp
		ff, err := os.Create(failSoundPath)
		if err != nil {
			log.Error().Msgf("error creating fail sound file: %s", err)
		}
		_, err = ff.Write(assets.FailSound)
		if err != nil {
			log.Error().Msgf("error writing fail sound file: %s", err)
		}
		_ = ff.Close()
	}

	return nil
}

func makeDatabase(pl platforms.Platform) (*database.Database, error) {
	db := &database.Database{
		MediaDB: nil,
		UserDB:  nil,
	}

	mediaDB, err := mediadb.OpenMediaDB(pl)
	if err != nil {
		return db, err
	}

	err = mediaDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating mediadb: %w", err)
	}

	db.MediaDB = mediaDB

	userDB, err := userdb.OpenUserDB(pl)
	if err != nil {
		return db, err
	}

	err = userDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating userdb: %w", err)
	}

	db.UserDB = userDB

	// migrate old boltdb mappings if required
	err = boltmigration.MaybeMigrate(pl, userDB)
	if err != nil {
		log.Error().Err(err).Msg("error migrating old boltdb mappings")
	}

	return db, nil
}

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (func() error, error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl) // global state, notification queue
	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)        // input token queue
	lsq := make(chan *tokens.Token)       // launch software queue
	plq := make(chan *playlists.Playlist) // playlist event queue

	err := setupEnvironment(pl)
	if err != nil {
		log.Error().Err(err).Msg("error setting up environment")
		return nil, err
	}

	log.Info().Msg("running platform pre start")
	err = pl.StartPre(cfg)
	if err != nil {
		log.Error().Err(err).Msg("platform start pre error")
		return nil, err
	}

	log.Info().Msg("opening databases")
	db, err := makeDatabase(pl)
	if err != nil {
		log.Error().Err(err).Msgf("error opening databases")
		return nil, err
	}

	log.Info().Msg("loading mapping files")
	err = cfg.LoadMappings(filepath.Join(utils.DataDir(pl), config.MappingsDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading mapping files")
	}

	log.Info().Msg("loading custom launchers")
	err = cfg.LoadCustomLaunchers(filepath.Join(utils.DataDir(pl), config.LaunchersDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading custom launchers")
	}

	log.Info().Msg("starting API service")
	go api.Start(pl, cfg, st, itq, db, ns)

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		go groovyproxy.Start(cfg, st, itq)
	}

	log.Info().Msg("starting reader manager")
	go readerManager(pl, cfg, st, db, itq, lsq, plq)

	log.Info().Msg("starting input token queue manager")
	go processTokenQueue(pl, cfg, st, itq, db, lsq, plq)

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.ActiveMedia, st.SetActiveMedia)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		return nil, err
	}

	return func() error {
		err = pl.Stop()
		if err != nil {
			log.Warn().Msgf("error stopping platform: %s", err)
		}
		st.StopService()
		close(plq)
		close(lsq)
		close(itq)
		return nil
	}, nil
}
