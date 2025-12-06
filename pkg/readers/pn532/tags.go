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

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/tagops"
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

func (r *Reader) readNDEFData(detectedTag *pn532.DetectedTag) (uid string, data []byte) {
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
		logTraceableError(err, "detect tag for NDEF")
		log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("failed to detect tag for NDEF reading")
		return "", detectedTag.TargetData
	}
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: tagOps.DetectTag completed")

	// Read NDEF message
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting tagOps.ReadNDEF")
	ndefMessage, err := tagOps.ReadNDEF(ctx)
	log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("NDEF: tagOps.ReadNDEF completed")
	if err != nil {
		logTraceableError(err, "read NDEF")
		log.Debug().Err(err).Msg("failed to read NDEF data")
		return "", detectedTag.TargetData
	}

	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return "", detectedTag.TargetData
	}

	// Process NDEF records and convert to token text
	tokenText := convertNDEFToTokenText(ndefMessage)

	// Return the token text and original target data
	return tokenText, detectedTag.TargetData
}

// convertNDEFToTokenText converts NDEF message to token text:
// - Text and URI records pass through directly
// - WiFi and VCard records convert to JSON
// - Other types convert to generic JSON with payload
func convertNDEFToTokenText(ndefMessage *pn532.NDEFMessage) string {
	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return ""
	}

	// Process first record (primary content)
	record := ndefMessage.Records[0]

	// Handle text records - pass through directly
	if record.Text != "" {
		return record.Text
	}

	// Handle URI records - pass through directly
	if record.URI != "" {
		return record.URI
	}

	// Handle WiFi credentials - convert to JSON
	if record.WiFi != nil {
		return convertWiFiToJSON(record.WiFi)
	}

	// Handle VCard records - convert to JSON
	if record.VCard != nil {
		return convertVCardToJSON(record.VCard)
	}

	// Handle Smart Poster records - convert to JSON
	if record.Type == pn532.NDEFTypeSmartPoster {
		return convertSmartPosterToJSON(record.Payload)
	}

	// For any other types with payload, convert to generic JSON
	if len(record.Payload) > 0 {
		return convertGenericRecordToJSON(string(record.Type), record.Payload)
	}

	return ""
}

func convertWiFiToJSON(wifi *pn532.WiFiCredential) string {
	wifiData := map[string]any{
		"type": "wifi",
		"ssid": wifi.SSID,
	}

	if wifi.NetworkKey != "" {
		wifiData["networkKey"] = wifi.NetworkKey
	}
	if wifi.AuthType != 0 {
		wifiData["authType"] = wifi.AuthType
	}
	if wifi.EncryptionType != 0 {
		wifiData["encryptionType"] = wifi.EncryptionType
	}
	if wifi.MACAddress != "" {
		wifiData["macAddress"] = wifi.MACAddress
	}

	jsonBytes, err := json.Marshal(wifiData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal WiFi data to JSON")
		return ""
	}
	return string(jsonBytes)
}

func convertVCardToJSON(vcard *pn532.VCardContact) string {
	vcardData := map[string]any{
		"type": "vcard",
	}

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

func convertSmartPosterToJSON(payload []byte) string {
	posterData := map[string]any{
		"type": "smartposter",
		"raw":  hex.EncodeToString(payload),
	}

	jsonBytes, err := json.Marshal(posterData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal smart poster data to JSON")
		return ""
	}
	return string(jsonBytes)
}

func convertGenericRecordToJSON(recordType string, payload []byte) string {
	genericData := map[string]any{
		"type":      "unknown",
		"typeField": recordType,
		"payload":   hex.EncodeToString(payload),
	}

	jsonBytes, err := json.Marshal(genericData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to marshal generic record data to JSON")
		return ""
	}
	return string(jsonBytes)
}
