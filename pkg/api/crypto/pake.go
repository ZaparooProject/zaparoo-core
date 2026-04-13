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

package crypto

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// ErrInvalidPakeMessage is returned when a PAKE message cannot be decoded.
var ErrInvalidPakeMessage = errors.New("invalid PAKE message")

// PakeMessage is the wire format for PAKE exchange messages. All elliptic
// curve coordinates are decimal strings to avoid precision loss in non-Go
// JSON parsers (IEEE 754 doubles only hold 53 bits).
type PakeMessage struct {
	UX   string `json:"ux"`
	UY   string `json:"uy"`
	VX   string `json:"vx"`
	VY   string `json:"vy"`
	XX   string `json:"xx"`
	XY   string `json:"xy"`
	YX   string `json:"yx"`
	YY   string `json:"yy"`
	Role int    `json:"role"`
}

// pakeInternal mirrors the schollz/pake/v3 Pake struct's exported fields.
// The library has no JSON struct tags, so json.Marshal uses the raw Go field
// names — which contain Unicode subscript characters (U+1D64, U+1D65).
//
//nolint:tagliatelle // tags must match pake library's Unicode field names exactly
type pakeInternal struct {
	UU   *big.Int `json:"Uᵤ"`
	UV   *big.Int `json:"Uᵥ"`
	VU   *big.Int `json:"Vᵤ"`
	VV   *big.Int `json:"Vᵥ"`
	XU   *big.Int `json:"Xᵤ"`
	XV   *big.Int `json:"Xᵥ"`
	YU   *big.Int `json:"Yᵤ"`
	YV   *big.Int `json:"Yᵥ"`
	Role int      `json:"Role"`
}

// EncodePakeMessage converts the pake library's internal JSON (from
// pake.Bytes()) into the clean wire format with ASCII field names and
// string-quoted coordinates.
func EncodePakeMessage(internal []byte) ([]byte, error) {
	var p pakeInternal
	if err := json.Unmarshal(internal, &p); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPakeMessage, err)
	}
	wire := PakeMessage{
		Role: p.Role,
		UX:   bigIntToStr(p.UU),
		UY:   bigIntToStr(p.UV),
		VX:   bigIntToStr(p.VU),
		VY:   bigIntToStr(p.VV),
		XX:   bigIntToStr(p.XU),
		XY:   bigIntToStr(p.XV),
		YX:   bigIntToStr(p.YU),
		YY:   bigIntToStr(p.YV),
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode pake wire message: %w", err)
	}
	return b, nil
}

// DecodePakeMessage converts the wire-format JSON (ASCII field names,
// string-quoted coordinates) back into the pake library's internal format
// so it can be passed to pake.Update().
func DecodePakeMessage(wire []byte) ([]byte, error) {
	var msg PakeMessage
	if err := json.Unmarshal(wire, &msg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPakeMessage, err)
	}
	p := pakeInternal{Role: msg.Role}
	var err error
	if p.UU, err = strToBigInt(msg.UX, "ux"); err != nil {
		return nil, err
	}
	if p.UV, err = strToBigInt(msg.UY, "uy"); err != nil {
		return nil, err
	}
	if p.VU, err = strToBigInt(msg.VX, "vx"); err != nil {
		return nil, err
	}
	if p.VV, err = strToBigInt(msg.VY, "vy"); err != nil {
		return nil, err
	}
	if p.XU, err = strToBigInt(msg.XX, "xx"); err != nil {
		return nil, err
	}
	if p.XV, err = strToBigInt(msg.XY, "xy"); err != nil {
		return nil, err
	}
	if p.YU, err = strToBigInt(msg.YX, "yx"); err != nil {
		return nil, err
	}
	if p.YV, err = strToBigInt(msg.YY, "yy"); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode pake internal message: %w", err)
	}
	return b, nil
}

func bigIntToStr(n *big.Int) string {
	if n == nil {
		return "0"
	}
	return n.Text(10)
}

func strToBigInt(s, field string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("%w: field %q is not a valid integer", ErrInvalidPakeMessage, field)
	}
	if n.Sign() < 0 {
		return nil, fmt.Errorf("%w: field %q must not be negative", ErrInvalidPakeMessage, field)
	}
	return n, nil
}
