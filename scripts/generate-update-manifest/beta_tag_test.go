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

package main

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCandidateTag_OTAPrerelease(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{
		"v2.17.0-beta.0",
		"v2.17.0-beta.9",
		"v2.17.0-beta.10",
		"v2.17.0-rc.0",
		"v2.17.0-rc.1",
		"v2.17.0-rc.10",
	} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateCandidateTag(tag, channelBeta))
		})
	}
}

func TestValidateCandidateTag_RejectsUnsupportedOTAPrerelease(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{
		"v2.17.0-beta10",
		"v2.17.0-beta.01",
		"v2.17.0-rc1",
		"v2.17.0-rc.01",
		"v2.17.0-alpha.1",
		"2.17.0-beta.10",
		"v2.17-beta.10",
	} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			err := validateCandidateTag(tag, channelBeta)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "use vX.Y.Z-beta.N or vX.Y.Z-rc.N")
		})
	}
}

func TestValidateCandidateTag_NonBetaChannelUnchanged(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateCandidateTag("v2.17.0", channelStable))
	require.NoError(t, validateCandidateTag("v2.17.0-alpha.1", channelStable))
}

func TestApplyPromote_RejectsUnsupportedOTAPrereleaseBeforeArchives(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"v2.17.0-beta10", "v2.17.0-alpha.1"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			_, err := applyPromote(afero.NewMemMapFs(), &manifest{}, &options{
				tag:         tag,
				channel:     channelBeta,
				archivesDir: "missing",
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "use vX.Y.Z-beta.N or vX.Y.Z-rc.N")
		})
	}
}

func TestCanonicalPrereleaseVersionOrdering(t *testing.T) {
	t.Parallel()

	ordered := []string{
		"v2.17.0-alpha.1",
		"v2.17.0-beta.9",
		"v2.17.0-beta.10",
		"v2.17.0-rc.1",
		"v2.17.0",
	}
	for i := 1; i < len(ordered); i++ {
		assert.True(t, newerRelease(
			&otameta.Release{TagName: ordered[i]},
			&otameta.Release{TagName: ordered[i-1]},
		), "%s should sort above %s", ordered[i], ordered[i-1])
	}
}

func TestApplyPromote_IgnoresMalformedHistoricalBetaTag(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFullMatrix(t, fs, testArchivesDir, "v2.17.0-beta.10")
	m := &manifest{Releases: []*release{{
		TagName: "v2.16.0-beta10",
		Channel: channelBeta,
	}}}

	_, err := applyPromote(fs, m, &options{
		tag:         "v2.17.0-beta.10",
		channel:     channelBeta,
		archivesDir: testArchivesDir,
		rollout:     fullRollout,
	})
	require.NoError(t, err)
	assert.NotNil(t, findRelease(m, "v2.16.0-beta10"))
	assert.NotNil(t, findRelease(m, "v2.17.0-beta.10"))
}
