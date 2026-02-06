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

package service

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

type scanAction int

const (
	scanSkipDuplicate      scanAction = iota // token matches prevToken, ignore
	scanNewToken                             // new non-nil token to process
	scanNormalRemoval                        // token removed normally (nil, no error)
	scanReaderErrorRemoval                   // token removed due to reader error
)

// scanPreprocessor encapsulates the duplicate-detection and prevToken
// management logic that sits between raw reader scans and the rest of
// the token processing pipeline.
type scanPreprocessor struct {
	prevToken *tokens.Token
}

// Process decides what action the caller should take for the given scan.
// It updates internal state (prevToken) as a side effect.
func (p *scanPreprocessor) Process(scan *tokens.Token, readerError bool) scanAction {
	if helpers.TokensEqual(scan, p.prevToken) {
		return scanSkipDuplicate
	}

	if !readerError {
		p.prevToken = scan
	}

	if scan != nil {
		return scanNewToken
	}

	if readerError {
		return scanReaderErrorRemoval
	}

	return scanNormalRemoval
}

// PrevToken returns the current prevToken for logging/debugging.
func (p *scanPreprocessor) PrevToken() *tokens.Token {
	return p.prevToken
}
