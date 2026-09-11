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
	scanSkipDuplicate      scanAction = iota // token matches this reader's previous scan, ignore
	scanNewToken                             // new non-nil token to process
	scanNormalRemoval                        // token removed normally (nil, no error)
	scanReaderErrorRemoval                   // token removed due to reader error
)

// scanPreprocessorPruneThreshold is the number of tracked readers above which
// the preprocessor reconciles against the connected readers. A reader that
// disappears while a token is still on it leaves its entry behind, so without
// this a device that cycles through reader identities — auto-detected drives
// being plugged and unplugged, say — would accumulate them for the life of the
// process.
const scanPreprocessorPruneThreshold = 16

// scanPreprocessor encapsulates the duplicate-detection and previous-token
// management logic that sits between raw reader scans and the rest of the
// token processing pipeline.
//
// State is per reader. Readers report presence independently, so one reader's
// scan says nothing about what is sitting on another: with a single shared
// slot, a removal on one reader made the next reader's removal look like a
// duplicate and dropped it, and the token a removal referred to was whichever
// reader had most recently been touched.
type scanPreprocessor struct {
	// prevTokens holds the token each reader currently reports as present,
	// keyed by reader ID. A reader with nothing on it has no entry, so the map
	// tracks only readers that currently carry a token.
	prevTokens map[string]*tokens.Token
}

// Process decides what action the caller should take for the given scan from
// readerID. It updates that reader's tracked token as a side effect.
func (p *scanPreprocessor) Process(readerID string, scan *tokens.Token, readerError bool) scanAction {
	if helpers.TokensEqual(scan, p.prevTokens[readerID]) {
		return scanSkipDuplicate
	}

	// A reader error is not evidence about what is on the reader, so the
	// tracked token is left alone and a reconnect is free to report it again.
	if !readerError {
		if scan == nil {
			delete(p.prevTokens, readerID)
		} else {
			if p.prevTokens == nil {
				p.prevTokens = make(map[string]*tokens.Token)
			}
			p.prevTokens[readerID] = scan
		}
	}

	if scan != nil {
		return scanNewToken
	}

	if readerError {
		return scanReaderErrorRemoval
	}

	return scanNormalRemoval
}

// PrevToken returns the token readerID currently reports as present, or nil.
func (p *scanPreprocessor) PrevToken(readerID string) *tokens.Token {
	return p.prevTokens[readerID]
}

// ResolveReaderID attributes a scan that arrived without a reader ID.
//
// Every in-tree driver labels its scans, including removals. One that did not
// would have its removals keyed separately from its scans and dropped as
// duplicates, so an unlabelled scan is attributed to the only reader currently
// tracked when there is exactly one — which is what a single-reader device did
// before scans were tracked per reader. With no reader tracked, or more than
// one, there is nothing to attribute it to and it stays unlabelled.
func (p *scanPreprocessor) ResolveReaderID(readerID string) string {
	if readerID != "" || len(p.prevTokens) != 1 {
		return readerID
	}
	for id := range p.prevTokens {
		return id
	}
	return readerID
}

// Tracked reports how many readers currently have a token tracked against them.
func (p *scanPreprocessor) Tracked() int {
	return len(p.prevTokens)
}

// Retain drops the tracked token of every reader not in readerIDs, so a reader
// that disconnected while a token was on it does not hold its entry forever.
func (p *scanPreprocessor) Retain(readerIDs []string) {
	if len(p.prevTokens) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(readerIDs))
	for _, id := range readerIDs {
		keep[id] = struct{}{}
	}
	for id := range p.prevTokens {
		if _, ok := keep[id]; !ok {
			delete(p.prevTokens, id)
		}
	}
}
