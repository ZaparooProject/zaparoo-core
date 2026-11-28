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
	"maps"
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// LaunchModifierTTL bounds how long a one-shot launch modifier waits for its
// next launch. Without it, a modifier armed and then forgotten silently
// rewrites a launch hours later. Sticky modifiers never expire; lifting
// their card ends them.
const LaunchModifierTTL = 5 * time.Minute

// LaunchModifier is an armed launch modifier: the advanced arguments of a
// launch card with no path, applied to launches from readers.
//
// A sticky modifier belongs to a card resting on a reader in hold mode. It
// applies to every launch until the card is lifted. Any other modifier is a
// one-shot that applies to the next launch only.
type LaunchModifier struct {
	Source    tokens.Token
	CreatedAt time.Time
	Args      map[string]string
	Sticky    bool
	// used records that a sticky modifier has changed at least one launch.
	// A sticky card lifted before it was ever used becomes a one-shot, so
	// placing a modifier, lifting it and then placing a game on the same
	// reader still works.
	used bool
}

func (m *LaunchModifier) expired(now time.Time) bool {
	return !m.Sticky && !m.CreatedAt.IsZero() && now.Sub(m.CreatedAt) > LaunchModifierTTL
}

func (m *LaunchModifier) fromReader(readerID string) bool {
	return readerID != "" && m.Source.ReaderID == readerID
}

func cloneLaunchModifier(m *LaunchModifier) LaunchModifier {
	c := *m
	c.Args = maps.Clone(m.Args)
	return c
}

// ArmLaunchModifier arms mod, replacing any modifier armed from the same
// reader and, for a one-shot, any pending one-shot. A sticky request whose
// card has already left its reader is armed as a one-shot instead. It
// reports whether the modifier was armed sticky.
func (s *State) ArmLaunchModifier(mod *LaunchModifier) bool {
	armed := cloneLaunchModifier(mod)
	armed.used = false

	s.mu.Lock()
	if armed.Sticky && !s.readerHoldsLocked(&armed.Source) {
		armed.Sticky = false
	}
	kept := make([]LaunchModifier, 0, len(s.launchModifiers)+1)
	for i := range s.launchModifiers {
		existing := &s.launchModifiers[i]
		if existing.fromReader(armed.Source.ReaderID) && (existing.Sticky || armed.Sticky) {
			continue
		}
		if !armed.Sticky && !existing.Sticky {
			continue
		}
		kept = append(kept, *existing)
	}
	kept = append(kept, armed)
	s.launchModifiers = kept
	payload := s.launchModifiersPayloadLocked(time.Now())
	s.mu.Unlock()

	notifications.TokensModifiers(s.Notifications, payload)
	return armed.Sticky
}

// TakeLaunchModifiers returns the modifiers that apply to a launch starting
// now: every sticky modifier oldest first, then the pending one-shot, which
// is consumed. An expired one-shot is dropped and returned separately so the
// caller can report it.
func (s *State) TakeLaunchModifiers(now time.Time) (active, expired []LaunchModifier) {
	s.mu.Lock()
	if len(s.launchModifiers) == 0 {
		s.mu.Unlock()
		return nil, nil
	}
	var oneShot *LaunchModifier
	kept := make([]LaunchModifier, 0, len(s.launchModifiers))
	for i := range s.launchModifiers {
		m := &s.launchModifiers[i]
		switch {
		case m.Sticky:
			m.used = true
			active = append(active, cloneLaunchModifier(m))
			kept = append(kept, *m)
		case m.expired(now):
			expired = append(expired, cloneLaunchModifier(m))
		default:
			c := cloneLaunchModifier(m)
			oneShot = &c
		}
	}
	if oneShot != nil {
		active = append(active, *oneShot)
	}
	changed := len(kept) != len(s.launchModifiers)
	s.launchModifiers = kept
	var payload models.LaunchModifiersParams
	if changed {
		payload = s.launchModifiersPayloadLocked(now)
	}
	s.mu.Unlock()

	if changed {
		notifications.TokensModifiers(s.Notifications, payload)
	}
	return active, expired
}

// LaunchModifiers returns a copy of the armed modifiers, oldest first,
// leaving out a one-shot that has expired.
func (s *State) LaunchModifiers() []LaunchModifier {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LaunchModifier, 0, len(s.launchModifiers))
	for i := range s.launchModifiers {
		if s.launchModifiers[i].expired(now) {
			continue
		}
		out = append(out, cloneLaunchModifier(&s.launchModifiers[i]))
	}
	return out
}

