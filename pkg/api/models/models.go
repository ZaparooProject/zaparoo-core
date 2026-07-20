// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"strings"
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
	NotificationMediaScraping        = "media.scraping"
	NotificationTokensStaged         = "tokens.staged"
	NotificationTokensStagedReady    = "tokens.staged.ready" //nolint:gosec // not a credential
	NotificationPlaytimeLimitReached = "playtime.limit.reached"
	NotificationPlaytimeLimitWarning = "playtime.limit.warning"
	NotificationInboxAdded           = "inbox.added"
	NotificationClientsPaired        = "clients.paired"
	NotificationProfilesActive       = "profiles.active"
	NotificationProfilesData         = "profiles.data"
	NotificationUIChanged            = "ui.changed"
	NotificationAuthLinkStatus       = "auth.link.status"
)

// Profile data swap statuses reported by the profiles.data notification.
const (
	ProfilesDataApplied     = "applied"
	ProfilesDataDeferred    = "deferred"
	ProfilesDataFailed      = "failed"
	ProfilesDataUnavailable = "unavailable"
)

const (
	PlaytimeLimitReasonSession = "session"
	PlaytimeLimitReasonDaily   = "daily"
)

type UIEventKind string

const (
	UIEventKindNotice  UIEventKind = "notice"
	UIEventKindLoader  UIEventKind = "loader"
	UIEventKindPicker  UIEventKind = "picker"
	UIEventKindConfirm UIEventKind = "confirm"
)

type UIResponseAction string

const (
	UIResponseActionDismiss UIResponseAction = "dismiss"
	UIResponseActionSelect  UIResponseAction = "select"
	UIResponseActionConfirm UIResponseAction = "confirm"
)

type UIOutcome string

const (
	UIOutcomeConfirmed  UIOutcome = "confirmed"
	UIOutcomeSelected   UIOutcome = "selected"
	UIOutcomeDismissed  UIOutcome = "dismissed"
	UIOutcomeTimedOut   UIOutcome = "timed_out"
	UIOutcomeCompleted  UIOutcome = "completed"
	UIOutcomeSuperseded UIOutcome = "superseded"
	UIOutcomeCancelled  UIOutcome = "cancelled"
)

