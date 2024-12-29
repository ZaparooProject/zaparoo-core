package methods

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/rs/zerolog/log"
)

func newStatus(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
) models.StatusResponse {
	active := st.GetActiveCard()
	last := st.GetLastScanned()

	readerConnected, readerType := false, ""

	rs := st.ListReaders()
	if len(rs) > 0 {
		// TODO: listing all readers will break API compatibility
		reader, ok := st.GetReader(rs[0])
		if ok && reader != nil {
			readerConnected = reader.Connected()
			readerType = reader.Info()
		}
	}

	readers := make([]models.ReaderResponse, 0)
	for _, device := range rs {
		reader, ok := st.GetReader(device)
		if ok && reader != nil {
			readers = append(readers, models.ReaderResponse{
				Connected: reader.Connected(),
				Device:    device,
				Info:      reader.Info(),
			})
		}
	}

	return models.StatusResponse{
		Launching: st.RunZapScriptEnabled(),
		Readers:   readers,
		Reader: models.ReaderStatusResponse{
			Connected: readerConnected,
			Type:      readerType,
		},
		ActiveToken: models.TokenResponse{
			Type:     active.Type,
			UID:      active.UID,
			Text:     active.Text,
			Data:     active.Data,
			ScanTime: active.ScanTime,
		},
		LastToken: models.TokenResponse{
			Type:     last.Type,
			UID:      last.UID,
			Text:     last.Text,
			Data:     last.Data,
			ScanTime: last.ScanTime,
		},
		GamesIndex: models.IndexResponse{
			Exists:      IndexInstance.Exists(pl),
			Indexing:    IndexInstance.Indexing,
			TotalSteps:  IndexInstance.TotalSteps,
			CurrentStep: IndexInstance.CurrentStep,
			CurrentDesc: IndexInstance.CurrentDesc,
			TotalFiles:  IndexInstance.TotalFiles,
		},
		Playing: models.PlayingResponse{
			System:     pl.ActiveSystem(),
			SystemName: pl.ActiveSystem(),
			Game:       pl.ActiveGame(),
			GameName:   pl.ActiveGameName(),
			GamePath:   pl.NormalizePath(cfg, pl.ActiveGamePath()),
		},
	}
}

func HandleStatus(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received status request")
	status := newStatus(env.Platform, env.Config, env.State)
	return status, nil
}

func HandleVersion(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received version request")
	return models.VersionResponse{
		Version:  config.AppVersion,
		Platform: env.Platform.Id(),
	}, nil
}
