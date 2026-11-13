//go:build linux && !android

package esbase

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/stretchr/testify/assert"
)

func TestELECSystemAliasesTargetESDEFolders(t *testing.T) {
	t.Parallel()

	for alias, folder := range ELECSystemAliases {
		_, isKey := esde.SystemMap[alias]
		assert.False(t, isKey, "%s is already an ES-DE folder", alias)
		_, ok := esde.SystemMap[folder]
		assert.True(t, ok, "%s maps to unknown ES-DE folder %s", alias, folder)
	}
}
