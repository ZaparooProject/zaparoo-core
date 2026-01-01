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

	// Use tagOps for tag operations (mocks won't reach here in tests)
	if r.tagOps == nil {
		log.Debug().Str("uid", detectedTag.UID).Msg("tagOps not available, returning target data")
		return "", detectedTag.TargetData
	}

	// Use reader's context with timeout to ensure proper cancellation
	ctx, cancel := context.WithTimeout(r.ctx, ndefReadTimeout)
	defer cancel()

	// Initialize tagops from the already-detected tag.
	// We use InitFromDetectedTag instead of DetectTag because the polling loop
	// already detected the tag via InListPassiveTarget. Calling DetectTag would
	// perform a redundant detection with InRelease(0) which can corrupt tag state.
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting tagOps.InitFromDetectedTag")
	if err := r.tagOps.InitFromDetectedTag(ctx, detectedTag); err != nil {
		logTraceableError(err, "init tag for NDEF")
		log.Warn().Err(err).
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Msg("failed to initialize tag for NDEF reading")
		return "", detectedTag.TargetData
	}
	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: tagOps.InitFromDetectedTag completed")

	r.logTagInfo(detectedTag)

	log.Debug().Str("uid", detectedTag.UID).Msg("NDEF: starting tagOps.ReadNDEF")
	ndefMessage, err := r.tagOps.ReadNDEF(ctx)
	log.Debug().Err(err).Str("uid", detectedTag.UID).Msg("NDEF: tagOps.ReadNDEF completed")
	if err != nil {
		logTraceableError(err, "read NDEF")
		log.Warn().Err(err).
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Msg("failed to read NDEF data from tag")
		return "", detectedTag.TargetData
	}

	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		log.Warn().
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Bool("messageNil", ndefMessage == nil).
			Msg("tag has no NDEF records - may be blank or incompatible format")
		return "", detectedTag.TargetData
	}

	record := ndefMessage.Records[0]
	log.Debug().
		Str("uid", detectedTag.UID).
		Int("recordCount", len(ndefMessage.Records)).
		Str("recordType", string(record.Type)).
		Bool("hasText", record.Text != "").
		Bool("hasURI", record.URI != "").
		Int("payloadLen", len(record.Payload)).
		Msg("NDEF: processing records")

	tokenText := convertNDEFToTokenText(ndefMessage)

	if tokenText == "" {
		log.Warn().
			Str("uid", detectedTag.UID).
			Str("tagType", string(detectedTag.Type)).
			Str("recordType", string(record.Type)).
			Int("payloadLen", len(record.Payload)).
			Msg("NDEF records found but no text/URI could be extracted - unsupported format")
	}

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

	// Only first record is used (NDEF standard primary payload)
	record := ndefMessage.Records[0]

	if record.Text != "" {
		return record.Text
	}

	if record.URI != "" {
		return record.URI
	}

	if record.WiFi != nil {
		return convertWiFiToJSON(record.WiFi)
	}

	if record.VCard != nil {
		return convertVCardToJSON(record.VCard)
	}

	if record.Type == pn532.NDEFTypeSmartPoster {
		return convertSmartPosterToJSON(record.Payload)
	}

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
