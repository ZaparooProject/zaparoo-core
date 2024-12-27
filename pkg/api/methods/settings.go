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
		RunZapScript:            env.State.CanRunZapScript(),
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
		env.Config.SetAutoConnect(*params.ReadersAutoDetect)
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
