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

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/models"

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

type ScraperStartParams struct {
	Systems *[]string `json:"systems"`
	Media   *int64    `json:"media"`
}
