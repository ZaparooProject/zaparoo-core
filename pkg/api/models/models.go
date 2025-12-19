// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package models

import (
	"encoding/json"
)

const (
	NotificationReadersConnected     = "readers.added"
	NotificationReadersDisconnected  = "readers.removed"
	NotificationRunning              = "running"
	NotificationTokensAdded          = "tokens.added"
	NotificationTokensRemoved        = "tokens.removed"
	NotificationStopped              = "media.stopped"
	NotificationStarted              = "media.started"
	NotificationMediaIndexing        = "media.indexing" // TODO: rename to generating
	NotificationPlaytimeLimitReached = "playtime.limit.reached"
	NotificationPlaytimeLimitWarning = "playtime.limit.warning"
	NotificationInboxAdded           = "inbox.added"
)

const (
	PlaytimeLimitReasonSession = "session"
	PlaytimeLimitReasonDaily   = "daily"
)

const (
	MethodLaunch               = "launch" // DEPRECATED
	MethodRun                  = "run"
	MethodRunScript            = "run.script"
	MethodStop                 = "stop"
	MethodTokens               = "tokens"
	MethodMedia                = "media"
	MethodMediaGenerate        = "media.generate"
	MethodMediaGenerateCancel  = "media.generate.cancel"
	MethodMediaIndex           = "media.index" // DEPRECATED
	MethodMediaSearch          = "media.search"
	MethodMediaTags            = "media.tags"
	MethodMediaActive          = "media.active"
	MethodMediaActiveUpdate    = "media.active.update"
	MethodSettings             = "settings"
	MethodSettingsUpdate       = "settings.update"
	MethodSettingsReload       = "settings.reload"
	MethodSettingsLogsDownload = "settings.logs.download"
	MethodPlaytimeLimits       = "settings.playtime.limits"
	MethodPlaytimeLimitsUpdate = "settings.playtime.limits.update"
	MethodPlaytime             = "playtime"
	MethodClients              = "clients"
	MethodClientsNew           = "clients.new"
	MethodClientsDelete        = "clients.delete"
	MethodSystems              = "systems"
	MethodLaunchersRefresh     = "launchers.refresh"
	MethodHistory              = "tokens.history"
	MethodMappings             = "mappings"
	MethodMappingsNew          = "mappings.new"
	MethodMappingsDelete       = "mappings.delete"
	MethodMappingsUpdate       = "mappings.update"
	MethodMappingsReload       = "mappings.reload"
	MethodReaders              = "readers"
	MethodReadersWrite         = "readers.write"
	MethodReadersWriteCancel   = "readers.write.cancel"
	MethodVersion              = "version"
	MethodHealthCheck          = "health"
	MethodInbox                = "inbox"
	MethodInboxDelete          = "inbox.delete"
	MethodInboxClear           = "inbox.clear"
)

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type RequestObject struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RPCID           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ErrorObject struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type ResponseObject struct {
	Result  any          `json:"result"`
	Error   *ErrorObject `json:"error,omitempty"`
	JSONRPC string       `json:"jsonrpc"`
	ID      RPCID        `json:"id"`
}

// ResponseErrorObject exists for sending errors, so we can omit result from
// the response, but so nil responses are still returned when using the main
// ResponseObject.
type ResponseErrorObject struct {
	Error   *ErrorObject `json:"error"`
	JSONRPC string       `json:"jsonrpc"`
	ID      RPCID        `json:"id"`
}
