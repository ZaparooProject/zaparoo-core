//go:build linux

package mister

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArcadeSetSystemsMapsCuratedPlatforms(t *testing.T) {
	t.Parallel()

	setSystems := arcadeSetSystems([]arcadedb.ArcadeDbEntry{
		{Setname: "CPS1GAME", Platform: "Capcom CPS-1"},
		{Setname: "cps15game", Platform: "Capcom CPS-1.5"},
		{Setname: "pgmgame", Platform: "IGS PGM"},
		{Setname: "unknown", Platform: "Unique hardware"},
	})

	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps1game"])
	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps15game"])
	assert.Equal(t, systemdefs.SystemPGM, setSystems["pgmgame"])
	assert.NotContains(t, setSystems, "unknown")
}

func TestArcadeSystemLaunchersPreserveArcadeAndAddGranularSystems(t *testing.T) {
	t.Parallel()

	launchers := addArcadeSystemLaunchers(NewPlatform(), CreateLaunchers(NewPlatform()))
	byID := make(map[string]platforms.Launcher, len(launchers))
	for i := range launchers {
		byID[launchers[i].ID] = launchers[i]
	}

	arcade, ok := byID[systemdefs.SystemArcade]
	require.True(t, ok)
	assert.False(t, arcade.SkipFilesystemScan)
	assert.NotNil(t, arcade.Scanner)

	for _, spec := range misterArcadeSystemSpecs {
		launcher, found := byID[spec.systemID]
		require.True(t, found, spec.systemID)
		assert.True(t, launcher.SkipFilesystemScan, spec.systemID)
		assert.Equal(t, []string{"_Arcade"}, launcher.Folders, spec.systemID)
		assert.NotNil(t, launcher.Scanner, spec.systemID)
		assert.NotNil(t, launcher.Launch, spec.systemID)
	}
}
