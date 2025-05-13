package models

import (
	"encoding/json"
	"github.com/google/uuid"
)

const (
	NotificationReadersConnected    = "readers.added"
	NotificationReadersDisconnected = "readers.removed"
	NotificationRunning             = "running"
	NotificationTokensAdded         = "tokens.added"
	NotificationTokensRemoved       = "tokens.removed"
	NotificationStopped             = "media.stopped"
	NotificationStarted             = "media.started"
	NotificationMediaIndexing       = "media.indexing"
)

const (
	MethodLaunch            = "launch" // DEPRECATED
	MethodRun               = "run"
	MethodRunScript         = "run.script"
	MethodStop              = "stop"
	MethodTokens            = "tokens"
	MethodMedia             = "media"
	MethodMediaGenerate     = "media.generate"
	MethodMediaIndex        = "media.index" // DEPRECATED
	MethodMediaSearch       = "media.search"
	MethodMediaActive       = "media.active"
	MethodMediaActiveUpdate = "media.active.update"
	MethodSettings          = "settings"
	MethodSettingsUpdate    = "settings.update"
	MethodSettingsReload    = "settings.reload"
	MethodClients           = "clients"
	MethodClientsNew        = "clients.new"
	MethodClientsDelete     = "clients.delete"
	MethodSystems           = "systems"
	MethodHistory           = "tokens.history"
	MethodMappings          = "mappings"
	MethodMappingsNew       = "mappings.new"
	MethodMappingsDelete    = "mappings.delete"
	MethodMappingsUpdate    = "mappings.update"
	MethodMappingsReload    = "mappings.reload"
	MethodReadersWrite      = "readers.write"
	MethodVersion           = "version"
)

type Notification struct {
	Method string
	Params json.RawMessage
}

type RequestObject struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uuid.UUID      `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponseObject struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      uuid.UUID    `json:"id"`
	Result  any          `json:"result"`
	Error   *ErrorObject `json:"error,omitempty"`
}

// ResponseErrorObject exists for sending errors, so we can omit result from
// the response, but so nil responses are still returned when using the main
// ResponseObject.
type ResponseErrorObject struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      uuid.UUID    `json:"id"`
	Error   *ErrorObject `json:"error"`
}
