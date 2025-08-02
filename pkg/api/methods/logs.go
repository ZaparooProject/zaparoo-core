package methods

import (
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
)

func HandleLogsDownload(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received logs download request")

	logFilePath := filepath.Join(env.Platform.Settings().TempDir, config.LogFile)

	data, err := os.ReadFile(logFilePath)
	if err != nil {
		log.Error().Err(err).Str("path", logFilePath).Msg("failed to read log file")
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return models.LogDownloadResponse{
		Filename: config.LogFile,
		Size:     len(data),
		Content:  encoded,
	}, nil
}
