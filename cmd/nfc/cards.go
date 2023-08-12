package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/clausecker/nfc/v2"
)

const (
	TypeNTAG213 = "NTAG213"
	TypeNTAG215 = "NTAG215"
	TypeNTAG216 = "NTAG216"
)

func getDataAreaSize(cardType string) int {
	switch cardType {
	// https://www.shopnfc.com/en/content/6-nfc-tags-specs

	case TypeNTAG213:
		// Block 0x04 to 0x27 = 0x23 (35)
		// Or capacity (144 - 4) / 4
		return 35
	case TypeNTAG215:
		// Guessing this is (504 - 4) / 4 = 125
		return 125
	case TypeNTAG216:
		// Block 0x04 to 0xE1 = 0xDD (221)
		// Or capacity (888 - 4) / 4
		return 221
	default:
		return 35 // fallback to NTAG213
	}
}

func readRecord(pnd nfc.Device, blockCount int) ([]byte, error) {
	allBlocks := make([]byte, 0)
	offset := 4

	for i := 0; i <= (blockCount / 4); i++ {
		blocks, err := readFourBlocks(pnd, byte(offset))
		if err != nil {
			return nil, err
		}
		allBlocks = append(allBlocks, blocks...)
		offset = offset + 4
	}

	return allBlocks, nil
}

func parseRecordText(blocks []byte) string {
	// Find the text NDEF record
	startIndex := bytes.Index(blocks, []byte{0x54, 0x02, 0x65, 0x6E})
	endIndex := bytes.Index(blocks, []byte{0xFE})

	if startIndex != -1 && endIndex != -1 {
		tagText := string(blocks[startIndex+4 : endIndex])
		return tagText
	}

	return ""
}

func getCardUID(target nfc.Target) string {
	var uid string
	switch target.Modulation() {
	case nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}:
		var card = target.(*nfc.ISO14443aTarget)
		var ID = card.UID
		uid = hex.EncodeToString(ID[:card.UIDLen])
		break
	default:
		uid = ""
	}
	return uid
}

func readFourBlocks(pnd nfc.Device, offset byte) ([]byte, error) {
	// Read 16 bytes at a time from a Type 2 tag
	// For NTAG this would be 4 blocks or pages.
	tx := []byte{0x30, offset}
	rx := make([]byte, 16)

	timeout := 0
	_, err := pnd.InitiatorTransceiveBytes(tx, rx, timeout)
	if err != nil {
		return nil, fmt.Errorf("error reading blocks: %s", err)
	}

	return rx, nil
}

func getCardType(pnd nfc.Device) (string, error) {
	// Find tag capacity by looking in block 3 (capability container)
	tx := []byte{0x30, 0x03}
	rx := make([]byte, 16)

	timeout := 0
	_, err := pnd.InitiatorTransceiveBytes(tx, rx, timeout)
	if err != nil {
		return "", fmt.Errorf("error card type: %s", err)
	}

	switch rx[2] {
	case 0x12:
		return TypeNTAG213, nil
	case 0x3E:
		return TypeNTAG215, nil
	case 0x6D:
		return TypeNTAG216, nil
	default:
		return "", fmt.Errorf("unknown card type: %v", rx[2])
	}
}