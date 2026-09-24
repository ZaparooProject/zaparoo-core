//go:build linux && !android

package arcadedb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	config2 "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitHubContentsResponse_Success(t *testing.T) {
	t.Parallel()

	body := `[{"name":"ArcadeDatabase.csv","path":"ArcadeDatabase.csv","sha":"abc123","type":"file"}]`

	contents, err := parseGitHubContentsResponse(http.StatusOK, []byte(body))

	require.NoError(t, err)
	require.Len(t, contents, 1)
	assert.Equal(t, "ArcadeDatabase.csv", contents[0].Name)
	assert.Equal(t, "abc123", contents[0].Sha)
}

func TestParseGitHubContentsResponse_EmptyArray(t *testing.T) {
	t.Parallel()

	contents, err := parseGitHubContentsResponse(http.StatusOK, []byte("[]"))

	require.NoError(t, err)
	assert.Empty(t, contents)
}

func TestParseGitHubContentsResponse_Forbidden(t *testing.T) {
	t.Parallel()

	body := `{"message":"API rate limit exceeded"}`

	contents, err := parseGitHubContentsResponse(http.StatusForbidden, []byte(body))

	assert.Nil(t, contents)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden, probably rate limited")
	assert.NotContains(t, err.Error(), "API rate limit exceeded")
}

func TestParseGitHubContentsResponse_NotFound(t *testing.T) {
	t.Parallel()

	body := `{"message":"Not Found"}`

	contents, err := parseGitHubContentsResponse(http.StatusNotFound, []byte(body))

	assert.Nil(t, contents)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NotContains(t, err.Error(), "Not Found")
}

func TestParseGitHubContentsResponse_ServerError(t *testing.T) {
	t.Parallel()

	body := `{"message":"Internal Server Error"}`

	contents, err := parseGitHubContentsResponse(http.StatusInternalServerError, []byte(body))

	assert.Nil(t, contents)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.NotContains(t, err.Error(), "Internal Server Error")
}

func TestParseGitHubContentsResponse_OmitsLongBody(t *testing.T) {
	t.Parallel()

	longBody := strings.Repeat("private response content", 20)

	_, err := parseGitHubContentsResponse(http.StatusBadRequest, []byte(longBody))

	require.Error(t, err)
	assert.EqualError(t, err, "GitHub API returned 400")
}

func TestParseGitHubContentsResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	body := `not valid json`

	contents, err := parseGitHubContentsResponse(http.StatusOK, []byte(body))

	assert.Nil(t, contents)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestClient_GetGitBlobSha1(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	client := NewClient(nil, fs, "", "")

	// Create a test file with known content
	// Git blob SHA1 = SHA1("blob <size>\0<content>")
	content := []byte("hello world\n")
	err := afero.WriteFile(fs, "/test/file.txt", content, 0o644)
	require.NoError(t, err)

	sha, err := client.getGitBlobSha1("/test/file.txt")

	require.NoError(t, err)
	// Known git blob SHA1 for "hello world\n"
	assert.Equal(t, "3b18e512dba79e4c8300dd08aeb37f8e728b8dad", sha)
}

func TestClient_GetGitBlobSha1_FileNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	client := NewClient(nil, fs, "", "")

	_, err := client.getGitBlobSha1("/nonexistent/file.txt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestClient_Read_Success(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	client := NewClient(nil, fs, "", "")

	csvHeader := "setname,name,region,version,alternative,parent_title,platform,series," +
		"homebrew,bootleg,year,manufacturer,category,linebreak1,resolution,flip," +
		"linebreak2,players,move_inputs,special_controls,num_buttons"
	csvContent := csvHeader + "\npacman,Pac-Man,World,,,,Arcade,Pac-Man,,,1980,Namco,Maze,,224x288,,,1-2,4-way,,1\n"
	err := afero.WriteFile(fs, "/data/arcade.csv", []byte(csvContent), 0o644)
	require.NoError(t, err)

	entries, err := client.Read("/data/arcade.csv")

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "pacman", entries[0].Setname)
	assert.Equal(t, "Pac-Man", entries[0].Name)
	assert.Equal(t, "1980", entries[0].Year)
	assert.Equal(t, "Namco", entries[0].Manufacturer)
}

