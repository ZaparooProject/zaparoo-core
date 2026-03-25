package mediadb

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagsForSystems_SingleSystem(t *testing.T) {
	t.Parallel()
	cache := &tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {
				{Type: "genre", Tag: "Action"},
				{Type: "genre", Tag: "Adventure"},
			},
		},
	}

	result := cache.tagsForSystems([]systemdefs.System{{ID: "nes"}})
	assert.Equal(t, []database.TagInfo{
		{Type: "genre", Tag: "Action"},
		{Type: "genre", Tag: "Adventure"},
	}, result)
}

func TestTagsForSystems_SingleSystemReturnsClone(t *testing.T) {
	t.Parallel()
	cache := &tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {
				{Type: "genre", Tag: "Action"},
			},
		},
	}

	result := cache.tagsForSystems([]systemdefs.System{{ID: "nes"}})
	require.Len(t, result, 1)

	// Mutating the result must not affect the cache.
	result[0] = database.TagInfo{Type: "year", Tag: "1999"}
	assert.Equal(t, database.TagInfo{Type: "genre", Tag: "Action"}, cache.bySystem["nes"][0])
}

func TestTagsForSystems_MultiSystemDedup(t *testing.T) {
	t.Parallel()
	cache := &tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {
				{Type: "genre", Tag: "Action"},
				{Type: "genre", Tag: "RPG"},
			},
			"snes": {
				{Type: "genre", Tag: "Action"},
				{Type: "genre", Tag: "Puzzle"},
			},
		},
	}

	result := cache.tagsForSystems([]systemdefs.System{{ID: "nes"}, {ID: "snes"}})

	assert.Len(t, result, 3)
	assert.Contains(t, result, database.TagInfo{Type: "genre", Tag: "Action"})
	assert.Contains(t, result, database.TagInfo{Type: "genre", Tag: "RPG"})
	assert.Contains(t, result, database.TagInfo{Type: "genre", Tag: "Puzzle"})
}

func TestTagsForSystems_UnknownSystem(t *testing.T) {
	t.Parallel()
	cache := &tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action"}},
		},
	}

	result := cache.tagsForSystems([]systemdefs.System{{ID: "unknown"}})
	assert.Empty(t, result)
}

func TestTagsForSystems_EmptySystems(t *testing.T) {
	t.Parallel()
	cache := &tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action"}},
		},
	}

	result := cache.tagsForSystems([]systemdefs.System{})
	assert.Empty(t, result)
}

func TestBuildTagCache_Success(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT s.SystemID, stc.TagType, stc.Tag`).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "TagType", "Tag"}).
			AddRow("nes", "genre", "Action").
			AddRow("nes", "genre", "RPG").
			AddRow("snes", "genre", "Action").
			AddRow("snes", "year", "1992"))

	cache, err := buildTagCache(context.Background(), db)

	require.NoError(t, err)
	assert.Len(t, cache.allTags, 3) // Action deduped
	assert.Len(t, cache.bySystem["nes"], 2)
	assert.Len(t, cache.bySystem["snes"], 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildTagCache_EmptyTable(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT s.SystemID, stc.TagType, stc.Tag`).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "TagType", "Tag"}))

	cache, err := buildTagCache(context.Background(), db)

	require.NoError(t, err)
	assert.Empty(t, cache.allTags)
	assert.Empty(t, cache.bySystem)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildTagCache_QueryError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT s.SystemID, stc.TagType, stc.Tag`).
		WillReturnError(fmt.Errorf("connection lost"))

	_, err = buildTagCache(context.Background(), db)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query system tags cache")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildTagCache_ScanError(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT s.SystemID, stc.TagType, stc.Tag`).
		WillReturnRows(sqlmock.NewRows([]string{"SystemID", "TagType"}).
			AddRow("nes", "genre")) // Missing Tag column

	_, err = buildTagCache(context.Background(), db)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan tag cache row")
	assert.NoError(t, mock.ExpectationsWereMet())
}
