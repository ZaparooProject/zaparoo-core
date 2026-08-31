//go:build linux

package batocera

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapers_RegisterGamelistAndMediaFolder(t *testing.T) {
	t.Parallel()

	scrapers := (&Platform{}).Scrapers(nil)

	require.Contains(t, scrapers, "gamelist.xml")
	require.Contains(t, scrapers, "media-folder")
	assert.NotNil(t, scrapers["gamelist.xml"].Scrape)
	assert.NotNil(t, scrapers["media-folder"].Scrape)
}
