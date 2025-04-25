package models

import (
	"github.com/google/uuid"
	"time"
)

type SearchResultMedia struct {
	System System `json:"system"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

type SearchResults struct {
	Results []SearchResultMedia `json:"results"`
	Total   int                 `json:"total"`
}

type SettingsResponse struct {
	RunZapScript            bool     `json:"runZapScript"`
	DebugLogging            bool     `json:"debugLogging"`
	AudioScanFeedback       bool     `json:"audioScanFeedback"`
	ReadersAutoDetect       bool     `json:"readersAutoDetect"`
	ReadersScanMode         string   `json:"readersScanMode"`
	ReadersScanExitDelay    float32  `json:"readersScanExitDelay"`
	ReadersScanIgnoreSystem []string `json:"readersScanIgnoreSystems"`
}

type System struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type SystemsResponse struct {
	Systems []System `json:"systems"`
}

type HistoryReponseEntry struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	UID     string    `json:"uid"`
	Text    string    `json:"text"`
	Data    string    `json:"data"`
	Success bool      `json:"success"`
}

type HistoryResponse struct {
	Entries []HistoryReponseEntry `json:"entries"`
}

type AllMappingsResponse struct {
	Mappings []MappingResponse `json:"mappings"`
}

type MappingResponse struct {
	Id       string `json:"id"`
	Added    string `json:"added"`
	Label    string `json:"label"`
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
}

type TokenResponse struct {
	Type     string    `json:"type"`
	UID      string    `json:"uid"`
	Text     string    `json:"text"`
	Data     string    `json:"data"`
	ScanTime time.Time `json:"scanTime"`
}

type IndexResponse struct {
	Exists             bool    `json:"exists"`
	Indexing           bool    `json:"indexing"`
	TotalSteps         *int    `json:"totalSteps,omitempty"`
	CurrentStep        *int    `json:"currentStep,omitempty"`
	CurrentStepDisplay *string `json:"currentStepDisplay,omitempty"`
	TotalFiles         *int    `json:"totalFiles,omitempty"`
}

type ReaderResponse struct {
	Connected bool   `json:"connected"`
	Driver    string `json:"driver"`
	Path      string `json:"path"`
}

type PlayingResponse struct {
	SystemId   string `json:"systemId"`
	SystemName string `json:"systemName"`
	MediaPath  string `json:"mediaPath"`
	MediaName  string `json:"mediaName"`
}

type VersionResponse struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

type MediaResponse struct {
	Database IndexResponse     `json:"database"`
	Active   []PlayingResponse `json:"active"`
}

type TokensResponse struct {
	Active []TokenResponse `json:"active"`
	Last   *TokenResponse  `json:"last,omitempty"`
}

type ClientResponse struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Secret  string    `json:"secret"`
}
