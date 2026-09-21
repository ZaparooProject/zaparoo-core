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

package android

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/hostmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredFailureReasons reads the FailureReason constants out of host.go, so a
// new host failure cannot be added without a mapping decision in these tests.
func declaredFailureReasons(t *testing.T) []FailureReason {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "host.go", nil, 0)
	require.NoError(t, err)
	var reasons []FailureReason
	for _, decl := range file.Decls {
		genDecl, isGen := decl.(*ast.GenDecl)
		if !isGen || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			ident, isIdent := valueSpec.Type.(*ast.Ident)
			if !isIdent || ident.Name != "FailureReason" {
				continue
			}
			for _, expr := range valueSpec.Values {
				literal, isLiteral := expr.(*ast.BasicLit)
				require.True(t, isLiteral, "FailureReason constants are string literals")
				value, unquoteErr := strconv.Unquote(literal.Value)
				require.NoError(t, unquoteErr)
				reasons = append(reasons, FailureReason(value))
			}
		}
	}
	require.NotEmpty(t, reasons)
	return reasons
}

// requireClosedReason asserts err is a repair error a client can act on: a
// reason from the closed set and nothing outside the closed parameter key set.
func requireClosedReason(t *testing.T, err error) *platforms.LaunchRepairError {
	t.Helper()

	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	assert.NotEmpty(t, string(repair.Reason()))
	assert.True(t, repair.Reason().Valid(), "reason %q is outside the closed set", repair.Reason())
	assert.NotEmpty(t, repair.Error())
	for key, value := range repair.Params() {
		assert.Contains(t, platforms.LaunchRepairParams(), key)
		assert.NotEmpty(t, value)
	}
	return repair
}

func TestRepairReasonMapsEveryHostFailure(t *testing.T) {
	t.Parallel()

	want := map[FailureReason]platforms.LaunchRepairReason{
		FailureHostUnavailable:     platforms.LaunchRepairHostUnavailable,
		FailureInvalidResponse:     platforms.LaunchRepairHostUnavailable,
		FailureNotInstalled:        platforms.LaunchRepairLauncherNotInstalled,
		FailureActivityUnavailable: platforms.LaunchRepairLauncherComponentMissing,
		FailureStorageDenied:       platforms.LaunchRepairStoragePermissionRequired,
		FailureStorageVersion:      platforms.LaunchRepairRefused,
		FailureProviderUnsupported: platforms.LaunchRepairStorageProviderUnsupported,
		FailureStorageUnmounted:    platforms.LaunchRepairStorageUnavailable,
		FailureSourceUnavailable:   platforms.LaunchRepairMediaUnavailable,
		FailureForegroundRequired:  platforms.LaunchRepairHostForegroundRequired,
		FailureCancelled:           platforms.LaunchRepairHostForegroundRequired,
		FailureOutcomeUnknown:      platforms.LaunchRepairOutcomeUnknown,
		FailureRefused:             platforms.LaunchRepairRefused,
	}

	declared := declaredFailureReasons(t)
	assert.Len(t, want, len(declared), "every declared host failure needs one mapping")
	for _, failure := range declared {
		mapped, listed := want[failure]
		require.True(t, listed, "host failure %s has no reason mapping", failure)
		assert.Equal(t, mapped, repairReason(failure), failure)
		assert.True(t, repairReason(failure).Valid(), failure)
	}

	// A host that answers with something this build does not know still gets a
	// usable reason.
	assert.Equal(t, platforms.LaunchRepairRefused, repairReason(""))
	assert.Equal(t, platforms.LaunchRepairRefused, repairReason("invented-by-a-host"))
}

func TestRepairParamsAreDisplayNames(t *testing.T) {
	t.Parallel()

	platform := startedPlatform(t.Context(), t, &fakeHost{})

	assert.Equal(t, map[string]string{"launcher": "RetroArch", "plugin": "Mesen"},
		platform.entryByID["RetroArch.Mesen"].repairParams())
	assert.Equal(t, map[string]string{"launcher": "DuckStation"},
		platform.entryByID["DuckStation.PSX"].repairParams(),
		"a standalone launcher has no plugin to name")

	// Every catalog entry's names must survive the repair error's filter, or a
	// client would be left without the name its wording needs.
	for i := range platform.entries {
		entry := &platform.entries[i]
		params := entry.repairParams()
		repair := requireClosedReason(t,
			repairError(platforms.LaunchRepairRefused, params, "refused"))
		assert.Equal(t, params, repair.Params(), entry.definition.ID)
	}
}

