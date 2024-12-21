package methods

import (
	"encoding/json"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
)

func HandleSettings(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings request")

	resp := models.SettingsResponse{
		AudioFeedback:   env.Config.AudioFeedback(),
		DebugLogging:    env.Config.DebugLogging(),
		LaunchingActive: !env.State.IsLauncherDisabled(),
	}

	return resp, nil
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

	if params.AudioFeedback != nil {
		log.Info().Bool("audioFeedback", *params.AudioFeedback).Msg("update")
		env.Config.SetAudioFeedback(*params.AudioFeedback)
	}

	if params.DebugLogging != nil {
		log.Info().Bool("debugLogging", *params.DebugLogging).Msg("update")
		env.Config.SetDebugLogging(*params.DebugLogging)
	}

	if params.LaunchingActive != nil {
		log.Info().Bool("launchingActive", *params.LaunchingActive).Msg("update")
		if *params.LaunchingActive {
			env.State.EnableLauncher()
		} else {
			env.State.DisableLauncher()
		}
	}

	return nil, env.Config.Save()
}

func HandleSettingsReaders(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings request")

	resp := models.SettingsReaders{
		AutoDetect: env.Config.Readers().AutoDetect,
	}

	return resp, nil
}

func HandleSettingsReadersUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings update request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.UpdateSettingsReadersParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	if params.AutoDetect != nil {
		log.Info().Bool("autoDetect", *params.AutoDetect).Msg("update")
		env.Config.SetAutoConnect(*params.AutoDetect)
	}

	return nil, env.Config.Save()
}

func HandleSettingsReadersScan(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings request")

	rs := env.Config.ReadersScan()

	resp := models.SettingsReadersScan{
		Mode:         rs.Mode,
		ExitDelay:    rs.ExitDelay,
		IgnoreSystem: make([]string, 0),
	}

	resp.IgnoreSystem = append(
		resp.IgnoreSystem,
		rs.IgnoreSystem...,
	)

	return resp, nil
}

func HandleSettingsReadersScanUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received settings update request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.UpdateSettingsReadersScanParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	if params.Mode != nil {
		log.Info().Str("mode", *params.Mode).Msg("update")
		if *params.Mode == "" {
			return nil, ErrInvalidParams
		} else if *params.Mode == config.ScanModeTap || *params.Mode == config.ScanModeHold {
			env.Config.SetScanMode(*params.Mode)
		} else {
			return nil, ErrInvalidParams
		}
	}

	if params.ExitDelay != nil {
		log.Info().Float32("exitDelay", *params.ExitDelay).Msg("update")
		env.Config.SetScanExitDelay(*params.ExitDelay)
	}

	if params.IgnoreSystem != nil {
		log.Info().Strs("ignoreSystem", *params.IgnoreSystem).Msg("update")
		env.Config.SetScanIgnoreSystem(*params.IgnoreSystem)
	}

	return nil, env.Config.Save()
}
