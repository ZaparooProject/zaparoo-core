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

type HistoryResponseEntry struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	UID     string    `json:"uid"`
	Text    string    `json:"text"`
	Data    string    `json:"data"`
	Success bool      `json:"success"`
}

type HistoryResponse struct {
	Entries []HistoryResponseEntry `json:"entries"`
}

type AllMappingsResponse struct {
	Mappings []MappingResponse `json:"mappings"`
}

type MappingResponse struct {
	ID       string `json:"id"`
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

type IndexingStatusResponse struct {
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

type ActiveMedia struct {
	LauncherID string    `json:"launcherId"`
	SystemID   string    `json:"systemId"`
	SystemName string    `json:"systemName"`
	Path       string    `json:"mediaPath"`
	Name       string    `json:"mediaName"`
	Started    time.Time `json:"started"`
}

func (a *ActiveMedia) Equal(with *ActiveMedia) bool {
	if with == nil {
		return false
	}
	if a.SystemID != with.SystemID {
		return false
	}
	if a.SystemName != with.SystemName {
		return false
	}
	if a.Path != with.Path {
		return false
	}
	if a.Name != with.Name {
		return false
	}
	return true
}

type VersionResponse struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

type MediaResponse struct {
	Database IndexingStatusResponse `json:"database"`
	Active   []ActiveMedia          `json:"active"`
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
