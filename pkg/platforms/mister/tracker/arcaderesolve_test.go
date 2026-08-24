//go:build linux

package tracker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func writeTestMRA(t *testing.T, path, setName, name string) {
	t.Helper()
	data := "<misterromdescription><setname>" + setName + "</setname><name>" + name + "</name></misterromdescription>"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
}

func TestLookupArcadeSetPathRetriesFailure(t *testing.T) {
	t.Parallel()

	mraPath := filepath.Join(t.TempDir(), "Pooyan.mra")
	writeTestMRA(t, mraPath, "pooyan", "Pooyan")

	mediaDB := testinghelpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
		Return([]database.SearchResultWithCursor{}, nil).Once()
	mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
		Return([]database.SearchResultWithCursor{{Path: mraPath}}, nil).Once()

	tr := &Tracker{db: &database.Database{MediaDB: mediaDB}}
	_, ok := tr.lookupArcadeSetPath("pooyan", "Pooyan")
	require.False(t, ok)

	path, ok := tr.lookupArcadeSetPath("pooyan", "Pooyan")
	require.True(t, ok)
	assert.Equal(t, mraPath, path)
	mediaDB.AssertExpectations(t)
}

func TestResolveArcadeSetName(t *testing.T) {
	t.Parallel()

	t.Run("unique confirmed match", func(t *testing.T) {
		t.Parallel()
		mraPath := filepath.Join(t.TempDir(), "Pooyan.mra")
		writeTestMRA(t, mraPath, "pooyan", "Pooyan")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: mraPath}}, nil)

		path, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		require.True(t, ok)
		assert.Equal(t, mraPath, path)
	})

	t.Run("confirmed match picked out of unrelated candidates", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		wantPath := filepath.Join(dir, "Pooyan.mra")
		otherPath := filepath.Join(dir, "Pooyan (alt).mra")
		writeTestMRA(t, wantPath, "pooyan", "Pooyan")
		writeTestMRA(t, otherPath, "pooyana", "Pooyan (alt)")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: wantPath}, {Path: otherPath}}, nil)

		path, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		require.True(t, ok)
		assert.Equal(t, wantPath, path)
	})

	t.Run("ambiguous confirmed matches are refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		altPath := filepath.Join(dir, "clone1.mra")
		otherPath := filepath.Join(dir, "clone2.mra")
		writeTestMRA(t, altPath, "pooyan", "Pooyan (clone 1)")
		writeTestMRA(t, otherPath, "pooyan", "Pooyan (clone 2)")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: altPath}, {Path: otherPath}}, nil)

		_, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		assert.False(t, ok)
	})

	t.Run("no candidate confirms the set name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		altPath := filepath.Join(dir, "Pooyan (alt).mra")
		otherPath := filepath.Join(dir, "Pooyan (bootleg).mra")
		writeTestMRA(t, altPath, "pooyana", "Pooyan (alt)")
		writeTestMRA(t, otherPath, "pooyanb", "Pooyan (bootleg)")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: altPath}, {Path: otherPath}}, nil)

		_, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		assert.False(t, ok)
	})

	t.Run("no candidates", func(t *testing.T) {
		t.Parallel()
		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{}, nil)

		_, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		assert.False(t, ok)
	})

	t.Run("duplicate paths from slug variants are deduplicated", func(t *testing.T) {
		t.Parallel()
		mraPath := filepath.Join(t.TempDir(), "Pooyan.mra")
		writeTestMRA(t, mraPath, "pooyan", "Pooyan")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: mraPath}, {Path: mraPath}}, nil)

		path, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		require.True(t, ok)
		assert.Equal(t, mraPath, path)
	})

	t.Run("single unreadable candidate is still accepted", func(t *testing.T) {
		t.Parallel()
		missingPath := filepath.Join(t.TempDir(), "Missing.mra")

		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: missingPath}}, nil)

		path, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		require.True(t, ok)
		assert.Equal(t, missingPath, path)
	})

	t.Run("nil mediaDB refuses", func(t *testing.T) {
		t.Parallel()
		_, ok := ResolveArcadeSetName(context.Background(), nil, "pooyan", "Pooyan")
		assert.False(t, ok)
	})

	t.Run("empty setName or arcadeName refuses", func(t *testing.T) {
		t.Parallel()
		mediaDB := testinghelpers.NewMockMediaDBI()
		_, ok := ResolveArcadeSetName(context.Background(), mediaDB, "", "Pooyan")
		assert.False(t, ok)
		_, ok = ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "")
		assert.False(t, ok)
	})

	t.Run("search error refuses", func(t *testing.T) {
		t.Parallel()
		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor(nil), assert.AnError)

		_, ok := ResolveArcadeSetName(context.Background(), mediaDB, "pooyan", "Pooyan")
		assert.False(t, ok)
	})
}
