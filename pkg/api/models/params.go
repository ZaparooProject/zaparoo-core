package models

import "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

type SearchParams struct {
	Query      string    `json:"query"`
	Systems    *[]string `json:"systems"`
	MaxResults *int      `json:"maxResults"`
}

type MediaIndexParams struct {
	Systems *[]string `json:"systems"`
}

type RunParams struct {
	Type   *string `json:"type"`
	UID    *string `json:"uid"`
	Text   *string `json:"text"`
	Data   *string `json:"data"`
	Unsafe bool    `json:"unsafe"`
}

type RunScriptParams struct {
	ZapScript int                   `json:"zapscript"`
	Name      *string               `json:"name"`
	Cmds      []models.ZapScriptCmd `json:"cmds"`
	Unsafe    bool                  `json:"unsafe"`
}

type AddMappingParams struct {
	Label    string `json:"label"`
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
}

type DeleteMappingParams struct {
	Id int `json:"id"`
}

type UpdateMappingParams struct {
	Id       int     `json:"id"`
	Label    *string `json:"label"`
	Enabled  *bool   `json:"enabled"`
	Type     *string `json:"type"`
	Match    *string `json:"match"`
	Pattern  *string `json:"pattern"`
	Override *string `json:"override"`
}

type ReaderWriteParams struct {
	Text string `json:"text"`
}

type UpdateSettingsParams struct {
	RunZapScript            *bool     `json:"runZapScript"`
	DebugLogging            *bool     `json:"debugLogging"`
	AudioScanFeedback       *bool     `json:"audioScanFeedback"`
	ReadersAutoDetect       *bool     `json:"readersAutoDetect"`
	ReadersScanMode         *string   `json:"readersScanMode"`
	ReadersScanExitDelay    *float32  `json:"readersScanExitDelay"`
	ReadersScanIgnoreSystem *[]string `json:"readersScanIgnoreSystems"`
}

type NewClientParams struct {
	Name string `json:"name"`
}

type DeleteClientParams struct {
	Id string `json:"id"`
}

type MediaStartedParams struct {
	SystemId   string `json:"systemId"`
	SystemName string `json:"systemName"`
	MediaPath  string `json:"mediaPath"`
	MediaName  string `json:"mediaName"`
}
