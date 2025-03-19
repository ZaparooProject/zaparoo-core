package models

import "github.com/ZaparooProject/zaparoo-core/pkg/api/models"

type NoticeArgs struct {
	Text     string `json:"text"`
	Timeout  int    `json:"timeout"`
	Complete string `json:"complete"`
}

type PickerArgs struct {
	Actions []models.ZapLinkAction `json:"actions"`
	Title   string                 `json:"title"`
	Timeout int                    `json:"timeout"`
	Trusted *bool                  `json:"trusted"`
}
