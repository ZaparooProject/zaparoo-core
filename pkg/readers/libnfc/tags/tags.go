//go:build linux

/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett
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
	"encoding/hex"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/clausecker/nfc/v2"
)

const (
	WriteCommand = byte(0xA2)
	ReadCommand  = byte(0x30)
)

var SupportedCardTypes = []nfc.Modulation{
	{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106},
}

type TagData struct {
	Type  string
	Bytes []byte
}

func GetTagUID(target nfc.Target) string {
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

func comm(pnd nfc.Device, tx []byte, replySize int) ([]byte, error) {
	rx := make([]byte, replySize)

	timeout := 0
	_, err := pnd.InitiatorTransceiveBytes(tx, rx, timeout)
	if err != nil {
		return nil, fmt.Errorf("comm error: %s", err)
	}

	return rx, nil
}

func GetTagType(target nfc.Target) string {
	switch target.Modulation() {
	case nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}:
		var card = target.(*nfc.ISO14443aTarget)
		if card.Atqa == [2]byte{0x00, 0x04} && card.Sak == 0x08 {
			// https://www.nxp.com/docs/en/application-note/AN10833.pdf page 9
			return tokens.TypeMifare
		}
		if card.Atqa == [2]byte{0x00, 0x44} && card.Sak == 0x00 {
			// https://www.nxp.com/docs/en/data-sheet/NTAG213_215_216.pdf page 33
			return tokens.TypeNTAG
		}
	}
	return ""
}
