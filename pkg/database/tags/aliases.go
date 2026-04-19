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

package tags

// deprecatedTagAliases maps old normalized "type:value" strings to their
// canonical replacements. Used at query time so old NFC tokens and ZapScript
// files written before the taxonomy change continue to resolve correctly.
var deprecatedTagAliases = map[string]string{
	// barcode: namespace was introduced; barcodeboy moved under it
	"addon:barcodeboy": "addon:barcode:barcodeboy",
	// jcart and rumble are features embedded in the cartridge, not external add-ons
	"addon:controller:jcart":  "embedded:slot:jcart",
	"addon:controller:rumble": "embedded:vibration:rumble",
}

// CanonicalizeTagAlias rewrites a deprecated full "type:value" tag string to its
// canonical form. Returns the input unchanged when no alias matches.
func CanonicalizeTagAlias(fullTag string) string {
	if canonical, ok := deprecatedTagAliases[fullTag]; ok {
		return canonical
	}
	return fullTag
}
