//go:build linux

/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023, 2024 Callan Barrett

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
	"encoding/binary"
	"fmt"

	"github.com/hsanjuan/go-ndef"
)

var NdefEnd = []byte{0xFE}
var NdefStart = []byte{0x54, 0x02, 0x65, 0x6E}

func ParseRecordText(blocks []byte) (string, error) {
	startIndex := bytes.Index(blocks, NdefStart)
	if startIndex == -1 {
		return "", fmt.Errorf("NDEF start not found: %x", blocks)
	}

	endIndex := bytes.Index(blocks, NdefEnd)
	if endIndex == -1 {
		return "", fmt.Errorf("NDEF end not found: %x", blocks)
	}

	if startIndex >= endIndex || startIndex+4 >= len(blocks) {
		return "", fmt.Errorf("start index out of bounds: %d, %x", startIndex, blocks)
	}

	if endIndex <= startIndex || endIndex >= len(blocks) {
		return "", fmt.Errorf("end index out of bounds: %d, %x", endIndex, blocks)
	}

	tagText := string(blocks[startIndex+4 : endIndex])

	return tagText, nil
}

func BuildMessage(text string) ([]byte, error) {
	msg := ndef.NewTextMessage(text, "en")
	payload, err := msg.Marshal()
	if err != nil {
		return nil, err
	}

	header, err := CalculateNdefHeader(payload)
	if err != nil {
		return nil, err
	}
	payload = append(header, payload...)
	payload = append(payload, []byte{0xFE}...)
	return payload, nil
}

func CalculateNdefHeader(ndefRecord []byte) ([]byte, error) {
	var recordLength = len(ndefRecord)
	if recordLength < 255 {
		return []byte{0x03, byte(len(ndefRecord))}, nil
	}

	// NFCForum-TS-Type-2-Tag_1.1.pdf Page 9
	// > 255 Use three consecutive bytes format
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, uint16(recordLength))
	if err != nil {
		return nil, err
	}

	var header = []byte{0x03, 0xFF}
	return append(header, buf.Bytes()...), nil
}
