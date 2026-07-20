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

//nolint:revive // custom validation tags (letter, duration, etc.) are unknown to revive
package models

import (
	"encoding/json"

	"github.com/ZaparooProject/go-zapscript"
)

type SearchParams struct {
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	MaxResults  *int      `json:"maxResults" validate:"omitempty,gt=0,max=1000"`
	Cursor      *string   `json:"cursor,omitempty"`
	Tags        *[]string `json:"tags,omitempty" validate:"omitempty,dive,min=1"`
	Letter      *string   `json:"letter,omitempty" validate:"omitempty,letter"`
	Query       *string   `json:"query"`
}

type BrowseParams struct {
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	Path        *string   `json:"path,omitempty"`
	MaxResults  *int      `json:"maxResults,omitempty" validate:"omitempty,gt=0,max=1000"`
	Cursor      *string   `json:"cursor,omitempty"`
	Letter      *string   `json:"letter,omitempty" validate:"omitempty,letter"`
	Sort        *string   `json:"sort,omitempty" validate:"omitempty,oneof=name-asc name-desc filename-asc filename-desc"`
}

type SystemsParams struct {
	All bool `json:"all,omitempty"`
}

type MediaIndexParams struct {
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	// Rebuild discards the media database entirely and indexes from scratch.
	// Scraped metadata is lost and must be re-scraped; user data (favourites,
	// launcher overrides) lives in UserDB and is re-applied after indexing.
	// Incompatible with a systems filter: a fresh database indexed selectively
	// would silently drop every other system's media.
	Rebuild *bool `json:"rebuild,omitempty"`
}

type RunParams struct {
	Type   *string `json:"type"`
	UID    *string `json:"uid"`
	Text   *string `json:"text"`
	Data   *string `json:"data" validate:"omitempty,hexdata"`
	Unsafe bool    `json:"unsafe"`
}

type UIRespondParams struct {
	ID       string           `json:"id" validate:"required"`
	Action   UIResponseAction `json:"action" validate:"required,oneof=dismiss select confirm"`
	ChoiceID string           `json:"choiceId,omitempty"`
}

type RunScriptParams struct {
	Name      *string                  `json:"name"`
	Cmds      []zapscript.ZapScriptCmd `json:"cmds"`
	ZapScript int                      `json:"zapscript"`
	Unsafe    bool                     `json:"unsafe"`
}

type AllMappingsParams struct {
	// IncludeReadOnly also returns read-only mappings loaded from the mappings
	// folder. Defaults to false so older clients keep receiving DB mappings only.
	IncludeReadOnly bool `json:"includeReadOnly,omitempty"`
}

type AddMappingParams struct {
	Label    string `json:"label" validate:"max=255"`
	Type     string `json:"type" validate:"required,oneof=id value data uid text"`
	Match    string `json:"match" validate:"required,oneof=exact partial regex"`
	Pattern  string `json:"pattern" validate:"required"`
	Override string `json:"override"`
	Enabled  bool   `json:"enabled"`
}

type DeleteMappingParams struct {
	ID int `json:"id" validate:"gt=0"`
}

type UpdateMappingParams struct {
	Label    *string `json:"label" validate:"omitempty,max=255"`
	Enabled  *bool   `json:"enabled"`
	Type     *string `json:"type" validate:"omitempty,oneof=id value data uid text"`
	Match    *string `json:"match" validate:"omitempty,oneof=exact partial regex"`
	Pattern  *string `json:"pattern" validate:"omitempty,min=1"`
	Override *string `json:"override"`
	ID       int     `json:"id" validate:"gt=0"`
}

type ReaderWriteParams struct {
	ReaderID *string `json:"readerId,omitempty"`
	Text     string  `json:"text" validate:"required"`
}

type BackupNameParams struct {
	Name string `json:"name" validate:"required"`
}

type BackupRemoteRestoreParams struct {
	ID string `json:"id" validate:"required"`
}

type SettingsAuthStatusParams struct {
	URL string `json:"url,omitempty" validate:"omitempty,url"`
}

type SettingsAuthLinkParams struct {
	// URL overrides the auth server base URL; defaults to the official
	// Zaparoo API.
	URL string `json:"url,omitempty" validate:"omitempty,url"`
}

type ReaderWriteCancelParams struct {
	ReaderID *string `json:"readerId,omitempty"`
}

type ReaderConnection struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Driver   string `json:"driver" validate:"required,min=1"`
	Path     string `json:"path"`
	IDSource string `json:"idSource,omitempty"`
}

type SystemDefault struct {
	System     string `json:"system" validate:"required,system"`
	Launcher   string `json:"launcher,omitempty"`
	BeforeExit string `json:"beforeExit,omitempty"`
}

// IsEnabled returns whether this connection is enabled.
// nil (omitted) and true both mean enabled; only explicit false disables.
func (r ReaderConnection) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