// TestEveryRepairErrorCarriesAClosedReason drives every path in this package
// that can produce a repair error and requires each one to name a reason a
// client can branch on.
//
//nolint:gocognit // one sweep over every producer reads better than several
func TestEveryRepairErrorCarriesAClosedReason(t *testing.T) {
	t.Parallel()

	game := []string{"nes", "Game.nes"}
	produced := make(map[platforms.LaunchRepairReason]struct{})
	record := func(t *testing.T, err error) {
		t.Helper()
		produced[requireClosedReason(t, err).Reason()] = struct{}{}
	}

	t.Run("launcher availability", func(t *testing.T) {
		for _, failure := range declaredFailureReasons(t) {
			host := &fakeHost{failures: map[string]FailureReason{retroArchPackage: failure}}
			launchers := startedPlatform(t.Context(), t, host).Launchers(nil)
			mesen := launcherByID(t, launchers, "RetroArch.Mesen")
			require.False(t, mesen.Available, failure)
			record(t, mesen.Availability(nil))
		}
	})

	t.Run("preflight", func(t *testing.T) {
		host := &fakeHost{scanned: true, cores: []string{"mesen_libretro_android.so"}}
		launchers := startedPlatform(t.Context(), t, host).Launchers(nil)
		mesen := launcherByID(t, launchers, "RetroArch.Mesen")
		record(t, launcherByID(t, launchers, "RetroArch.FCEUmm").Preflight(nil, identity(t, game...), nil))
		record(t, mesen.Preflight(nil, identity(t, "snes", "Game.sfc"), nil))
		record(t, mesen.Preflight(nil, identity(t, game...), &platforms.LaunchOptions{SetName: "other"}))
	})

	t.Run("dispatch", func(t *testing.T) {
		hosts := make([]*fakeHost, 0, len(declaredFailureReasons(t))+4)
		for _, failure := range declaredFailureReasons(t) {
			hosts = append(hosts, &fakeHost{dispatchErr: &HostError{Reason: failure}})
		}
		hosts = append(hosts,
			&fakeHost{dispatchErr: errors.New("binder died")},
			&fakeHost{receipt: &DispatchReceipt{Package: "org.example.other"}},
			&fakeHost{openErr: hostmedia.ErrUnavailable},
		)
		for _, host := range hosts {
			platform := startedPlatform(t.Context(), t, host)
			record(t, platform.LaunchMedia(&config.Instance{}, identity(t, game...),
				&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil))
		}

		// A media entry the host can no longer resolve.
		platform := startedPlatform(t.Context(), t, &fakeHost{})
		record(t, platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Gone.nes"),
			&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil))
	})

	// The sweep is only meaningful if it actually reached most of the set.
	for _, reason := range []platforms.LaunchRepairReason{
		platforms.LaunchRepairLauncherNotInstalled,
		platforms.LaunchRepairLauncherComponentMissing,
		platforms.LaunchRepairLauncherPluginMissing,
		platforms.LaunchRepairLauncherUnsupportedMedia,
		platforms.LaunchRepairLauncherOptionsUnsupported,
		platforms.LaunchRepairStoragePermissionRequired,
		platforms.LaunchRepairStorageProviderUnsupported,
		platforms.LaunchRepairStorageUnavailable,
		platforms.LaunchRepairMediaUnavailable,
		platforms.LaunchRepairHostUnavailable,
		platforms.LaunchRepairHostForegroundRequired,
		platforms.LaunchRepairOutcomeUnknown,
		platforms.LaunchRepairRefused,
	} {
		assert.Contains(t, produced, reason, "no producer exercised %s", reason)
	}
	// Only a client chooses between launchers, so this platform never asks.
	assert.NotContains(t, produced, platforms.LaunchRepairLauncherAmbiguous)
	for reason := range produced {
		assert.True(t, slices.Contains(platforms.LaunchRepairReasons(), reason), reason)
	}
}
