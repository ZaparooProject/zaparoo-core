package notifications

import "github.com/ZaparooProject/zaparoo-core/pkg/api/models"

func MediaIndexing(ns chan<- models.Notification, payload models.IndexResponse) {
	ns <- models.Notification{
		Method: models.NotificationMediaIndexing,
		Params: payload,
	}
}

func MediaStopped(ns chan<- models.Notification) {
	ns <- models.Notification{
		Method: models.NotificationStopped,
	}
}

func MediaStarted(ns chan<- models.Notification, payload models.MediaStartedParams) {
	ns <- models.Notification{
		Method: models.NotificationStarted,
		Params: payload,
	}
}

func TokensAdded(ns chan<- models.Notification, payload models.TokenResponse) {
	ns <- models.Notification{
		Method: models.NotificationTokensAdded,
		Params: payload,
	}
}

func TokensRemoved(ns chan<- models.Notification) {
	ns <- models.Notification{
		Method: models.NotificationTokensRemoved,
	}
}

func ReadersAdded(ns chan<- models.Notification, id string) {
	ns <- models.Notification{
		Method: models.NotificationReadersConnected,
		Params: id,
	}
}

func ReadersRemoved(ns chan<- models.Notification, id string) {
	ns <- models.Notification{
		Method: models.NotificationReadersDisconnected,
		Params: id,
	}
}
