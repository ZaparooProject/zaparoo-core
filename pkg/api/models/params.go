package models

import "encoding/json"

type SearchParams struct {
	Query      string    `json:"query"`
	Systems    *[]string `json:"systems"`
	MaxResults *int      `json:"maxResults"`
}

type MediaIndexParams struct {
	Systems *[]string `json:"systems"`
}

type RunParams struct {
	Type *string `json:"type"`
	UID  *string `json:"uid"`
	Text *string `json:"text"`
	Data *string `json:"data"`
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

const (
	ZapLinkActionZapScript = "zapscript"
	ZapLinkActionMedia     = "media"
)

type ZapLinkAction struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type ZapLink struct {
	Version int             `json:"version"`
	Name    string          `json:"name"`
	Actions []ZapLinkAction `json:"actions"`
}

type ZapScriptParams struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type MediaParams struct {
	Name   string  `json:"name"`
	System string  `json:"system"`
	Url    *string `json:"url"`
}