// SetReaderToken records the token now resting on readerID. A different
// token already recorded there was replaced without a removal being
// reported, so its card is treated as lifted.
func (s *State) SetReaderToken(readerID string, token *tokens.Token, now time.Time) {
	if readerID == "" || token == nil {
		return
	}
	s.mu.Lock()
	if s.readerTokens == nil {
		s.readerTokens = make(map[string]tokens.Token)
	}
	changed := false
	if previous, ok := s.readerTokens[readerID]; ok && !helpers.TokensEqual(&previous, token) {
		changed = s.releaseReaderModifiersLocked(readerID, now, true)
	}
	s.readerTokens[readerID] = *token
	var payload models.LaunchModifiersParams
	if changed {
		payload = s.launchModifiersPayloadLocked(now)
	}
	s.mu.Unlock()

	if changed {
		notifications.TokensModifiers(s.Notifications, payload)
	}
}

// ReleaseReaderToken records that the card on readerID left it. When the
// removal was a normal one, a sticky modifier from that card that never
// changed a launch becomes a one-shot; otherwise it ends. A reader error says
// nothing reliable about the card, so its modifier ends.
func (s *State) ReleaseReaderToken(readerID string, now time.Time, readerError bool) {
	if readerID == "" {
		return
	}
	s.mu.Lock()
	delete(s.readerTokens, readerID)
	changed := s.releaseReaderModifiersLocked(readerID, now, !readerError)
	var payload models.LaunchModifiersParams
	if changed {
		payload = s.launchModifiersPayloadLocked(now)
	}
	s.mu.Unlock()

	if changed {
		notifications.TokensModifiers(s.Notifications, payload)
	}
}

// ReaderTokens returns the tokens resting on readers that report removal,
// oldest scan first. A reader that cannot report removal never says its
// token left, so its token is not listed.
func (s *State) ReaderTokens() []tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]tokens.Token, 0, len(s.readerTokens))
	for readerID := range s.readerTokens {
		r, ok := s.readers[readerID]
		if !ok || r == nil || !slices.Contains(r.Capabilities(), readers.CapabilityRemovable) {
			continue
		}
		out = append(out, s.readerTokens[readerID])
	}
	slices.SortFunc(out, func(a, b tokens.Token) int {
		return a.ScanTime.Compare(b.ScanTime)
	})
	return out
}

// readerHoldsLocked reports whether source is the token resting on its
// reader. Requires s.mu.
func (s *State) readerHoldsLocked(source *tokens.Token) bool {
	if source.ReaderID == "" {
		return false
	}
	present, ok := s.readerTokens[source.ReaderID]
	return ok && helpers.TokensEqual(&present, source)
}

// releaseReaderModifiersLocked ends or degrades the sticky modifiers armed
// from readerID and reports whether the set changed. Requires s.mu.
func (s *State) releaseReaderModifiersLocked(readerID string, now time.Time, degrade bool) bool {
	var degraded *LaunchModifier
	kept := make([]LaunchModifier, 0, len(s.launchModifiers))
	changed := false
	for i := range s.launchModifiers {
		m := s.launchModifiers[i]
		if !m.Sticky || !m.fromReader(readerID) {
			kept = append(kept, m)
			continue
		}
		changed = true
		if degrade && !m.used {
			m.Sticky = false
			m.CreatedAt = now
			degraded = &m
		}
	}
	if degraded != nil {
		kept = slices.DeleteFunc(kept, func(m LaunchModifier) bool { return !m.Sticky })
		kept = append(kept, *degraded)
	}
	if changed {
		s.launchModifiers = kept
	}
	return changed
}

// launchModifiersPayloadLocked builds the tokens.modifiers payload.
// Requires s.mu.
func (s *State) launchModifiersPayloadLocked(now time.Time) models.LaunchModifiersParams {
	payload := models.LaunchModifiersParams{
		Modifiers: make([]models.LaunchModifierResponse, 0, len(s.launchModifiers)),
	}
	for i := range s.launchModifiers {
		m := &s.launchModifiers[i]
		if m.expired(now) {
			continue
		}
		payload.Modifiers = append(payload.Modifiers, LaunchModifierResponse(m))
	}
	return payload
}

// LaunchModifierResponse converts a modifier to its API form.
func LaunchModifierResponse(m *LaunchModifier) models.LaunchModifierResponse {
	return models.LaunchModifierResponse{
		CreatedAt: m.CreatedAt,
		Args:      maps.Clone(m.Args),
		ReaderID:  m.Source.ReaderID,
		UID:       m.Source.UID,
		Sticky:    m.Sticky,
	}
}
