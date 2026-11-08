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

package hoststatus

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestControllerDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		vendor  string
		product string
		want    string
	}{
		{vendor: "054c", product: "09cc", want: "DualShock 4"},
		{vendor: "054C", product: "05C4", want: "DualShock 4"},
		{vendor: "054c", product: "0ce6", want: "DualSense"},
		{vendor: "054c", product: "0df2", want: "DualSense Edge"},
		{vendor: "045e", product: "0b13", want: "Xbox Series Controller"},
		{vendor: "045e", product: "02fd", want: "Xbox One S Controller"},
		{vendor: "057e", product: "2009", want: "Switch Pro Controller"},
		{vendor: "28de", product: "1205", want: "Steam Deck"},
		{vendor: "054c", product: "ffff", want: ""},
		{vendor: "1234", product: "09cc", want: ""},
		{vendor: "", product: "09cc", want: ""},
		{vendor: "054c", product: "", want: ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ControllerDisplayName(tt.vendor, tt.product), tt.vendor+":"+tt.product)
	}
}

func TestControllerNamesTable(t *testing.T) {
	t.Parallel()

	key := regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{4}$`)
	for id, name := range controllerNames {
		assert.Regexp(t, key, id, "keys are lowercase vendor:product")
		assert.NotEmpty(t, name, id)
	}
}

func TestParseModalias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		modalias string
		vendor   string
		product  string
	}{
		{modalias: "usb:v054Cp09CCd0100", vendor: "054c", product: "09cc"},
		{modalias: "bluetooth:v000Fp1200d1436", vendor: "000f", product: "1200"},
		{modalias: "usb:v045Ep0B13", vendor: "045e", product: "0b13"},
		{modalias: "usb:v057Ep2009d0001", vendor: "057e", product: "2009"},
		{modalias: ""},
		{modalias: "usb:"},
		{modalias: "usb:v054Cp09C"},
		{modalias: "usb:v054Gp09CCd0100"},
		{modalias: "usb:x054Cp09CCd0100"},
		{modalias: "usb:v054Cp09CCx0100"},
		{modalias: "pci:v054Cp09CCd0100"},
		{modalias: "v054Cp09CCd0100"},
	}
	for _, tt := range tests {
		vendor, product := ParseModalias(tt.modalias)
		assert.Equal(t, tt.vendor, vendor, tt.modalias)
		assert.Equal(t, tt.product, product, tt.modalias)
	}
}
