/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

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

package nfctag

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// ErrUnsupported is readers.ErrUnsupportedTagType, so the write path logs it
// as an expected user condition.
var ErrUnsupported = readers.ErrUnsupportedTagType

// errEndOfMemory stops a read at the end of the tag's user memory.
var errEndOfMemory = errors.New("end of tag memory")

// readThroughNDEF extends mem, which already holds the first block of user
// memory, until it covers the NDEF data and its terminator. next returns the
// following block, or errEndOfMemory when there is none.
//
// The NDEF length comes from the tag, so the read is bounded by next running
// out of memory, never by that length.
func readThroughNDEF(mem []byte, next func() ([]byte, error)) ([]byte, error) {
	loc, err := pn532.ScanForNDEFTLV(mem)
	if err != nil {
		// The TLV before the NDEF data, or the NDEF header itself, can run
		// past the first block. A tag with nothing in two blocks has no NDEF.
		block, nextErr := next()
		if nextErr != nil {
			return mem, endOfMemoryIsNil(nextErr)
		}
		mem = append(mem, block...)
		loc, err = pn532.ScanForNDEFTLV(mem)
		if err != nil {
			return mem, nil //nolint:nilerr // no NDEF on the tag is a normal result
		}
	}

	need := loc.Offset + loc.Length + 1
	for len(mem) < need {
		block, nextErr := next()
		if nextErr != nil {
			return mem, endOfMemoryIsNil(nextErr)
		}
		mem = append(mem, block...)
	}
	return mem, nil
}

func endOfMemoryIsNil(err error) error {
	if errors.Is(err, errEndOfMemory) {
		return nil
	}
	return err
}

// TextFromMemory returns the token text for tag memory that starts at the
// first user page or block. Memory with no usable NDEF gives an empty string.
func TextFromMemory(mem []byte) string {
	msg, err := pn532.ParseNDEFMessage(mem)
	if err != nil {
		return ""
	}
	return TextFromMessage(msg)
}

// TextFromMessage converts an NDEF message to token text. Only the first
// record is used. Text and URI records pass through; Wi-Fi, vCard, smart
// poster and other records become JSON.
func TextFromMessage(msg *pn532.NDEFMessage) string {
	if msg == nil || len(msg.Records) == 0 {
		return ""
	}
	record := msg.Records[0]

	if record.Text != "" {
		return record.Text
	}
	if record.URI != "" {
		return record.URI
	}
	// A text or URI record with nothing in it, or one that failed to parse,
	// is empty text: not a JSON description of the bytes that did not parse.
	if record.Type == pn532.NDEFTypeText || record.Type == pn532.NDEFTypeURI {
		return ""
	}
	if record.Type == pn532.NDEFTypeSmartPoster {
		return toJSON(map[string]any{
			"type": "smartposter",
			"raw":  hex.EncodeToString(record.Payload),
		})
	}
	if record.WiFi != nil {
		return wifiJSON(record.WiFi)
	}
	if record.VCard != nil {
		return vcardJSON(record.VCard)
	}
	if len(record.Payload) > 0 {
		return toJSON(map[string]any{
			"type":      "unknown",
			"typeField": string(record.Type),
			"payload":   hex.EncodeToString(record.Payload),
		})
	}
	return ""
}

func wifiJSON(wifi *pn532.WiFiCredential) string {
	data := map[string]any{
		"type": "wifi",
		"ssid": wifi.SSID,
	}
	if wifi.NetworkKey != "" {
		data["networkKey"] = wifi.NetworkKey
	}
	if wifi.AuthType != 0 {
		data["authType"] = wifi.AuthType
	}
	if wifi.EncryptionType != 0 {
		data["encryptionType"] = wifi.EncryptionType
	}
	if wifi.MACAddress != "" {
		data["macAddress"] = wifi.MACAddress
	}
	return toJSON(data)
}

func vcardJSON(vcard *pn532.VCardContact) string {
	contact := make(map[string]any)
	if vcard.FormattedName != "" {
		contact["name"] = vcard.FormattedName
	}
	if len(vcard.PhoneNumbers) > 0 {
		contact["phones"] = vcard.PhoneNumbers
	}
	if len(vcard.EmailAddresses) > 0 {
		contact["emails"] = vcard.EmailAddresses
	}
	if vcard.Organization != "" {
		contact["organization"] = vcard.Organization
	}
	if vcard.Title != "" {
		contact["title"] = vcard.Title
	}
	if vcard.URL != "" {
		contact["url"] = vcard.URL
	}

	data := map[string]any{"type": "vcard"}
	if len(contact) > 0 {
		data["contact"] = contact
	}
	return toJSON(data)
}

func toJSON(data map[string]any) string {
	encoded, err := json.Marshal(data)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal NDEF record to JSON")
		return ""
	}
	return string(encoded)
}

// buildTextMessage returns the bytes written to a tag for text: one NDEF text
// record, wrapped in its TLV and followed by the terminator.
func buildTextMessage(text string) ([]byte, error) {
	payload, err := pn532.BuildNDEFMessageEx([]pn532.NDEFRecord{{
		Type: pn532.NDEFTypeText,
		Text: text,
	}})
	if err != nil {
		return nil, fmt.Errorf("build NDEF message: %w", err)
	}
	return payload, nil
}

// padTo returns data extended with zeros to a multiple of size.
func padTo(data []byte, size int) []byte {
	if rem := len(data) % size; rem != 0 {
		padded := make([]byte, len(data)+size-rem)
		copy(padded, data)
		return padded
	}
	return data
}
