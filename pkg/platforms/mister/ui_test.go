//go:build linux

package mister

import (
	"path/filepath"
	"testing"

	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoticeArgsLifecycleUsesInjectedFilesystem(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	argsPath := filepath.Join("tmp", "zaparoo", "notice.json")
	require.NoError(t, fs.MkdirAll(filepath.Dir(argsPath), 0o755))

	err := writeNoticeArgs(fs, argsPath, widgetmodels.NoticeArgs{Text: "Loading"})
	require.NoError(t, err)
	exists, err := afero.Exists(fs, argsPath)
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, hideNotice(fs, argsPath))
	exists, err = afero.Exists(fs, argsPath)
	require.NoError(t, err)
	assert.False(t, exists)
	exists, err = afero.Exists(fs, argsPath+".complete")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestNoticeArgsLifecycleReportsFilesystemErrors(t *testing.T) {
	t.Parallel()

	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	argsPath := filepath.Join("tmp", "zaparoo", "notice.json")
	err := writeNoticeArgs(fs, argsPath, widgetmodels.NoticeArgs{Text: "Loading"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error writing notice args")

	err = hideNotice(fs, argsPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error removing notice args")
}
