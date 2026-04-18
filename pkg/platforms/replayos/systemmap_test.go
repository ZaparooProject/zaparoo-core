//go:build linux

package replayos

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
)

func TestSystemInfo_GetLauncherID(t *testing.T) {
	t.Parallel()

	t.Run("returns LauncherID when set", func(t *testing.T) {
		t.Parallel()
		info := SystemMap["arcade_fbneo"]
		assert.Equal(t, "ArcadeFBNeo", info.GetLauncherID())
	})

	t.Run("falls back to SystemID when LauncherID empty", func(t *testing.T) {
		t.Parallel()
		info := SystemMap["nintendo_snes"]
		assert.Equal(t, systemdefs.SystemSNES, info.GetLauncherID())
		assert.Empty(t, info.LauncherID)
	})
}
