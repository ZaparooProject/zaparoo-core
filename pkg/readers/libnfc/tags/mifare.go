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

	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/rs/zerolog/log"
	"golang.org/x/exp/slices"
)

const (
	MifareWritableSectorCount     = 15
	MifareWritableBlocksPerSector = 3
	MifareBlockSizeBytes          = 16
)

// buildMifareAuthCommand returns a command to authenticate against a block
func buildMifareAuthCommand(block byte, cardUID string) []byte {
	command := []byte{
		// Auth using key A
		0x60, block,
		// Using the NDEF well known private key
		0xd3, 0xf7, 0xd3, 0xf7, 0xd3, 0xf7,
	}
	// And finally append the tag UID to the end
	uidBytes, _ := hex.DecodeString(cardUID)
	return append(command, uidBytes...)
}

// ReadMifare reads data from all blocks in sectors 1-15
func ReadMifare(pnd nfc.Device, cardUID string) (TagData, error) {
	permissionSectors := []int{4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60}
	var allBlocks []byte
	for block := 0; block < 64; block++ {
		if block <= 3 {
			// The first sector contains infomation we don't care about and
			// also has a different key (0xA0A1A2A3A4A5) YAGNI, so skip over
			continue
		}

		// The last block of a sector contains KeyA + Permissions + KeyB
		// We don't care about that info so skip if present.
		if slices.Contains(permissionSectors, block+1) {
			continue
		}

		// Mifare is split up into 16 sectors each containing 4 blocks.
		// We need to authenticate before any read/ write operations can be performed
		// Only need to authenticate once per sector
		if block%4 == 0 {
			_, err := comm(pnd, buildMifareAuthCommand(byte(block), cardUID), 2)
			if err != nil {
				log.Warn().Err(err).Msg("authenticating sector error")
			}
		}

		blockData, err := comm(pnd, []byte{0x30, byte(block)}, 16)
		if err != nil {
			return TagData{}, err
		}

		allBlocks = append(allBlocks, blockData...)

		if bytes.Contains(blockData, NdefEnd) {
			// Once we find the end of the NDEF text record there is no need to
			// continue reading the rest of the card.
			// This should make things "load" quicker
			break
		}
	}

	return TagData{
		Type:  tokens.TypeMifare,
		Bytes: allBlocks,
	}, nil
}

// getMifareCapacityInBytes returns the Mifare card capacity
func getMifareCapacityInBytes() int {
	return (MifareWritableBlocksPerSector * MifareWritableSectorCount) * MifareBlockSizeBytes
}

// WriteMifare writes the given text string to a Mifare card starting from sector, skipping any trailer blocks
func WriteMifare(pnd nfc.Device, text string, cardUID string) ([]byte, error) {
	payload, err := BuildMessage(text)
	if err != nil {
		return nil, err
	}

	cardCapacity := getMifareCapacityInBytes()
	if len(payload) > cardCapacity {
		return nil, fmt.Errorf("payload too big for card: [%d/%d] bytes used", len(payload), cardCapacity)
	}

	payloadChunks := chunkBy(payload, 16)
	chunks := make([][]byte, 0, len(payloadChunks))
	for _, chunk := range payloadChunks {
		for len(chunk) < 16 {
			chunk = append(chunk, []byte{0x00}...)
		}
		chunks = append(chunks, chunk)
	}

	chunkIndex := 0
	for sector := 1; sector <= 15; sector++ {
		// Iterate over blocks in sector (0-2) skipping trailer block (3)
		for sectorIndex := 0; sectorIndex < 3; sectorIndex++ {
			blockToWrite := (sector * 4) + sectorIndex
			if sectorIndex == 0 {
				// We changed sectors, time to authenticate
				_, err := comm(pnd, buildMifareAuthCommand(byte(blockToWrite), cardUID), 2)
				if err != nil {
					return nil, err
				}
			}

			writeBlockCommand := append([]byte{0xA0, byte(blockToWrite)}, chunks[chunkIndex]...)
			_, err := comm(pnd, writeBlockCommand, 2)
			if err != nil {
				return nil, err
			}
			chunkIndex++
			if chunkIndex >= len(chunks) {
				// All data has been written, we are done
				return payload, nil
			}
		}
	}

	return payload, nil
}
