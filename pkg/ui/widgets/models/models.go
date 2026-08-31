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

import apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"

type NoticeArgs struct {
	Text        string `json:"text"`
	Complete    string `json:"complete"`
	EventID     string `json:"eventId,omitempty"`
	Timeout     int    `json:"timeout"`
	Dismissible bool   `json:"dismissible,omitempty"`
}

type PickerItem struct {
	ID        string                     `json:"id,omitempty"`
	Name      string                     `json:"name"`
	ZapScript string                     `json:"zapscript,omitempty"`
	Action    apimodels.UIResponseAction `json:"action,omitempty"`
}

type PickerArgs struct {
	Title       string       `json:"title"`
	Message     string       `json:"message,omitempty"`
	Complete    string       `json:"complete,omitempty"`
	EventID     string       `json:"eventId,omitempty"`
	Items       []PickerItem `json:"items"`
	Selected    int          `json:"selected"`
	Timeout     int          `json:"timeout"`
	Unsafe      bool         `json:"unsafe"`
	Dismissible bool         `json:"dismissible,omitempty"`
}