func TestClient_Read_FiltersInvalidDiskEntries(t *testing.T) {
	t.Parallel()

	csvHeader := "setname,name,region,version,alternative,parent_title,platform,series," +
		"homebrew,bootleg,year,manufacturer,category,linebreak1,resolution,flip," +
		"linebreak2,players,move_inputs,special_controls,num_buttons"
	csvContent := csvHeader +
		"\npacman,Pac-Man,World,,,,Arcade,Pac-Man,,," +
		"1980,Namco,Maze,,224x288,,,1-2,4-way,,1" +
		"\n,Missing Setname,World,,,,Arcade,,,," +
		"1990,,Platform,,256x224,,,1-2,4-way,,2" +
		"\nmissingname,,World,,,,Arcade,,,," +
		"1990,,Platform,,256x224,,,1-2,4-way,,2" +
		"\ngalaga,Galaga,World,,,,Arcade,Galaxian,,," +
		"1981,Namco,Shooter,,224x288,,,1-2,4-way,,1\n"

	fs := afero.NewMemMapFs()
	err := afero.WriteFile(fs, "/data/arcade.csv", []byte(csvContent), 0o644)
	require.NoError(t, err)

	client := NewClient(nil, fs, "", "")

	entries, err := client.Read("/data/arcade.csv")

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "pacman", entries[0].Setname)
	assert.Equal(t, "galaga", entries[1].Setname)
}

