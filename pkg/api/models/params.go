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

import "github.com/ZaparooProject/go-zapscript"

type SearchParams struct {
	Systems    *[]string `json:"systems" validate:"omitempty,dive,system"`
	MaxResults *int      `json:"maxResults" validate:"omitempty,gt=0,max=1000"`
	Cursor     *string   `json:"cursor,omitempty"`
	Tags       *[]string `json:"tags,omitempty" validate:"omitempty,dive,min=1"`
	Letter     *string   `json:"letter,omitempty" validate:"omitempty,letter"`
	Query      *string   `json:"query"`
}

type MediaIndexParams struct {
	Systems *[]string `json:"systems" validate:"omitempty,dive,system"`
}

type RunParams struct {
	Type   *string `json:"type"`
	UID    *string `json:"uid"`
	Text   *string `json:"text"`
	Data   *string `json:"data" validate:"omitempty,hexdata"`
	Unsafe bool    `json:"unsafe"`
}

type RunScriptParams struct {
	Name      *string                  `json:"name"`
	Cmds      []zapscript.ZapScriptCmd `json:"cmds"`
	ZapScript int                      `json:"zapscript"`
	Unsafe    bool                     `json:"unsafe"`
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

type ReaderWriteCancelParams struct {
	ReaderID *string `json:"readerId,omitempty"`
}

type ReaderConnection struct {
	Driver   string `json:"driver" validate:"required,min=1"`
	Path     string `json:"path"`
	IDSource string `json:"idSource,omitempty"`
}

type UpdateSettingsParams struct {
	RunZapScript            *bool               `json:"runZapScript"`
	DebugLogging            *bool               `json:"debugLogging"`
	AudioScanFeedback       *bool               `json:"audioScanFeedback"`
	ReadersAutoDetect       *bool               `json:"readersAutoDetect"`
	ErrorReporting          *bool               `json:"errorReporting"`
	ReadersScanMode         *string             `json:"readersScanMode" validate:"omitempty,oneof=tap hold"`
	ReadersScanExitDelay    *float32            `json:"readersScanExitDelay" validate:"omitempty,gte=0"`
	ReadersScanIgnoreSystem *[]string           `json:"readersScanIgnoreSystems" validate:"omitempty,dive,system"`
	ReadersConnect          *[]ReaderConnection `json:"readersConnect,omitempty"`
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

type MediaStartedParams struct {
	SystemID   string `json:"systemId" validate:"required"`
	SystemName string `json:"systemName" validate:"required"`
	MediaPath  string `json:"mediaPath" validate:"required"`
	MediaName  string `json:"mediaName" validate:"required"`
}

type UpdateActiveMediaParams struct {
	SystemID  string `json:"systemId" validate:"required"`
	MediaPath string `json:"mediaPath" validate:"required"`
	MediaName string `json:"mediaName" validate:"required"`
}

type DeleteInboxParams struct {
	ID int64 `json:"id" validate:"gt=0"`
}
