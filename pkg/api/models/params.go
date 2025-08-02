package models

import "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"

type SearchParams struct {
	Systems    *[]string `json:"systems"`
	MaxResults *int      `json:"maxResults"`
	Query      string    `json:"query"`
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
	Name      *string               `json:"name"`
	Cmds      []models.ZapScriptCmd `json:"cmds"`
	ZapScript int                   `json:"zapscript"`
	Unsafe    bool                  `json:"unsafe"`
}

type AddMappingParams struct {
	Label    string `json:"label"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
	Enabled  bool   `json:"enabled"`
}

type DeleteMappingParams struct {
	ID int `json:"id"`
}

type UpdateMappingParams struct {
	Label    *string `json:"label"`
	Enabled  *bool   `json:"enabled"`
	Type     *string `json:"type"`
	Match    *string `json:"match"`
	Pattern  *string `json:"pattern"`
	Override *string `json:"override"`
	ID       int     `json:"id"`
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
	ID string `json:"id"`
}

type MediaStartedParams struct {
	SystemID   string `json:"systemId"`
	SystemName string `json:"systemName"`
	MediaPath  string `json:"mediaPath"`
	MediaName  string `json:"mediaName"`
}

type UpdateActiveMediaParams struct {
	SystemID  string `json:"systemId"`
	MediaPath string `json:"mediaPath"`
	MediaName string `json:"mediaName"`
}
