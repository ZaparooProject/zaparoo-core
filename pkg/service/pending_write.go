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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const pendingWriteTTL = time.Minute

func handlePendingWrite(svc *ServiceContext, scan *tokens.Token, player audio.Player) bool {
	pending := svc.State.ConsumePendingWrite()
	if pending == nil {
		return false
	}
	if pendingWriteExpired(pending.CreatedAt) {
		log.Warn().Msg("pending write expired")
		return false
	}

	if samePhysicalToken(scan, &pending.Source) || helpers.TokensEqual(scan, &pending.Source) {
		log.Info().Msg("pending write ignored source token")
		svc.State.SetPendingWrite(pending)
		return true
	}

	allReaders := svc.State.ListReaders()
	var writer readers.Reader
	var err error
	if scan.ReaderID != "" {
		writer, err = readers.SelectWriterStrict(allReaders, scan.ReaderID)
	} else {
		writer, err = readers.SelectWriterPreferred(allReaders, nil)
	}
	if err != nil {
		log.Error().Err(err).Msg("pending write failed to select writer")
		playPendingWriteFailSound(svc, player)
		return true
	}

	targetWriter, ok := writer.(readers.TargetWriter)
	if !ok {
		log.Error().Str("readerID", writer.ReaderID()).Msg("pending write reader does not support targeted writes")
		playPendingWriteFailSound(svc, player)
		return true
	}

	written, err := targetWriter.WriteTarget(svc.State.GetContext(), pending.Payload, readers.WriteOptions{
		TargetUID:  scan.UID,
		ExcludeUID: pending.Source.UID,
	})
	if err != nil {
		log.Error().Err(err).Msg("pending write failed")
		playPendingWriteFailSound(svc, player)
		return true
	}
	if written != nil {
		svc.State.SetWroteToken(written)
	}
	playPendingWriteSuccessSound(svc, player)
	log.Info().Msg("pending write completed")
	return true
}

func playPendingWriteSuccessSound(svc *ServiceContext, player audio.Player) {
	path, enabled := svc.Config.SuccessSoundPath(helpers.DataDir(svc.Platform))
	helpers.PlayConfiguredSound(player, path, enabled, assets.SuccessSound, "success")
}

func playPendingWriteFailSound(svc *ServiceContext, player audio.Player) {
	path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
	helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
}

func pendingWriteExpired(createdAt time.Time) bool {
	return !createdAt.IsZero() && time.Since(createdAt) > pendingWriteTTL
}

func samePhysicalToken(a, b *tokens.Token) bool {
	if a == nil || b == nil {
		return false
	}
	return a.UID != "" && a.UID == b.UID
}