func TestClient_Read_EmbeddedFallback(t *testing.T) {
	// These subtests modify the package-level EmbeddedArcadeDB variable,
	// so they must not run in parallel with each other or other tests.
	csvHeader := "setname,name,region,version,alternative,parent_title,platform,series," +
		"homebrew,bootleg,year,manufacturer,category,linebreak1,resolution,flip," +
		"linebreak2,players,move_inputs,special_controls,num_buttons"

	t.Run("nil embed returns error", func(t *testing.T) {
		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = nil
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		_, err := client.Read("/nonexistent/arcade.csv")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no embedded database available")
	})

	t.Run("empty embed returns error", func(t *testing.T) {
		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte{}
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		_, err := client.Read("/nonexistent/arcade.csv")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no embedded database available")
	})

	t.Run("falls back to embedded", func(t *testing.T) {
		csvContent := csvHeader +
			"\nbublbobl,Bubble Bobble,World,,,,Arcade,,,," +
			"1986,Taito,Platform,,256x224,,,1-2,4-way,,2\n"

		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(csvContent)
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		entries, err := client.Read("/nonexistent/arcade.csv")

		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "bublbobl", entries[0].Setname)
		assert.Equal(t, "Bubble Bobble", entries[0].Name)
		assert.Equal(t, "1986", entries[0].Year)
	})

	t.Run("disk takes priority over embedded", func(t *testing.T) {
		embeddedContent := csvHeader +
			"\nbublbobl,Bubble Bobble,World,,,,Arcade,,,," +
			"1986,Taito,Platform,,256x224,,,1-2,4-way,,2\n"
		diskContent := csvHeader +
			"\npacman,Pac-Man,World,,,,Arcade,Pac-Man,,," +
			"1980,Namco,Maze,,224x288,,,1-2,4-way,,1\n"

		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(embeddedContent)
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		err := afero.WriteFile(fs, "/data/arcade.csv", []byte(diskContent), 0o644)
		require.NoError(t, err)

		client := NewClient(nil, fs, "", "")

		entries, err := client.Read("/data/arcade.csv")

		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "pacman", entries[0].Setname,
			"disk file should take priority over embedded")
	})

	t.Run("headers only returns error", func(t *testing.T) {
		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(csvHeader + "\n")
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		_, err := client.Read("/nonexistent/arcade.csv")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no usable entries")
	})

	t.Run("garbage data returns error", func(t *testing.T) {
		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte("not,a,valid,csv\x00\x01\x02")
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		_, err := client.Read("/nonexistent/arcade.csv")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no usable entries")
	})

	t.Run("entries missing required fields returns error", func(t *testing.T) {
		csvContent := csvHeader +
			"\n,,World,,,,Arcade,,,," +
			"1986,Taito,Platform,,256x224,,,1-2,4-way,,2\n"

		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(csvContent)
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		_, err := client.Read("/nonexistent/arcade.csv")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no usable entries")
	})

	t.Run("filters invalid entries from valid ones", func(t *testing.T) {
		csvContent := csvHeader +
			"\nbublbobl,Bubble Bobble,World,,,,Arcade,,,," +
			"1986,Taito,Platform,,256x224,,,1-2,4-way,,2" +
			"\n,,World,,,,Arcade,,,," +
			"1990,,Platform,,256x224,,,1-2,4-way,,2" +
			"\npacman,Pac-Man,World,,,,Arcade,Pac-Man,,," +
			"1980,Namco,Maze,,224x288,,,1-2,4-way,,1\n"

		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(csvContent)
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		entries, err := client.Read("/nonexistent/arcade.csv")

		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, "bublbobl", entries[0].Setname)
		assert.Equal(t, "pacman", entries[1].Setname)
	})

	t.Run("embedded multiple entries", func(t *testing.T) {
		csvContent := csvHeader +
			"\nbublbobl,Bubble Bobble,World,,,,Arcade,,,," +
			"1986,Taito,Platform,,256x224,,,1-2,4-way,,2" +
			"\npacman,Pac-Man,World,,,,Arcade,Pac-Man,,," +
			"1980,Namco,Maze,,224x288,,,1-2,4-way,,1" +
			"\ngalaga,Galaga,World,,,,Arcade,Galaxian,,," +
			"1981,Namco,Shooter,,224x288,,,1-2,4-way,,1\n"

		original := EmbeddedArcadeDB
		EmbeddedArcadeDB = []byte(csvContent)
		t.Cleanup(func() { EmbeddedArcadeDB = original })

		fs := afero.NewMemMapFs()
		client := NewClient(nil, fs, "", "")

		entries, err := client.Read("/nonexistent/arcade.csv")

		require.NoError(t, err)
		require.Len(t, entries, 3)
		assert.Equal(t, "bublbobl", entries[0].Setname)
		assert.Equal(t, "pacman", entries[1].Setname)
		assert.Equal(t, "galaga", entries[2].Setname)
	})
}

func TestClient_Read_InvalidCSV(t *testing.T) {
	original := EmbeddedArcadeDB
	EmbeddedArcadeDB = nil
	t.Cleanup(func() { EmbeddedArcadeDB = original })

	fs := afero.NewMemMapFs()
	client := NewClient(nil, fs, "", "")

	err := afero.WriteFile(fs, "/data/arcade.csv", []byte("invalid,csv\n1,2\n"), 0o644)
	require.NoError(t, err)

	entries, err := client.Read("/data/arcade.csv")

	// Syntactically valid input without usable entries is not a catalog.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no usable entries")
	assert.Empty(t, entries)
}

func TestClient_Update_Success(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	csvContent := "setname,name\npacman,Pac-Man\n"

	// Create test server that serves both API and download endpoints
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contents":
			contents := []GithubContentsItem{{
				Name:        "ArcadeDatabase.csv",
				Sha:         "newsha123",
				Type:        "file",
				DownloadURL: "http://" + r.Host + "/download",
			}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contents)
		case "/download":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(csvContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), fs, server.URL+"/contents", "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	require.NoError(t, err)
	assert.True(t, updated)

	// Verify file was written
	content, err := afero.ReadFile(fs, "/data/assets/ArcadeDatabase.csv")
	require.NoError(t, err)
	assert.Equal(t, csvContent, string(content))
}

