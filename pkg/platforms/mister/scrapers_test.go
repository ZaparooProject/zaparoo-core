//go:build linux

package mister

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapers_RegisterMiSTerScrapers(t *testing.T) {
	t.Parallel()

	scrapers := (&Platform{}).Scrapers(nil)

	require.Contains(t, scrapers, "gamelist.xml")
	require.Contains(t, scrapers, "media-folder")
	require.Contains(t, scrapers, "mister-docs")
	assert.NotNil(t, scrapers["gamelist.xml"].Scrape)
	assert.NotNil(t, scrapers["media-folder"].Scrape)
	assert.NotNil(t, scrapers["mister-docs"].Scrape)
}
