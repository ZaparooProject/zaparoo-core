package methods

import (
	"encoding/json"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"path/filepath"
)

func HandleSettings(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings request")

	resp := models.SettingsResponse{
		RunZapScript:            env.State.RunZapScriptEnabled(),
		DebugLogging:            env.Config.DebugLogging(),
		AudioScanFeedback:       env.Config.AudioFeedback(),
		ReadersAutoDetect:       env.Config.Readers().AutoDetect,
		ReadersScanMode:         env.Config.ReadersScan().Mode,
		ReadersScanExitDelay:    env.Config.ReadersScan().ExitDelay,
		ReadersScanIgnoreSystem: make([]string, 0),
	}

	for _, s := range env.Config.ReadersScan().IgnoreSystem {
		resp.ReadersScanIgnoreSystem = append(
			resp.ReadersScanIgnoreSystem,
			s,
		)
	}

	return resp, nil
}

func HandleSettingsReload(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings reload request")

	err := env.Config.Load()
	if err != nil {
		log.Error().Err(err).Msg("error loading settings")
		return nil, errors.New("error loading settings")
	}

	mapDir := filepath.Join(utils.DataDir(env.Platform), platforms.MappingsDir)
	err = env.Config.LoadMappings(mapDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading mappings")
		return nil, errors.New("error loading mappings")
	}

	launchersDir := filepath.Join(utils.DataDir(env.Platform), platforms.LaunchersDir)
	err = env.Config.LoadCustomLaunchers(launchersDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading custom launchers")
		return nil, errors.New("error loading custom launchers")
	}

	return nil, nil
}

func HandleSettingsUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings update request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.UpdateSettingsParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	if params.RunZapScript != nil {
		log.Info().Bool("runZapScript", *params.RunZapScript).Msg("update")
		if *params.RunZapScript {
			env.State.SetRunZapScript(true)
		} else {
			env.State.SetRunZapScript(false)
		}
	}

	if params.DebugLogging != nil {
		log.Info().Bool("debugLogging", *params.DebugLogging).Msg("update")
		env.Config.SetDebugLogging(*params.DebugLogging)
	}

	if params.AudioScanFeedback != nil {
		log.Info().Bool("audioScanFeedback", *params.AudioScanFeedback).Msg("update")
		env.Config.SetAudioFeedback(*params.AudioScanFeedback)
	}

	if params.ReadersAutoDetect != nil {
		log.Info().Bool("readersAutoDetect", *params.ReadersAutoDetect).Msg("update")
		env.Config.SetAutoDetect(*params.ReadersAutoDetect)
	}

	if params.ReadersScanMode != nil {
		log.Info().Str("readersScanMode", *params.ReadersScanMode).Msg("update")
		if *params.ReadersScanMode == "" {
			env.Config.SetScanMode(config.ScanModeTap)
		} else if *params.ReadersScanMode == config.ScanModeTap || *params.ReadersScanMode == config.ScanModeHold {
			env.Config.SetScanMode(*params.ReadersScanMode)
		} else {
			return nil, ErrInvalidParams
		}
	}

	if params.ReadersScanExitDelay != nil {
		log.Info().Float32("readersScanExitDelay", *params.ReadersScanExitDelay).Msg("update")
		env.Config.SetScanExitDelay(*params.ReadersScanExitDelay)
	}

	if params.ReadersScanIgnoreSystem != nil {
		log.Info().Strs("readsScanIgnoreSystem", *params.ReadersScanIgnoreSystem).Msg("update")
		env.Config.SetScanIgnoreSystem(*params.ReadersScanIgnoreSystem)
	}

	return nil, env.Config.Save()
}
