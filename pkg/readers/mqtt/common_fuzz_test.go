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

package mqtt

import (
	"testing"
)

// FuzzParseMQTTPath tests MQTT path parsing with arbitrary strings to discover
// edge cases in URL parsing and broker/topic extraction.
func FuzzParseMQTTPath(f *testing.F) {
	// Valid formats
	f.Add("localhost:1883/zaparoo/tokens")
	f.Add("mqtt.example.com:8883/home/zaparoo")
	f.Add("10.0.0.1:1883/topic")
	f.Add("broker:1883/a/b/c/d")

	// With scheme prefixes
	f.Add("mqtt://localhost:1883/topic")
	f.Add("mqtts://localhost:8883/topic")

	// Edge cases
	f.Add("")
	f.Add("localhost:1883")
	f.Add("/topic")
	f.Add(":")
	f.Add("://")
	f.Add("localhost:1883/")
	f.Add(":1883/topic")
	f.Add("localhost:/topic")
	f.Add("mqtt://")
	f.Add("mqtt:///topic")

	// Special characters
	f.Add("[::1]:1883/topic")
	f.Add("host:1883/topic with spaces")
	f.Add("host:1883/topic%20encoded")
	f.Add("host:1883/\u65e5\u672c\u8a9e/topic")
	f.Add("user:pass@host:1883/topic")

	f.Fuzz(func(t *testing.T, path string) {
		broker, topic, err := ParseMQTTPath(path)

		// Empty input must always error
		if path == "" && err == nil {
			t.Error("empty path should produce an error")
		}

		// Successful parse must have non-empty broker and topic
		if err == nil {
			if broker == "" {
				t.Error("successful parse returned empty broker")
			}
			if topic == "" {
				t.Error("successful parse returned empty topic")
			}
		}

		// Determinism check
		broker2, topic2, err2 := ParseMQTTPath(path)
		if (err == nil) != (err2 == nil) {
			t.Errorf("non-deterministic error for input %q", path)
		}
		if err == nil && (broker != broker2 || topic != topic2) {
			t.Errorf("non-deterministic result for input %q", path)
		}
	})
}

// FuzzParseMQTTProtocol tests MQTT protocol parsing with arbitrary URL strings
// to discover edge cases in scheme extraction and TLS detection.
func FuzzParseMQTTProtocol(f *testing.F) {
	// Standard schemes
	f.Add("mqtt://broker:1883")
	f.Add("mqtts://broker:8883")
	f.Add("ssl://broker:8883")
	f.Add("tcp://broker:1883")

	// No scheme
	f.Add("broker:1883")
	f.Add("localhost")

	// Edge cases
	f.Add("")
	f.Add("://")
	f.Add("://broker")
	f.Add("mqtt://")
	f.Add("a://b://c")
	f.Add("MQTT://broker:1883")
	f.Add("MQTTS://broker:8883")

	// Special characters
	f.Add("mqtt://[::1]:1883")
	f.Add("mqtt://user:pass@broker:1883")

	f.Fuzz(func(t *testing.T, urlStr string) {
		info := ParseMQTTProtocol(urlStr)

		// Protocol must be either "tcp" or "ssl"
		if info.Protocol != "tcp" && info.Protocol != "ssl" {
			t.Errorf("unexpected protocol %q for input %q", info.Protocol, urlStr)
		}

		// UseTLS must be consistent with protocol
		if info.Protocol == "ssl" && !info.UseTLS {
			t.Errorf("ssl protocol but UseTLS=false for input %q", urlStr)
		}
		if info.Protocol == "tcp" && info.UseTLS {
			t.Errorf("tcp protocol but UseTLS=true for input %q", urlStr)
		}

		// Determinism check
		info2 := ParseMQTTProtocol(urlStr)
		if info != info2 {
			t.Errorf("non-deterministic result for input %q", urlStr)
		}
	})
}
