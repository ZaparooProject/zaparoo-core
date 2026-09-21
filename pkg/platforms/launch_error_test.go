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

package platforms_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const genericRepairMessage = "player request could not be completed"

func repairErrorOf(t *testing.T, err error) *platforms.LaunchRepairError {
	t.Helper()
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	return repair
}

func TestLaunchRepairReasonSetIsClosed(t *testing.T) {
	t.Parallel()

	reasons := platforms.LaunchRepairReasons()
	require.NotEmpty(t, reasons)
	seen := make(map[platforms.LaunchRepairReason]struct{}, len(reasons))
	for _, reason := range reasons {
		assert.NotEmpty(t, string(reason))
		assert.True(t, reason.Valid(), reason)
		_, duplicate := seen[reason]
		assert.False(t, duplicate, reason)
		seen[reason] = struct{}{}
		assert.Regexp(t, `^[a-z]+(_[a-z]+)*$`, string(reason), "reasons are lower snake case wire values")
	}
	assert.False(t, platforms.LaunchRepairReason("").Valid())
	assert.False(t, platforms.LaunchRepairReason("not_a_reason").Valid())

	// Mutating the returned slice must not reach the set.
	reasons[0] = "tampered"
	assert.NotContains(t, platforms.LaunchRepairReasons(), platforms.LaunchRepairReason("tampered"))

	params := platforms.LaunchRepairParams()
	assert.Equal(t, []string{"launcher", "plugin"}, params)
	params[0] = "tampered"
	assert.Equal(t, []string{"launcher", "plugin"}, platforms.LaunchRepairParams())
}

