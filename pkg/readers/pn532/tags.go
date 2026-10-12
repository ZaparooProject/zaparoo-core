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

package pn532

import (
	"context"
	"errors"
	"fmt"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/tagops"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

func (*Reader) convertTagType(tagType pn532.TagType) string {
	switch tagType {
	case pn532.TagTypeNTAG:
		return tokens.TypeNTAG
	case pn532.TagTypeMIFARE:
		return tokens.TypeMifare
	case pn532.TagTypeFeliCa:
		return tokens.TypeFeliCa
	case pn532.TagTypeUnknown, pn532.TagTypeAny:
		return tokens.TypeUnknown
	default:
		return tokens.TypeUnknown
	}
}

// readTag returns what a newly detected tag holds, as a token carries it.
//
// go-pn532 reads the NDEF message. The shared tag layer then decides what
// kind of token the tag is and supplies its memory, so a tag gives the same
// token here as on any other reader.
func (r *Reader) readTag(ctx context.Context, detectedTag *pn532.DetectedTag) (tokenType, text string, data []byte) {
	tokenType = r.convertTagType(detectedTag.Type)
	if r.tagOps == nil {
		log.Debug().Str("uid", detectedTag.UID).Msg("tagOps not available, reporting tag by UID")
		return tokenType, "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, ndefReadTimeout)
	defer cancel()

	msg := r.readNDEFMessage(ctx, detectedTag)
	text = nfctag.TextFromMessage(msg)
	if msg != nil && len(msg.Records) > 0 && text == "" {
		log.Warn().
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Str("recordType", string(msg.Records[0].Type)).
			Msg("NDEF records found but no text/URI could be extracted - unsupported format")
	}

	kind := r.memoryKind(detectedTag.Type, msg)
	if kind == nfctag.KindUnknown {
		return tokenType, text, nil
	}
	res, err := readMemory(ctx, r.tagOps, kind, detectedTag.UIDBytes)
	if err != nil {
		log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("could not read tag memory")
	}
	if res.Type == tokens.TypeAmiibo || res.Type == tokens.TypeLegoDimensions {
		// A toy's contents are not ZapScript. They stay available in Data.
		return res.Type, "", res.Data
	}
	return tokenType, text, res.Data
}

// memoryKind returns the tag kind to read memory as, or KindUnknown to skip.
//
// A MIFARE Classic tag is only read when it holds an NDEF message. go-pn532
// opens a blank tag with its factory key, and other readers report a blank
// tag with no data at all.
func (r *Reader) memoryKind(tagType pn532.TagType, msg *pn532.NDEFMessage) nfctag.Kind {
	switch tagType {
	case pn532.TagTypeNTAG:
		return nfctag.KindType2
	case pn532.TagTypeMIFARE:
		if msg == nil || len(msg.Records) == 0 {
			return nfctag.KindUnknown
		}
		if info, err := r.tagOps.GetTagInfo(); err == nil && info.Sectors > classic1KSectors {
			return nfctag.KindClassic4K
		}
		return nfctag.KindClassic1K
	case pn532.TagTypeFeliCa, pn532.TagTypeUnknown, pn532.TagTypeAny:
		return nfctag.KindUnknown
	default:
		return nfctag.KindUnknown
	}
}

// readNDEFMessage reads the tag's NDEF message through go-pn532, or returns
// nil when the tag has none or could not be read.
func (r *Reader) readNDEFMessage(ctx context.Context, detectedTag *pn532.DetectedTag) *pn532.NDEFMessage {
	// Initialize tagops from the already-detected tag.
	// We use InitFromDetectedTag instead of DetectTag because the polling loop
	// already detected the tag via InListPassiveTarget. Calling DetectTag would
	// perform a redundant detection with InRelease(0) which can corrupt tag state.
	if err := r.tagOps.InitFromDetectedTag(ctx, detectedTag); err != nil {
		logTraceableError(err, "init tag for NDEF")
		log.Warn().Err(err).
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Msg("failed to initialize tag for NDEF reading")
		return nil
	}

	r.logTagInfo(detectedTag)

	ndefMessage, err := r.tagOps.ReadNDEF(ctx)
	if err != nil {
		log.Warn().Err(err).
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Msg("failed to read NDEF data from tag")
		return nil
	}

	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		log.Warn().
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Bool("messageNil", ndefMessage == nil).
			Msg("tag has no NDEF records - may be blank or incompatible format")
	}
	return ndefMessage
}

const classic1KSectors = 16

// blockReader is go-pn532's block read. On a MIFARE Classic tag it
// authenticates each sector itself.
type blockReader interface {
	ReadBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error)
}