const (
	MethodLaunch                      = "launch" // DEPRECATED
	MethodRun                         = "run"
	MethodConfirm                     = "confirm"
	MethodUI                          = "ui"
	MethodUIRespond                   = "ui.respond"
	MethodRunScript                   = "run.script"
	MethodStop                        = "stop"
	MethodTokens                      = "tokens"
	MethodMedia                       = "media"
	MethodMediaGenerate               = "media.generate"
	MethodMediaGenerateCancel         = "media.generate.cancel"
	MethodMediaGenerateResume         = "media.generate.resume"
	MethodMediaIndex                  = "media.index" // DEPRECATED
	MethodMediaSearch                 = "media.search"
	MethodMediaTags                   = "media.tags"
	MethodMediaTagsUpdate             = "media.tags.update"
	MethodMediaMetaUpdate             = "media.meta.update"
	MethodMediaActive                 = "media.active"
	MethodMediaHistory                = "media.history"
	MethodMediaHistoryLatest          = "media.history.latest"
	MethodMediaHistoryTop             = "media.history.top"
	MethodMediaLookup                 = "media.lookup"
	MethodMediaMeta                   = "media.meta"
	MethodMediaImage                  = "media.image"
	MethodScrapers                    = "scrapers"
	MethodMediaScrape                 = "media.scrape"
	MethodMediaScrapeStatus           = "media.scrape.status"
	MethodMediaScrapeCancel           = "media.scrape.cancel"
	MethodMediaScrapeResume           = "media.scrape.resume"
	MethodMediaBrowse                 = "media.browse"
	MethodMediaBrowseIndex            = "media.browse.index"
	MethodMediaControl                = "media.control"
	MethodMediaActiveUpdate           = "media.active.update"
	MethodMediaCleanOrphans           = "media.clean.orphans"
	MethodSettings                    = "settings"
	MethodSettingsUpdate              = "settings.update"
	MethodSettingsReload              = "settings.reload"
	MethodSettingsLogsDownload        = "settings.logs.download"
	MethodSettingsBackup              = "settings.backup"
	MethodSettingsBackupList          = "settings.backup.list"
	MethodSettingsBackupInspect       = "settings.backup.inspect"
	MethodSettingsBackupDelete        = "settings.backup.delete"
	MethodSettingsBackupRestore       = "settings.backup.restore"
	MethodSettingsBackupStatus        = "settings.backup.status"
	MethodSettingsBackupRemoteRun     = "settings.backup.remote.run"
	MethodSettingsBackupRemoteList    = "settings.backup.remote.list"
	MethodSettingsBackupRemoteRestore = "settings.backup.remote.restore"
	MethodPlaytimeLimits              = "settings.playtime.limits"
	MethodPlaytimeLimitsUpdate        = "settings.playtime.limits.update"
	MethodPlaytime                    = "playtime"
	MethodClients                     = "clients"
	MethodClientsDelete               = "clients.delete"
	MethodClientsPairStart            = "clients.pair.start"
	MethodClientsPairCancel           = "clients.pair.cancel"
	MethodProfiles                    = "profiles"
	MethodProfilesNew                 = "profiles.new"
	MethodProfilesUpdate              = "profiles.update"
	MethodProfilesDelete              = "profiles.delete"
	MethodProfilesActive              = "profiles.active"
	MethodProfilesSwitch              = "profiles.switch"
	MethodProfilesVerify              = "profiles.verify"
	MethodSystems                     = "systems"
	MethodLaunchers                   = "launchers"
	MethodLaunchersRefresh            = "launchers.refresh"
	MethodHistory                     = "tokens.history"
	MethodMappings                    = "mappings"
	MethodMappingsNew                 = "mappings.new"
	MethodMappingsDelete              = "mappings.delete"
	MethodMappingsUpdate              = "mappings.update"
	MethodMappingsReload              = "mappings.reload"
	MethodReaders                     = "readers"
	MethodReadersWrite                = "readers.write"
	MethodReadersWriteCancel          = "readers.write.cancel"
	MethodVersion                     = "version"
	MethodHealthCheck                 = "health"
	MethodInbox                       = "inbox"
	MethodInboxDelete                 = "inbox.delete"
	MethodInboxClear                  = "inbox.clear"
	MethodSettingsAuthClaim           = "settings.auth.claim"
	MethodSettingsAuthStatus          = "settings.auth.status"
	MethodSettingsAuthUnlink          = "settings.auth.unlink"
	MethodSettingsAuthLink            = "settings.auth.link"
	MethodSettingsAuthLinkStatus      = "settings.auth.link.status"
	MethodSettingsAuthLinkCancel      = "settings.auth.link.cancel"
	MethodUpdateCheck                 = "update.check"
	MethodUpdateApply                 = "update.apply"
	MethodInputKeyboard               = "input.keyboard"
	MethodInputGamepad                = "input.gamepad"
	MethodScreenshot                  = "screenshot"
	MethodMediaTitleParse             = "media.title.parse"
)

// MethodHasUnboundedRuntime reports whether a method may run without a fixed
// whole-operation deadline. Full-device backup and restore work is bounded by
// caller cancellation, shutdown, and per-transfer timeouts instead; both the
// server request context and the local client wait consult this list so the
// two cannot disagree.
func MethodHasUnboundedRuntime(method string) bool {
	switch strings.ToLower(method) {
	case MethodSettingsBackup,
		MethodSettingsBackupRestore,
		MethodSettingsBackupRemoteRun,
		MethodSettingsBackupRemoteRestore:
		return true
	default:
		return false
	}
}

// ResponseWithCallback wraps a method result with a function that should be
// called after the response has been written to the client. This allows
// handlers to defer side effects (like triggering a restart) until the client
// has received the response, without relying on arbitrary sleep timers.
type ResponseWithCallback struct {
	Result     any
	AfterWrite func()
}

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type NotificationObject struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RequestObject struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RPCID           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ErrorObject struct {
	// Data is optional structured detail about the error. Per JSON-RPC 2.0
	// §5.1 it MUST be a member of the error object, not a sibling.
	Data    any    `json:"data,omitempty"`
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
