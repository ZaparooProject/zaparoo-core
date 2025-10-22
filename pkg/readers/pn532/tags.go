// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"encoding/hex"
	"encoding/json"
	"strings"

	pn533 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/tagops"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/hsanjuan/go-ndef"
	"github.com/rs/zerolog/log"
)

func (*Reader) convertTagType(tagType pn533.TagType) string {
	switch tagType {
	case pn533.TagTypeNTAG:
		return tokens.TypeNTAG
	case pn533.TagTypeMIFARE:
		return tokens.TypeMifare
	case pn533.TagTypeFeliCa:
		return tokens.TypeFeliCa
	case pn533.TagTypeUnknown, pn533.TagTypeAny:
		return tokens.TypeUnknown
	default:
		return tokens.TypeUnknown
	}
}

func (r *Reader) readNDEFData(detectedTag *pn533.DetectedTag) (uid string, data []byte) {
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting readNDEFData")

	// Use realDevice for tag operations (mocks won't reach here in tests)
	if r.realDevice == nil {
		log.Debug().Str("uid", detectedTag.UID).Msg("real device not available, returning target data")
		return "", detectedTag.TargetData
	}

	tagOps := tagops.New(r.realDevice)

	// Detect tag first - use reader's context to ensure proper cancellation
	ctx, cancel := context.WithTimeout(r.ctx, ndefReadTimeout)
	defer cancel()

	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting tagOps.DetectTag")
	if err := tagOps.DetectTag(ctx); err != nil {
		log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("failed to detect tag for NDEF reading")
		return "", detectedTag.TargetData
	}
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: tagOps.DetectTag completed")

	// Read NDEF message
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting tagOps.ReadNDEF")
	ndefMessage, err := tagOps.ReadNDEF(ctx)
	log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("NDEF: tagOps.ReadNDEF completed")
	if err != nil {
		log.Debug().Err(err).Msg("failed to read NDEF data")
		return "", detectedTag.TargetData
	}

	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return "", detectedTag.TargetData
	}

	// Process NDEF records and convert to token text
	tokenText := r.convertNDEFToTokenText(ndefMessage)

	// Return the token text and original target data
	return tokenText, detectedTag.TargetData
}

// convertNDEFToTokenText converts NDEF message to token text:
// - Text and URI records pass through directly
// - All other types (WiFi, VCard, etc.) convert to JSON
func (*Reader) convertNDEFToTokenText(ndefMessage *ndef.Message) string {
	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return ""
	}

	// Process first record (primary content)
	record := ndefMessage.Records[0]
	tnf := record.TNF()
	typeField := record.Type()

	// Handle text records - pass through directly
	if tnf == ndef.NFCForumWellKnownType && len(typeField) == 1 && typeField[0] == 'T' {
		payload, err := record.Payload()
		if err != nil {
			return ""
		}
		payloadBytes := payload.Marshal()
		if len(payloadBytes) > 3 {
			// Skip language code to get actual text
			langLen := int(payloadBytes[0] & 0x3F)
			if len(payloadBytes) > langLen+1 {
				return string(payloadBytes[langLen+1:])
			}
		}
		return ""
	}

	// Handle URI records - pass through directly
	if tnf == ndef.NFCForumWellKnownType && len(typeField) == 1 && typeField[0] == 'U' {
		payload, err := record.Payload()
		if err != nil {
			return ""
		}
		return string(payload.Marshal())
	}

	// Handle WiFi credentials - convert to JSON
	if typeField == "application/vnd.wfa.wsc" {
		return convertWiFiToJSON(record)
	}

	// Handle VCard records - convert to JSON
	if typeField == "text/vcard" || typeField == "text/x-vcard" {
		return convertVCardToJSON(record)
	}

	// Handle Smart Poster records - convert to JSON
	if tnf == ndef.NFCForumWellKnownType && len(typeField) == 2 && typeField == "Sp" {
		return convertSmartPosterToJSON(record)
	}

	// For any other complex types, convert to generic JSON
	return convertGenericRecordToJSON(record)
}

func convertWiFiToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	wifiData := map[string]any{
		"type": "wifi",
		"raw":  hex.EncodeToString(payload.Marshal()),
	}

	// Try to parse WiFi credentials if possible
	// This is a simplified approach - full parsing would require WSC binary format parsing
	wifiData["note"] = "WiFi credentials (binary format)"

	jsonBytes, err := json.Marshal(wifiData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal WiFi data to JSON")
		return ""
	}
	return string(jsonBytes)
}

func convertVCardToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	vcardText := string(payload.Marshal())

	vcardData := map[string]any{
		"type":  "vcard",
		"vcard": vcardText,
	}

	// Try to extract basic contact info
	lines := strings.Split(vcardText, "\n")
	contact := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "FN:"):
			contact["name"] = strings.TrimPrefix(line, "FN:")
		case strings.HasPrefix(line, "TEL:"):
			contact["phone"] = strings.TrimPrefix(line, "TEL:")
		case strings.HasPrefix(line, "EMAIL:"):
			contact["email"] = strings.TrimPrefix(line, "EMAIL:")
		}
	}

	if len(contact) > 0 {
		vcardData["contact"] = contact
	}

	jsonBytes, err := json.Marshal(vcardData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal VCard data to JSON")
		return ""
	}
	return string(jsonBytes)
}

func convertSmartPosterToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	posterData := map[string]any{
		"type": "smartposter",
		"raw":  hex.EncodeToString(payload.Marshal()),
	}

	jsonBytes, err := json.Marshal(posterData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal smart poster data to JSON")
		return ""
	}
	return string(jsonBytes)
}

func convertGenericRecordToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	genericData := map[string]any{
		"type":      "unknown",
		"tnf":       record.TNF(),
		"typeField": record.Type(),
		"payload":   hex.EncodeToString(payload.Marshal()),
	}

	jsonBytes, err := json.Marshal(genericData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal generic record data to JSON")
		return ""
	}
	return string(jsonBytes)
}