type UpdateSettingsParams struct {
	RunZapScript              *bool               `json:"runZapScript"`
	DebugLogging              *bool               `json:"debugLogging"`
	AudioScanFeedback         *bool               `json:"audioScanFeedback"`
	ReadersAutoDetect         *bool               `json:"readersAutoDetect"`
	ErrorReporting            *bool               `json:"errorReporting"`
	Encryption                *bool               `json:"encryption"`
	BackupRemoteEnabled       *bool               `json:"backupRemoteEnabled"`
	UpdateChannel             *string             `json:"updateChannel" validate:"omitempty,oneof=stable beta"`
	BackupRemoteSchedule      *string             `json:"backupRemoteSchedule" validate:"omitempty,oneof=daily weekly manual"`
	ReadersScanMode           *string             `json:"readersScanMode" validate:"omitempty,oneof=tap hold"`
	ReadersScanExitDelay      *float32            `json:"readersScanExitDelay" validate:"omitempty,gte=0"`
	ReadersScanIgnoreSystem   *[]string           `json:"readersScanIgnoreSystems" validate:"omitempty,dive,system"`
	ReadersConnect            *[]ReaderConnection `json:"readersConnect,omitempty"`
	SystemDefaults            *[]SystemDefault    `json:"systemDefaults,omitempty" validate:"omitempty,dive"`
	AudioVolume               *int                `json:"audioVolume" validate:"omitempty,gte=0,lte=200"`
	LaunchGuardEnabled        *bool               `json:"launchGuardEnabled"`
	LaunchGuardTimeout        *float32            `json:"launchGuardTimeout" validate:"omitempty,gte=-1"`
	LaunchGuardDelay          *float32            `json:"launchGuardDelay" validate:"omitempty,gte=0"`
	LaunchGuardRequireConfirm *bool               `json:"launchGuardRequireConfirm"`
	ProfilesRequireForLaunch  *bool               `json:"profilesRequireForLaunch"`
	ProfilesSwapData          *bool               `json:"profilesSwapData"`
}

type UpdatePlaytimeLimitsParams struct {
	Enabled      *bool     `json:"enabled"`
	Daily        *string   `json:"daily" validate:"omitempty,duration"`
	Session      *string   `json:"session" validate:"omitempty,duration"`
	SessionReset *string   `json:"sessionReset" validate:"omitempty,duration"`
	Warnings     *[]string `json:"warnings" validate:"omitempty,dive,duration"`
	Retention    *int      `json:"retention" validate:"omitempty,gte=0"`
}

type NewClientParams struct {
	Name string `json:"name" validate:"required,min=1,max=255"`
}

type DeleteClientParams struct {
	ID string `json:"id" validate:"required,min=1"`
}

// ClientsPairStartParams configures a new pairing flow. Role is the
// permission role the paired client will receive ("admin" or "member");
// empty defaults to member.
type ClientsPairStartParams struct {
	Role string `json:"role" validate:"omitempty,oneof=admin member"`
}

// NewProfileParams creates a profile. Nil limit fields inherit the global
// config; a "0" duration means explicitly unlimited.
type NewProfileParams struct {
	PIN           *string `json:"pin" validate:"omitempty,numeric,min=4,max=8"`
	LimitsEnabled *bool   `json:"limitsEnabled"`
	DailyLimit    *string `json:"dailyLimit" validate:"omitempty,duration"`
	SessionLimit  *string `json:"sessionLimit" validate:"omitempty,duration"`
	Name          string  `json:"name" validate:"required,min=1,max=255"`
	Role          string  `json:"role" validate:"omitempty,oneof=admin member"`
}

// UpdateProfileParams updates a profile. Omitted fields are unchanged.
// ClearPIN removes the PIN; ClearLimits resets all limit overrides back to
// inheriting global config before any limit fields in the same request are
// applied (clear-then-set); RegenerateSwitchID issues a new switch ID
// (lost-card replacement).
type UpdateProfileParams struct {
	Name               *string `json:"name" validate:"omitempty,min=1,max=255"`
	PIN                *string `json:"pin" validate:"omitempty,numeric,min=4,max=8"`
	LimitsEnabled      *bool   `json:"limitsEnabled"`
	DailyLimit         *string `json:"dailyLimit" validate:"omitempty,duration"`
	SessionLimit       *string `json:"sessionLimit" validate:"omitempty,duration"`
	Role               *string `json:"role" validate:"omitempty,oneof=admin member"`
	ProfileID          string  `json:"profileId" validate:"required,min=1"`
	ClearPIN           bool    `json:"clearPin"`
	ClearLimits        bool    `json:"clearLimits"`
	RegenerateSwitchID bool    `json:"regenerateSwitchId"`
}

type DeleteProfileParams struct {
	ProfileID string `json:"profileId" validate:"required,min=1"`
}

// SwitchProfileParams switches the device's active profile. Exactly one of
// ProfileID or SwitchID selects the target; both omitted (or null) means
// deactivate. PIN is required when the target profile has one set.
type SwitchProfileParams struct {
	ProfileID *string `json:"profileId"`
	SwitchID  *string `json:"switchId"`
	PIN       *string `json:"pin"`
}

