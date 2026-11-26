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

package state

import (
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// ScanClaim is a caller waiting for the next token a reader scans, in place
// of the token running. See ClaimNextScan.
type ScanClaim struct {
	ch    chan tokens.Token
	owner string
}

// Scans yields the claimed token, then closes. It closes with no token when
// the claim is cancelled.
func (c *ScanClaim) Scans() <-chan tokens.Token {
	return c.ch
}

// ClaimNextScan takes the next token a reader scans away from the service,
// which then neither runs it nor treats it as a launch. Every claim open when
// the token arrives receives it. owner names the caller, so its claims can be
// cancelled as a group. The returned release function drops the claim if it
// is still open and is safe to call more than once.
func (s *State) ClaimNextScan(owner string) (claim *ScanClaim, release func()) {
	claim = &ScanClaim{owner: owner, ch: make(chan tokens.Token, 1)}
	s.scanClaimsMu.Lock()
	s.scanClaims = append(s.scanClaims, claim)
	s.scanClaimsMu.Unlock()
	return claim, func() {
		s.scanClaimsMu.Lock()
		defer s.scanClaimsMu.Unlock()
		s.scanClaims = slices.DeleteFunc(s.scanClaims, func(c *ScanClaim) bool { return c == claim })
	}
}

// CancelScanClaims ends every open claim owned by owner without a token and
// reports how many there were.
func (s *State) CancelScanClaims(owner string) int {
	s.scanClaimsMu.Lock()
	defer s.scanClaimsMu.Unlock()
	cancelled := 0
	s.scanClaims = slices.DeleteFunc(s.scanClaims, func(c *ScanClaim) bool {
		if c.owner != owner {
			return false
		}
		close(c.ch)
		cancelled++
		return true
	})
	return cancelled
}

// HasScanClaims reports whether any caller is waiting for a scan.
func (s *State) HasScanClaims() bool {
	s.scanClaimsMu.Lock()
	defer s.scanClaimsMu.Unlock()
	return len(s.scanClaims) > 0
}

// DeliverScanToClaims hands a scanned token to every open claim and closes
// them. It reports false when nobody was waiting, so the token should be
// handled as usual.
//
//nolint:gocritic // the token is copied to each claim
func (s *State) DeliverScanToClaims(t tokens.Token) bool {
	s.scanClaimsMu.Lock()
	claims := s.scanClaims
	s.scanClaims = nil
	s.scanClaimsMu.Unlock()
	for _, c := range claims {
		c.ch <- t
		close(c.ch)
	}
	return len(claims) > 0
}
