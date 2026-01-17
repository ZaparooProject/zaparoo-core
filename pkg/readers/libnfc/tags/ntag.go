//go:build linux

/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package tags

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/ndef"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/rs/zerolog/log"
)

const (
	Ntag213CapacityBytes = 114
	Ntag213Identifier    = 0x12

	Ntag215CapacityBytes = 496
	Ntag215Identifier    = 0x3E

	Ntag216CapacityBytes = 872
	Ntag216Identifier    = 0x6D
)

// Can be identified by matching blocks 0x03-0x07
// https://github.com/RfidResearchGroup/proxmark3/blob/master/client/src/cmdhfmfu.c
var LegoDimensionsMatcher = []byte{
	// 0xE1, 0x10, 0x12, 0x00, // Skip as we never read 0x03
	0x01, 0x03, 0xA0, 0x0C,
	0x34, 0x03, 0x13, 0xD1,
	0x01, 0x0F, 0x54, 0x02,
	0x65, 0x6E,
}

// Can be identified by matching address 0x09-0x0F
var AmiiboMatcher = []byte{
	0x48, 0x0F, 0xE0,
	0xF1, 0x10, 0xFF, 0xEE,
}

func ReadNtag(pnd nfc.Device) (TagData, error) {
	blockCount, err := getNtagBlockCount(pnd)
	if err != nil {
		return TagData{}, err
	}

	log.Debug().Msgf("NTAG has %d blocks", blockCount)

	header, _ := comm(pnd, []byte{ReadCommand, byte(0)}, 16)
	if len(header) > 9 && bytes.Equal(header[9:], AmiiboMatcher) {
		log.Info().Msg("found Amiibo")
		amiibo, _ := comm(pnd, []byte{ReadCommand, byte(21)}, 16)
		amiibo = amiibo[:8]
		log.Info().Msg("Amiibo identifier:" + hex.EncodeToString(amiibo))
		return TagData{
			Type:  tokens.TypeAmiibo,
			Bytes: amiibo,
		}, nil
	}

	allBlocks := make([]byte, 0)
	currentBlock := 4

	for i := 0; i <= (blockCount / 4); i++ {
		blocks, err := comm(pnd, []byte{ReadCommand, byte(currentBlock)}, 16)
		if err != nil {
			return TagData{}, err
		}

		if byte(currentBlock) == 0x04 && len(blocks) >= 13 && bytes.Equal(blocks[0:14], LegoDimensionsMatcher) {
			log.Info().Msg("found Lego Dimensions tag")
			return TagData{
				Type:  tokens.TypeLegoDimensions,
				Bytes: []byte{},
			}, nil
		}

		allBlocks = append(allBlocks, blocks...)
		currentBlock += 4

		if bytes.Contains(allBlocks, ndef.NdefEnd) {
			// Once we find the end of the NDEF text record there is no need to
			// continue reading the rest of the card.
			// This should make things "load" quicker
			log.Debug().Msg("found end of ndef record")
			break
		}
	}

	return TagData{
		Type:  tokens.TypeNTAG,
		Bytes: allBlocks,
	}, nil
}

func WriteNtag(pnd nfc.Device, text string) ([]byte, error) {
	payload, err := ndef.BuildTextMessage(text)
	if err != nil {
		return nil, fmt.Errorf("failed to build NDEF text message: %w", err)
	}

	cardCapacity, err := getNtagCapacity(pnd)
	if err != nil {
		return nil, err
	}

	if len(payload) > cardCapacity {
		return nil, fmt.Errorf("payload too big for card: [%d/%d] bytes used", len(payload), cardCapacity)
	}

	var startingBlock byte = 0x04
	for i, chunk := range chunkBy(payload, 4) {
		for len(chunk) < 4 {
			chunk = append(chunk, []byte{0x00}...)
		}
		tx := make([]byte, 0, 2+len(chunk))
		tx = append(tx, WriteCommand, startingBlock+byte(i))
		tx = append(tx, chunk...)
		_, err := comm(pnd, tx, 1)
		if err != nil {
			return nil, err
		}
	}

	return payload, nil
}

func readCapabilityContainer(pnd nfc.Device) ([]byte, error) {
	// Find tag capacity by looking in block 3 (capability container)
	tx := []byte{ReadCommand, 0x03}
	rx := make([]byte, 16)

	timeout := 0
	_, err := pnd.InitiatorTransceiveBytes(tx, rx, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to read NTAG capability container: %w", err)
	}
	return rx, nil
}

func getNtagBlockCount(pnd nfc.Device) (int, error) {
	rx, err := readCapabilityContainer(pnd)
	if err != nil {
		return 0, err
	}

	switch rx[2] {
	case 0x12:
		// NTAG213. (144 -4) / 4
		return 35, nil
	case 0x3E:
		// NTAG215. (504 - 4) / 4
		return 125, nil
	case 0x6D:
		// NTAG216. (888 -4) / 4
		return 221, nil
	default:
		// fallback to NTAG213
		return 35, nil
	}
}

func getNtagCapacity(pnd nfc.Device) (int, error) {
	rx, err := readCapabilityContainer(pnd)
	if err != nil {
		return 0, err
	}

	// https://github.com/adafruit/Adafruit_MFRC630/blob/master/docs/NTAG.md#capability-container
	switch rx[2] {
	case Ntag213Identifier:
		return Ntag213CapacityBytes, nil
	case Ntag215Identifier:
		return Ntag215CapacityBytes, nil
	case Ntag216Identifier:
		return Ntag216CapacityBytes, nil
	default:
		// fallback
		return Ntag213CapacityBytes, nil
	}
}

func chunkBy[T any](items []T, chunkSize int) [][]T {
	expectedCap := (len(items) + chunkSize - 1) / chunkSize
	chunks := make([][]T, 0, expectedCap)
	for chunkSize < len(items) {
		items, chunks = items[chunkSize:], append(chunks, items[0:chunkSize:chunkSize])
	}
	return append(chunks, items)
}