// VerifyProfileParams verifies a profile credential without switching.
// Exactly one of ProfileID (with PIN when the profile has one) or SwitchID
// (a bearer credential) must be given.
type VerifyProfileParams struct {
	ProfileID *string `json:"profileId"`
	SwitchID  *string `json:"switchId"`
	PIN       *string `json:"pin"`
}

type MediaStartedParams struct {
	SystemID   string `json:"systemId" validate:"required"`
	SystemName string `json:"systemName" validate:"required"`
	MediaPath  string `json:"mediaPath" validate:"required"`
	MediaName  string `json:"mediaName" validate:"required"`
	Slot       string `json:"slot,omitempty"`
}

// ActiveMediaQueryParams holds the optional filter parameters for the media.active method.
// Slot selects which slot to read; defaults to primary when empty.
type ActiveMediaQueryParams struct {
	Slot string `json:"slot,omitempty"`
}

type UpdateActiveMediaParams struct {
	SystemID  string `json:"systemId" validate:"required"`
	MediaPath string `json:"mediaPath" validate:"required"`
	MediaName string `json:"mediaName" validate:"required"`
}

type MediaStoppedParams struct {
	SystemID   string `json:"systemId"`
	SystemName string `json:"systemName"`
	MediaName  string `json:"mediaName"`
	MediaPath  string `json:"mediaPath"`
	LauncherID string `json:"launcherId"`
	Slot       string `json:"slot,omitempty"`
	Elapsed    int    `json:"elapsed"`
}

type MediaHistoryParams struct {
	Systems     *[]string `json:"systems,omitempty" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	Limit       *int      `json:"limit,omitempty" validate:"omitempty,gt=0,max=100"`
	Cursor      *string   `json:"cursor,omitempty"`
}

type MediaHistoryTopParams struct {
	Systems     *[]string `json:"systems,omitempty" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	Since       *string   `json:"since,omitempty"`
	Limit       *int      `json:"limit,omitempty" validate:"omitempty,gt=0,max=100"`
}

type MediaMetaParams struct {
	MediaID *int64 `json:"mediaId,omitempty"`
	System  string `json:"system" validate:"omitempty,min=1"`
	Path    string `json:"path"   validate:"omitempty,min=1"`
}

type MediaTagsUpdateParams struct {
	MediaID *int64   `json:"mediaId,omitempty"`
	System  string   `json:"system" validate:"omitempty,min=1"`
	Path    string   `json:"path"   validate:"omitempty,min=1"`
	Add     []string `json:"add,omitempty" validate:"omitempty,dive,min=1"`
	Remove  []string `json:"remove,omitempty" validate:"omitempty,dive,min=1"`
}

type MediaMetaUpdateParams struct {
	MediaID *int64          `json:"mediaId,omitempty"`
	System  string          `json:"system" validate:"omitempty,min=1"`
	Path    string          `json:"path"   validate:"omitempty,min=1"`
	Media   json.RawMessage `json:"media,omitempty"`
}

type MediaImageParams struct {
	MediaID *int64 `json:"mediaId,omitempty"`
	// MaxSize is a hint for the longest edge (in pixels) of the returned image.
	// The server resizes down to fit a MaxSize×MaxSize box and caches the result;
	// omit it to receive the full-size image. Requests are snapped to a small set
	// of standard sizes server-side, so the returned image may be larger than
	// requested — clients should downscale to their final display size.
	MaxSize    *int32   `json:"maxSize,omitempty" validate:"omitempty,gt=0,max=8192"`
	System     string   `json:"system"            validate:"omitempty,min=1"`
	Path       string   `json:"path"              validate:"omitempty,min=1"`
	ImageTypes []string `json:"imageTypes"        validate:"omitempty,dive,min=1"`
}

type MediaScrapeParams struct {
	ScraperID string   `json:"scraperId" validate:"required,min=1"`
	Systems   []string `json:"systems"   validate:"omitempty,dive,min=1"`
	Force     bool     `json:"force"`
}

type MediaLookupParams struct {
	FuzzySystem *bool  `json:"fuzzySystem,omitempty"`
	Name        string `json:"name" validate:"required,min=1"`
	System      string `json:"system" validate:"required,min=1"`
}

type MediaControlParams struct {
	Args   map[string]string `json:"args,omitempty"`
	Action string            `json:"action" validate:"required,min=1"`
	Slot   string            `json:"slot,omitempty"`
}

type DeleteInboxParams struct {
	ID int64 `json:"id" validate:"gt=0"`
}

type SettingsAuthClaimParams struct {
	ClaimURL string `json:"claimUrl" validate:"required,url"`
	Token    string `json:"token" validate:"required"`
}

type InputKeyboardParams struct {
	Keys string `json:"keys" validate:"required,min=1"`
}

type InputGamepadParams struct {
	Buttons string `json:"buttons" validate:"required,min=1"`
}

type MediaTitleParseParams struct {
	SystemID string `json:"systemId" validate:"required,min=1"`
	Path     string `json:"path" validate:"required,min=1"`
}
