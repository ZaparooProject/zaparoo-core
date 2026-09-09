//go:build linux

package arcadedb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMalformedCacheUsesEmbedded(t *testing.T) {
	original := EmbeddedArcadeDB
	originalLogger := log.Logger
	var output bytes.Buffer
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() {
		EmbeddedArcadeDB = original
		log.Logger = originalLogger
	})
	for _, content := range []string{"setname,name\nbad,too,many\n", "setname,name\n", "unknown,columns\na,b\n"} {
		t.Run(content, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := filepath.Join("data", "arcade.csv")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o600))
			client := NewClient(nil, fs, "", "")
			EmbeddedArcadeDB = []byte("setname,name\nfallback,Fallback\n")
			output.Reset()
			entries, err := client.Read(path)
			require.NoError(t, err)
			assert.Contains(t, output.String(), `"level":"error"`)
			assert.Contains(t, output.String(), "invalid cached arcade database")
			require.Len(t, entries, 1)
			assert.Equal(t, "fallback", entries[0].Setname)
			cached, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			assert.Equal(t, content, string(cached), "fallback must not modify cached file")
			EmbeddedArcadeDB = nil
			_, err = client.Read(path)
			require.Error(t, err, "unusable cache and fallback must still fail")
			EmbeddedArcadeDB = []byte("setname,name\nbroken,too,many\n")
			_, err = client.Read(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to unmarshal embedded arcadedb CSV")
		})
	}
}

func TestUpdateRejectsInvalidCatalog(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"setname,name\nbad,too,many\n", "setname,name\n", "unknown,columns\na,b\n"} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			path := filepath.Join("data", "arcade.csv")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
			good := []byte("setname,name\ngood,Good\n")
			require.NoError(t, afero.WriteFile(fs, path, good, 0o600))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/download" {
					_, _ = w.Write([]byte(content))
					return
				}
				_ = json.NewEncoder(w).Encode([]GithubContentsItem{{
					Name: "arcade.csv", Type: "file", Sha: "changed",
					DownloadURL: "http://" + r.Host + "/download",
				}})
			}))
			defer server.Close()
			updated, err := NewClient(server.Client(), fs, server.URL, "arcade.csv").Update(path)
			require.Error(t, err)
			assert.False(t, updated)
			cached, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			assert.Equal(t, good, cached)
		})
	}
}