// TestLaunchRepairReasonsAreDocumented fails when a reason is added to the
// closed set without documenting it for clients, or when the documentation
// names a reason the set does not define.
func TestLaunchRepairReasonsAreDocumented(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "methods.md"))
	require.NoError(t, err)
	section := string(doc)
	start := strings.Index(section, "##### Launch repair errors")
	require.GreaterOrEqual(t, start, 0, "docs/api/methods.md must document the launch_repair error")
	section = section[start:]
	if end := strings.Index(section, "\n#### "); end >= 0 {
		section = section[:end]
	}

	for _, reason := range platforms.LaunchRepairReasons() {
		assert.Contains(t, section, "`"+string(reason)+"`", "reason is not documented")
	}
	documented := regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`").FindAllStringSubmatch(section, -1)
	require.NotEmpty(t, documented)
	for _, match := range documented {
		value := platforms.LaunchRepairReason(match[1])
		if value == "launch_repair" {
			continue
		}
		assert.True(t, value.Valid(), "documentation names an undefined reason: %s", match[1])
	}
}

func TestNewLaunchRepairErrorDefaultsToRefused(t *testing.T) {
	t.Parallel()

	err := platforms.NewLaunchRepairError("the launcher service is not responding")
	repair := repairErrorOf(t, err)

	assert.Equal(t, "the launcher service is not responding", repair.Error())
	assert.Equal(t, platforms.LaunchRepairRefused, repair.Reason())
	assert.Nil(t, repair.Params())
}

func TestLaunchRepairErrorAlwaysCarriesAReasonAndAMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    platforms.LaunchRepairReason
		wantMsg string
		reason  platforms.LaunchRepairReason
	}{
		{
			name: "a known reason is kept", reason: platforms.LaunchRepairMediaUnavailable,
			message: "this media entry could not be opened",
			want:    platforms.LaunchRepairMediaUnavailable, wantMsg: "this media entry could not be opened",
		},
		{
			name: "an empty reason becomes the catch-all", reason: "",
			message: "unreachable", want: platforms.LaunchRepairRefused, wantMsg: "unreachable",
		},
		{
			name: "an unknown reason becomes the catch-all", reason: "invented_by_a_caller",
			message: "unreachable", want: platforms.LaunchRepairRefused, wantMsg: "unreachable",
		},
		{
			name:   "an empty message becomes the generic fallback",
			reason: platforms.LaunchRepairHostUnavailable, message: "",
			want: platforms.LaunchRepairHostUnavailable, wantMsg: genericRepairMessage,
		},
		{
			name:   "a multi-line message becomes the generic fallback",
			reason: platforms.LaunchRepairHostUnavailable, message: "host said:\nbinder died",
			want: platforms.LaunchRepairHostUnavailable, wantMsg: genericRepairMessage,
		},
		{
			name:   "an over-long message becomes the generic fallback",
			reason: platforms.LaunchRepairRefused, message: strings.Repeat("a", 1025),
			want: platforms.LaunchRepairRefused, wantMsg: genericRepairMessage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repair := repairErrorOf(t, platforms.NewLaunchRepairErrorWithReason(tt.reason, nil, tt.message))
			assert.Equal(t, tt.want, repair.Reason())
			assert.Equal(t, tt.wantMsg, repair.Error())
			assert.NotEmpty(t, string(repair.Reason()))
		})
	}
}

// A zero value can only come from a caller building the struct by hand, and it
// must still answer with the catch-all rather than nothing.
func TestLaunchRepairErrorZeroValueReportsTheCatchAll(t *testing.T) {
	t.Parallel()

	zero := &platforms.LaunchRepairError{}
	assert.Equal(t, platforms.LaunchRepairRefused, zero.Reason())
	assert.Equal(t, genericRepairMessage, zero.Error())
	assert.Nil(t, zero.Params())

	var missing *platforms.LaunchRepairError
	assert.Equal(t, platforms.LaunchRepairRefused, missing.Reason())
	assert.Equal(t, genericRepairMessage, missing.Error())
	assert.Nil(t, missing.Params())
}

func TestLaunchRepairParamsAreBoundedDisplayNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		params map[string]string
		want   map[string]string
		name   string
	}{
		{
			name:   "both closed keys are kept",
			params: map[string]string{"launcher": "RetroArch", "plugin": "Mesen"},
			want:   map[string]string{"launcher": "RetroArch", "plugin": "Mesen"},
		},
		{
			name:   "names with reviewed punctuation are kept",
			params: map[string]string{"launcher": "RetroArch", "plugin": "MAME 2003 (0.78)"},
			want:   map[string]string{"launcher": "RetroArch", "plugin": "MAME 2003 (0.78)"},
		},
		{
			name:   "an unknown key is dropped",
			params: map[string]string{"launcher": "RetroArch", "package": "com.retroarch.aarch64"},
			want:   map[string]string{"launcher": "RetroArch"},
		},
		{
			name:   "a filesystem path is dropped",
			params: map[string]string{"launcher": "/storage/emulated/0/roms/Game.nes"},
			want:   nil,
		},
		{
			name:   "a content URI is dropped",
			params: map[string]string{"plugin": "content://org.example.documents/tree/games"},
			want:   nil,
		},
		{
			name:   "a script fragment is dropped",
			params: map[string]string{"launcher": "sh -c 'rm -rf $HOME'"},
			want:   nil,
		},
		{
			name:   "a credential is dropped",
			params: map[string]string{"plugin": "user:hunter2@example.com"},
			want:   nil,
		},
		{
			name:   "provider error text is dropped",
			params: map[string]string{"plugin": "java.lang.SecurityException: Permission Denial: uid=10123"},
			want:   nil,
		},
		{
			name:   "an over-long name is dropped",
			params: map[string]string{"launcher": strings.Repeat("A", 65)},
			want:   nil,
		},
		{
			name:   "a name at the cap is kept",
			params: map[string]string{"launcher": strings.Repeat("A", 64)},
			want:   map[string]string{"launcher": strings.Repeat("A", 64)},
		},
		{
			name:   "an empty name is dropped",
			params: map[string]string{"launcher": "", "plugin": "Mesen"},
			want:   map[string]string{"plugin": "Mesen"},
		},
		{
			name:   "a padded name is dropped",
			params: map[string]string{"launcher": " RetroArch "},
			want:   nil,
		},
		{
			name:   "a control character is dropped",
			params: map[string]string{"launcher": "Retro\x00Arch"},
			want:   nil,
		},
		{
			name:   "a non-ASCII name is dropped",
			params: map[string]string{"launcher": "Émulateur"},
			want:   nil,
		},
		{name: "no parameters stay absent", params: nil, want: nil},
		{name: "an empty map stays absent", params: map[string]string{}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repair := repairErrorOf(t,
				platforms.NewLaunchRepairErrorWithReason(platforms.LaunchRepairRefused, tt.params, "refused"))
			assert.Equal(t, tt.want, repair.Params())
		})
	}
}

func TestLaunchRepairParamsCannotBeEditedThroughTheError(t *testing.T) {
	t.Parallel()

	source := map[string]string{"launcher": "RetroArch"}
	repair := repairErrorOf(t,
		platforms.NewLaunchRepairErrorWithReason(platforms.LaunchRepairRefused, source, "refused"))

	source["launcher"] = "tampered"
	returned := repair.Params()
	returned["launcher"] = "tampered too"

	assert.Equal(t, map[string]string{"launcher": "RetroArch"}, repair.Params())
}

func TestLaunchRepairErrorSurvivesWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("binder transaction failed for /storage/emulated/0/roms/Game.nes")
	err := fmt.Errorf("%w: %w",
		platforms.NewLaunchRepairErrorWithReason(platforms.LaunchRepairHostUnavailable,
			map[string]string{"launcher": "RetroArch"}, "the launcher service is not responding"),
		cause)

	repair := repairErrorOf(t, err)
	assert.Equal(t, platforms.LaunchRepairHostUnavailable, repair.Reason())
	assert.Equal(t, "the launcher service is not responding", repair.Error())
	assert.Equal(t, map[string]string{"launcher": "RetroArch"}, repair.Params())
	require.ErrorIs(t, err, cause, "the cause stays reachable for logging")
	assert.NotContains(t, repair.Error(), "/storage")
}
