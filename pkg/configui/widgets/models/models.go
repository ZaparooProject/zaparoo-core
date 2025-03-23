package models

import "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

type NoticeArgs struct {
	Text     string `json:"text"`
	Timeout  int    `json:"timeout"`
	Complete string `json:"complete"`
}

type PickerArgs struct {
	Cmds    []models.ZapScriptCmd `json:"commands"`
	Title   string                `json:"title"`
	Timeout int                   `json:"timeout"`
	Trusted *bool                 `json:"trusted"`
}
