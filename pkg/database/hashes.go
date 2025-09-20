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

package database

// HashType represents a specific hash algorithm type
type HashType string

const (
	HashTypeCRC32 HashType = "crc32"
	HashTypeMD5   HashType = "md5"
	HashTypeSHA1  HashType = "sha1"
)

// HashConfig contains configuration for which hash types to compute
type HashConfig struct {
	CRC32 bool
	MD5   bool
	SHA1  bool
}

// AllHashTypes returns all available hash types
func AllHashTypes() []HashType {
	return []HashType{HashTypeCRC32, HashTypeMD5, HashTypeSHA1}
}

// ParseHashTypes converts string slice to HashType slice
func ParseHashTypes(hashStrings []string) []HashType {
	var hashTypes []HashType
	for _, hashStr := range hashStrings {
		switch HashType(hashStr) {
		case HashTypeCRC32, HashTypeMD5, HashTypeSHA1:
			hashTypes = append(hashTypes, HashType(hashStr))
		}
	}
	return hashTypes
}

// ToHashConfig converts HashType slice to HashConfig
func ToHashConfig(hashTypes []HashType) HashConfig {
	config := HashConfig{}
	for _, hashType := range hashTypes {
		switch hashType {
		case HashTypeCRC32:
			config.CRC32 = true
		case HashTypeMD5:
			config.MD5 = true
		case HashTypeSHA1:
			config.SHA1 = true
		}
	}
	return config
}

// ToStringSlice converts HashType slice to string slice
func ToStringSlice(hashTypes []HashType) []string {
	result := make([]string, 0, len(hashTypes))
	for _, hashType := range hashTypes {
		result = append(result, string(hashType))
	}
	return result
}

// MediaHash represents hash information for a media file
type MediaHash struct {
	SystemID   string
	MediaPath  string
	CRC32      string
	MD5        string
	SHA1       string
	DBID       int64
	ComputedAt int64
	FileSize   int64
}
