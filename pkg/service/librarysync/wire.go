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

package librarysync

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database"

// Device routes and headers of the Library sync contract.
const (
	pathResolve         = "/v1/device/library/resolve"
	pathState           = "/v1/device/library/state"
	pathInventory       = "/v1/device/library/inventory"
	headerGeneration    = "X-Zaparoo-Library-Generation"
	headerSchemaVersion = "X-Zaparoo-Library-Schema-Version"
	headerItemCount     = "X-Zaparoo-Library-Item-Count"

	resolveStatusResolved = "resolved"
	resolveStatusRejected = "rejected"

	codeUnknownOrdinal    = "unknown_ordinal"
	codeInventoryTooLarge = "inventory_too_large"
	codePayloadTooLarge   = "payload_too_large"
)

type resolveRequest struct {
	Items []resolveItem `json:"items"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type resolveItem struct {
	MediaIdentity *database.MediaIdentity `json:"media_identity"`
}

type resolveResponse struct {
	Items []resolveResult `json:"items"`
}

type resolveResult struct {
	Ordinal *int64 `json:"ordinal"`
	Status  string `json:"status"`
	Code    string `json:"code"`
	Index   int    `json:"index"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type inventoryResponse struct {
	ContentSHA256   string `json:"content_sha256"`
	IndexGeneration int64  `json:"index_generation"`
	ItemCount       int    `json:"item_count"`
	Superseded      bool   `json:"superseded"`
}