func TestClient_Update_NoUpdateNeeded(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	// Pre-create file with known content
	csvContent := "setname,name\npacman,Pac-Man\n"
	err := afero.WriteFile(fs, "/data/assets/ArcadeDatabase.csv", []byte(csvContent), 0o644)
	require.NoError(t, err)

	// Get the git blob SHA1 of the existing file
	client := NewClient(nil, fs, "", "ArcadeDatabase.csv")
	existingSha, err := client.getGitBlobSha1("/data/assets/ArcadeDatabase.csv")
	require.NoError(t, err)

	// Create test server that returns same SHA
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contents := []GithubContentsItem{{
			Name: "ArcadeDatabase.csv",
			Sha:  existingSha, // Same SHA = no update needed
			Type: "file",
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contents)
	}))
	defer server.Close()

	client = NewClient(server.Client(), fs, server.URL, "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	require.NoError(t, err)
	assert.False(t, updated)
}

func TestClient_Update_APIError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), fs, server.URL, "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	assert.False(t, updated)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "rate limit")
}

func TestClient_Update_FileNotInRepo(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contents := []GithubContentsItem{{
			Name: "OtherFile.csv",
			Sha:  "abc123",
			Type: "file",
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contents)
	}))
	defer server.Close()

	client := NewClient(server.Client(), fs, server.URL, "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	assert.False(t, updated)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in repository")
}

func TestClient_Update_DownloadFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contents":
			contents := []GithubContentsItem{{
				Name:        "ArcadeDatabase.csv",
				Sha:         "newsha123",
				Type:        "file",
				DownloadURL: "http://" + r.Host + "/download",
			}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contents)
		case "/download":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), fs, server.URL+"/contents", "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	assert.False(t, updated)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

func TestClient_Update_EmptyContents(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), fs, server.URL, "ArcadeDatabase.csv")

	updated, err := client.Update("/data/assets/ArcadeDatabase.csv")

	require.NoError(t, err)
	assert.False(t, updated)
}

// TestClient_Read_ParsesRotation pins the upstream column order. The catalog
// states the cabinet monitor rotation between resolution and flip; a struct
// missing that field parses the file without error and silently drops it,
// which is how it went unread for as long as it did.
func TestClient_Read_ParsesRotation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	client := NewClient(nil, fs, "", "")

	csvHeader := "setname,name,region,version,alternative,parent_title,platform,series," +
		"homebrew,bootleg,year,manufacturer,category,linebreak1,resolution,rotation,flip," +
		"linebreak2,players,move_inputs,special_controls,num_buttons"
	csvContent := csvHeader +
		"\n1941,1941- Counter Attack (W),World,900227,yes,1941- Counter Attack,Capcom CPS-1,19XX," +
		"no,no,1990,Capcom,Shooter - Flying Vertical,,15kHz,vertical (ccw),yes,,2 (simultaneous),8-way,,2\n"
	require.NoError(t, afero.WriteFile(fs, "/data/arcade.csv", []byte(csvContent), 0o644))

	entries, err := client.Read("/data/arcade.csv")

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "15kHz", entries[0].Resolution)
	assert.Equal(t, "vertical (ccw)", entries[0].Rotation)
	assert.Equal(t, "yes", entries[0].Flip)
	assert.Equal(t, "2 (simultaneous)", entries[0].Players)
	assert.Equal(t, "2", entries[0].NumButtons)
}

func TestDefaultClient_SendsUserAgent(t *testing.T) {
	t.Parallel()

	userAgent := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent <- r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := defaultClient()
	assert.Equal(t, config2.ArcadeDbURL, c.apiURL)
	assert.Equal(t, config2.ArcadeDbFile, c.filename)

	status, _, err := c.doRequest(t.Context(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, useragent.String(), <-userAgent)
}
