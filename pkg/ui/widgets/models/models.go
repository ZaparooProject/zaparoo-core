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

type NoticeArgs struct {
	Text     string `json:"text"`
	Complete string `json:"complete"`
	Timeout  int    `json:"timeout"`
}

type PickerItem struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type PickerArgs struct {
	Title    string       `json:"title"`
	Items    []PickerItem `json:"items"`
	Selected int          `json:"selected"`
	Timeout  int          `json:"timeout"`
	Unsafe   bool         `json:"unsafe"`
}
