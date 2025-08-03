package notifications

import (
	"encoding/json"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/rs/zerolog/log"
)

func sendNotification(ns chan<- models.Notification, method string, payload any) {
	log.Debug().Msgf("sending notification: %s, %v", method, payload)
	if payload != nil {
		params, err := json.Marshal(payload)
		if err != nil {
			log.Error().Err(err).Msgf("error marshalling notification params: %s", method)
			return
		}
		ns <- models.Notification{
			Method: method,
			Params: params,
		}
	} else {
		ns <- models.Notification{
			Method: method,
		}
	}
}

func MediaIndexing(ns chan<- models.Notification, payload models.IndexingStatusResponse) {
	sendNotification(ns, models.NotificationMediaIndexing, payload)
}

func MediaStopped(ns chan<- models.Notification) {
	sendNotification(ns, models.NotificationStopped, nil)
}

func MediaStarted(ns chan<- models.Notification, payload models.MediaStartedParams) {
	sendNotification(ns, models.NotificationStarted, payload)
}

//nolint:gocritic // single-use parameter in notification
func TokensAdded(ns chan<- models.Notification, payload models.TokenResponse) {
	sendNotification(ns, models.NotificationTokensAdded, payload)
}

func TokensRemoved(ns chan<- models.Notification) {
	sendNotification(ns, models.NotificationTokensRemoved, nil)
}

func ReadersAdded(ns chan<- models.Notification, payload models.ReaderResponse) {
	sendNotification(ns, models.NotificationReadersConnected, payload)
}

func ReadersRemoved(ns chan<- models.Notification, payload models.ReaderResponse) {
	sendNotification(ns, models.NotificationReadersDisconnected, payload)
}