// blockIO gives the shared tag layer read access to a tag through go-pn532.
// Writes stay with go-pn532, so it does not offer them.
type blockIO struct {
	ops   blockReader
	type2 bool
}

var (
	_ nfctag.BlockIO       = (*blockIO)(nil)
	_ nfctag.Authenticator = (*blockIO)(nil)
)

var errReadOnly = errors.New("tag writes go through go-pn532")

func (b *blockIO) ReadBlock(ctx context.Context, addr int) ([]byte, error) {
	end := addr
	if b.type2 {
		// One native read returns four pages.
		end = addr + nfctag.BlockSize/4 - 1
	}
	if addr < 0 || addr > 0xFF {
		return nil, fmt.Errorf("block %d out of range", addr)
	}
	end = min(end, 0xFF)

	data, err := b.ops.ReadBlocks(ctx, byte(addr), byte(end))
	if err != nil {
		return nil, fmt.Errorf("read blocks %d-%d: %w", addr, end, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("read blocks %d-%d: no data", addr, end)
	}
	// go-pn532 stops at the tag's last page, so the final read of a tag
	// comes back short.
	block := make([]byte, nfctag.BlockSize)
	copy(block, data)
	return block, nil
}

func (*blockIO) WriteBlock(context.Context, int, []byte) error {
	return errReadOnly
}

// Authenticate has nothing to do: go-pn532 authenticates inside its reads.
func (*blockIO) Authenticate(context.Context, int, nfctag.KeyType, nfctag.Key) error {
	return nil
}

func readMemory(ctx context.Context, ops blockReader, kind nfctag.Kind, uid []byte) (nfctag.Result, error) {
	res, err := nfctag.Read(ctx, &nfctag.Tag{
		IO:   &blockIO{ops: ops, type2: kind == nfctag.KindType2},
		UID:  uid,
		Kind: kind,
	})
	if err != nil {
		return res, fmt.Errorf("read tag memory: %w", err)
	}
	return res, nil
}

// logTagInfo logs detailed tag information including manufacturer and type details.
// This helps identify clone tags and provides useful debugging information.
func (r *Reader) logTagInfo(detectedTag *pn532.DetectedTag) {
	mfr := detectedTag.Manufacturer()
	displayName := tagops.TagTypeDisplayName(detectedTag.Type)

	if !detectedTag.IsGenuine() {
		log.Warn().
			Str("uid", detectedTag.UID).
			Str("manufacturer", string(mfr)).
			Msg("tag appears to be a clone (unknown manufacturer)")
	} else {
		log.Debug().
			Str("uid", detectedTag.UID).
			Str("manufacturer", string(mfr)).
			Msg("tag manufacturer identified")
	}

	if r.tagOps == nil {
		return
	}

	info, err := r.tagOps.GetTagInfo()
	if err != nil {
		log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("failed to get detailed tag info")
		return
	}

	switch info.Type {
	case pn532.TagTypeNTAG:
		log.Info().
			Str("uid", detectedTag.UID).
			Str("type", displayName).
			Str("variant", info.NTAGType).
			Int("totalPages", info.TotalPages).
			Int("userMemory", info.UserMemory).
			Str("manufacturer", string(mfr)).
			Msg("NTAG tag details")
	case pn532.TagTypeMIFARE:
		log.Info().
			Str("uid", detectedTag.UID).
			Str("type", displayName).
			Str("variant", info.MIFAREType).
			Int("sectors", info.Sectors).
			Int("totalMemory", info.TotalMemory).
			Str("manufacturer", string(mfr)).
			Msg("MIFARE tag details")
	default:
		log.Info().
			Str("uid", detectedTag.UID).
			Str("type", displayName).
			Str("manufacturer", string(mfr)).
			Msg("tag details")
	}
}
