// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package jellyfin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Library IDs used across the launcher tests.
var (
	movieID   = id(100)
	showID    = id(200)
	ep1ID     = id(201)
	ep2ID     = id(202)
	ep3ID     = id(203)
	artistID  = id(300)
	albumAID  = id(310)
	albumBID  = id(320)
	trackA1ID = id(311)
	trackA2ID = id(312)
	trackA3ID = id(313)
	trackB1ID = id(321)
)

// seedLibrary fills a fake server with a movie, a show whose episodes come
// back out of order, and an artist with two albums whose tracks also do.
func seedLibrary(f *fakeJellyfin) {
	f.addItems(
		fakeItem{Item: Item{
			ID: movieID, Name: "Heat", Type: typeMovie, ProductionYear: 1995,
			ProviderIDs: map[string]string{"Tmdb": "949", "Imdb": "tt0113277"},
		}},
		fakeItem{Item: Item{
			ID: showID, Name: "Bluey", Type: typeSeries,
			ProviderIDs: map[string]string{"Tvdb": "353546"},
		}},
		fakeItem{seriesID: showID, Item: Item{
			ID: ep3ID, Name: "Hammerbarn", Type: typeEpisode,
			SeriesName: "Bluey", ParentIndexNumber: intPtr(2), IndexNumber: intPtr(1),
		}},
		fakeItem{seriesID: showID, Item: Item{
			ID: ep2ID, Name: "Hospital", Type: typeEpisode,
			SeriesName: "Bluey", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2),
			ProviderIDs: map[string]string{"Tvdb": "6772970"},
		}},
		fakeItem{seriesID: showID, Item: Item{
			ID: ep1ID, Name: "Magic Xylophone", Type: typeEpisode,
			SeriesName: "Bluey", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1),
		}},
		fakeItem{Item: Item{
			ID: albumAID, Name: "Abbey Road", Type: typeAlbum, AlbumArtist: "The Beatles",
			ProductionYear: 1969, ProviderIDs: map[string]string{"MusicBrainzAlbum": "mb-abbey"},
		}},
		fakeItem{Item: Item{
			ID: albumBID, Name: "Revolver", Type: typeAlbum, AlbumArtist: "The Beatles",
			ProductionYear: 1966,
		}},
		fakeItem{artistID: artistID, Item: Item{
			ID: trackB1ID, Name: "Taxman", Type: typeAudio,
			Album: "Revolver", AlbumID: albumBID, AlbumArtist: "The Beatles", Artists: []string{"The Beatles"},
			ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1),
		}},
		fakeItem{artistID: artistID, Item: Item{
			ID: trackA3ID, Name: "Here Comes the Sun", Type: typeAudio,
			Album: "Abbey Road", AlbumID: albumAID, AlbumArtist: "The Beatles", Artists: []string{"The Beatles"},
			ParentIndexNumber: intPtr(2), IndexNumber: intPtr(1),
			ProviderIDs: map[string]string{"MusicBrainzTrack": "mb-sun", "MusicBrainzAlbum": "mb-abbey"},
		}},
		fakeItem{artistID: artistID, Item: Item{
			ID: trackA2ID, Name: "Something", Type: typeAudio,
			Album: "Abbey Road", AlbumID: albumAID, AlbumArtist: "The Beatles", Artists: []string{"The Beatles"},
			ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2),
		}},
		fakeItem{artistID: artistID, Item: Item{
			ID: trackA1ID, Name: "Come Together", Type: typeAudio,
			Album: "Abbey Road", AlbumID: albumAID, AlbumArtist: "The Beatles", Artists: []string{"The Beatles"},
			ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1),
		}},
	)
	f.mu.Lock()
	f.artists = []Item{
		{
			ID: artistID, Name: "The Beatles", Type: typeArtist,
			ProviderIDs: map[string]string{"MusicBrainzArtist": "mb-beatles"},
		},
		{ID: id(399), Name: "Various Artists", Type: typeArtist},
	}
	f.mu.Unlock()
	f.setSessions(Session{
		ID: "sess-1", DeviceName: "Living Room", Client: "Jellyfin Android TV",
		DeviceID: "dev-tv",
	})
}

func TestLaunchPlaysEachKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind *mediaKind
		path string
		want []string
	}{
		{kindMovie, "jellyfin-movie://" + movieID + "/Heat%20%281995%29?tmdb=949", []string{movieID}},
		{kindEpisode, "jellyfin-episode://" + ep2ID + "/Bluey", []string{ep2ID}},
		{kindSong, "jellyfin-song://" + trackA1ID + "/x", []string{trackA1ID}},
		{kindShow, "jellyfin-show://" + showID + "/Bluey", []string{ep1ID, ep2ID, ep3ID}},
		{kindAlbum, "jellyfin-album://" + albumAID + "/x", []string{trackA1ID, trackA2ID, trackA3ID}},
		{
			kindArtist, "jellyfin-artist://" + artistID + "/The%20Beatles",
			[]string{trackA1ID, trackA2ID, trackA3ID, trackB1ID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.kind.label, func(t *testing.T) {
			t.Parallel()
			f := newFakeJellyfin(t)
			seedLibrary(f)
			sent, err := launchPath(context.Background(), testClient(t, f, "Living Room"), tt.kind, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, sent)
			plays := f.playCalls()
			require.Len(t, plays, 1)
			assert.Equal(t, "sess-1", plays[0].session)
			assert.Equal(t, "PlayNow", plays[0].command)
			assert.Equal(t, tt.want, plays[0].ids)
		})
	}
}

func TestLaunchThroughLauncher(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	cfg := testConfig(t, f.URL(), "living room")

	l := NewJellyfinMovieLauncher()
	path := "jellyfin-movie://" + movieID + "/Heat%20%281995%29"
	require.True(t, l.Test(cfg, path))
	proc, err := l.Launch(cfg, path, &platforms.LaunchOptions{})
	require.NoError(t, err)
	assert.Nil(t, proc)
	require.Len(t, f.playCalls(), 1)
}

// TestStopReportsLaunchExit guards that a stop the player accepted clears
// Core's active media: the player is remote, so nothing else reports it.
func TestStopReportsLaunchExit(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	cfg := testConfig(t, f.URL(), "living room")
	l := NewJellyfinMovieLauncher()
	var exits atomic.Int32
	_, err := l.Launch(cfg, "jellyfin-movie://"+movieID+"/Heat", &platforms.LaunchOptions{
		MediaExited: func() { exits.Add(1) },
	})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, l.Controls[platforms.ControlPause].Func(ctx, cfg, platforms.ControlParams{}))
	assert.Zero(t, exits.Load(), "only stop ends the media")

	f.setSessions()
	require.Error(t, l.Controls[platforms.ControlStop].Func(ctx, cfg, platforms.ControlParams{}))
	assert.Zero(t, exits.Load(), "a stop that never reached the player leaves the media active")

	f.setSessions(Session{ID: "sess-1", DeviceName: "Living Room", SupportsRemoteControl: true})
	require.NoError(t, l.Controls[platforms.ControlStop].Func(ctx, cfg, platforms.ControlParams{}))
	assert.Equal(t, int32(1), exits.Load())
}

func TestLaunchQueuesLongListsInChunks(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "sess-1", DeviceName: "TV"})
	f.addItems(fakeItem{Item: Item{ID: albumAID, Name: "Box Set", Type: typeAlbum}})
	want := make([]string, 0, 150)
	for i := range 150 {
		tid := id(1000 + i)
		want = append(want, tid)
		f.addItems(fakeItem{Item: Item{
			ID: tid, Name: strconv.Itoa(i), Type: typeAudio, AlbumID: albumAID,
			Album: "Box Set", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(i + 1),
		}})
	}
	_, err := launchPath(context.Background(), testClient(t, f, ""), kindAlbum,
		"jellyfin-album://"+albumAID+"/Box%20Set")
	require.NoError(t, err)
	plays := f.playCalls()
	require.Len(t, plays, 2)
	assert.Equal(t, "PlayNow", plays[0].command)
	assert.Equal(t, want[:100], plays[0].ids)
	assert.Equal(t, "PlayLast", plays[1].command)
	assert.Equal(t, want[100:], plays[1].ids)
}

// TestLaunchKeepsPlaybackWhenQueueingFails guards that a launch whose first
// chunk is playing succeeds, reporting the IDs actually sent, when a later
// chunk cannot be queued.
func TestLaunchKeepsPlaybackWhenQueueingFails(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.failPlayLast = true
	f.setSessions(Session{ID: "sess-1", DeviceName: "TV"})
	f.addItems(fakeItem{Item: Item{ID: albumAID, Name: "Box Set", Type: typeAlbum}})
	want := make([]string, 0, 150)
	for i := range 150 {
		tid := id(1000 + i)
		want = append(want, tid)
		f.addItems(fakeItem{Item: Item{
			ID: tid, Name: strconv.Itoa(i), Type: typeAudio, AlbumID: albumAID,
			Album: "Box Set", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(i + 1),
		}})
	}
	ids, err := launchPath(context.Background(), testClient(t, f, ""), kindAlbum,
		"jellyfin-album://"+albumAID+"/Box%20Set")
	require.NoError(t, err)
	assert.Equal(t, want[:100], ids)
	plays := f.playCalls()
	require.Len(t, plays, 1)
	assert.Equal(t, "PlayNow", plays[0].command)
}

func TestLaunchFailsWithoutPlayer(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	_, err := launchPath(context.Background(), testClient(t, f, "Bedroom"), kindMovie,
		"jellyfin-movie://"+movieID+"/Heat")
	require.ErrorIs(t, err, ErrNoPlayer)
	assert.Contains(t, err.Error(), "Living Room (Jellyfin Android TV)")
	assert.Empty(t, f.playCalls())
}

func TestControls(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	cfg := testConfig(t, f.URL(), "Living Room")
	l := NewJellyfinSongLauncher()

	want := map[string]string{
		platforms.ControlTogglePause: "PlayPause",
		platforms.ControlPause:       "Pause",
		platforms.ControlResume:      "Unpause",
		platforms.ControlStop:        "Stop",
		platforms.ControlNext:        "NextTrack",
		platforms.ControlPrevious:    "PreviousTrack",
		platforms.ControlFastForward: "FastForward",
		platforms.ControlRewind:      "Rewind",
	}
	require.Len(t, l.Controls, len(want))
	sent := make([]string, 0, len(want))
	for control, command := range want {
		ctrl, ok := l.Controls[control]
		require.True(t, ok, control)
		require.NoError(t, ctrl.Func(context.Background(), cfg, platforms.ControlParams{}))
		sent = append(sent, "sess-1/"+command)
	}
	assert.ElementsMatch(t, sent, f.playstateCalls())
}

func TestControlsResolveSessionEachCall(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	cfg := testConfig(t, f.URL(), "TV")
	stop := NewJellyfinMovieLauncher().Controls[platforms.ControlStop]

	f.setSessions(Session{ID: "first", DeviceName: "TV"})
	require.NoError(t, stop.Func(context.Background(), cfg, platforms.ControlParams{}))
	f.setSessions(Session{ID: "second", DeviceName: "TV"})
	require.NoError(t, stop.Func(context.Background(), cfg, platforms.ControlParams{}))
	assert.Equal(t, []string{"first/Stop", "second/Stop"}, f.playstateCalls())
}

func TestScannerPagesAndNames(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	for i := range 1203 {
		f.addItems(fakeItem{Item: Item{ID: id(5000 + i), Name: fmt.Sprintf("Film %d", i), Type: typeMovie}})
	}
	results, err := scan(context.Background(), testClient(t, f, ""), kindMovie, nil)
	require.NoError(t, err)
	assert.Len(t, results, 1203)

	var starts []string
	for _, r := range f.recorded() {
		if r.path != "/Items" {
			continue
		}
		starts = append(starts, r.query.Get("StartIndex"))
		assert.Equal(t, "500", r.query.Get("Limit"))
		assert.Equal(t, "Movie", r.query.Get("IncludeItemTypes"))
		assert.Equal(t, "true", r.query.Get("Recursive"))
		assert.Equal(t, itemFields, r.query.Get("Fields"))
	}
	assert.Equal(t, []string{"0", "500", "1000"}, starts)
}

func TestScannerEachKind(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	c := testClient(t, f, "")
	scanned := func(kind *mediaKind) map[string]string {
		results, err := scan(context.Background(), c, kind, nil)
		require.NoError(t, err)
		out := map[string]string{}
		for _, r := range results {
			assert.True(t, r.NoExt)
			out[r.Name] = r.Path
		}
		return out
	}

	assert.Equal(t, map[string]string{
		"Heat (1995)": "jellyfin-movie://" + movieID + "/Heat%20%281995%29;tmdb=949;imdb=tt0113277",
	}, scanned(kindMovie))
	assert.Equal(t, map[string]string{
		"Bluey": "jellyfin-show://" + showID + "/Bluey;tvdb=353546",
	}, scanned(kindShow))
	eps := scanned(kindEpisode)
	assert.Equal(t, "jellyfin-episode://"+ep2ID+"/Bluey%20-%20S01E02%20-%20Hospital;tvdb=6772970",
		eps["Bluey - S01E02 - Hospital"])
	assert.Contains(t, eps, "Bluey - S02E01 - Hammerbarn")
	albums := scanned(kindAlbum)
	assert.Equal(t, "jellyfin-album://"+albumAID+"/The%20Beatles%20-%20Abbey%20Road%20%281969%29;mb_album=mb-abbey",
		albums["The Beatles - Abbey Road (1969)"])
	songs := scanned(kindSong)
	assert.Equal(t, "jellyfin-song://"+trackA3ID+
		"/The%20Beatles%20-%20Abbey%20Road%20-%20Here%20Comes%20the%20Sun;mb_track=mb-sun",
		songs["The Beatles - Abbey Road - Here Comes the Sun"])
	assert.Equal(t, map[string]string{
		"The Beatles": "jellyfin-artist://" + artistID + "/The%20Beatles;mb_artist=mb-beatles",
	}, scanned(kindArtist), "Various Artists is skipped")

	for _, r := range f.recorded() {
		if r.path == "/Artists/AlbumArtists" {
			assert.Empty(t, r.query.Get("IncludeItemTypes"))
		}
	}
}

func TestMissingEpisodesAreSkipped(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	f.addItems(fakeItem{seriesID: showID, missing: true, Item: Item{
		ID: id(105), Name: "Unaired", Type: typeEpisode, SeriesName: "Bluey",
		ParentIndexNumber: new(9), IndexNumber: new(1),
	}})
	c := testClient(t, f, "")

	results, err := scan(context.Background(), c, kindEpisode, nil)
	require.NoError(t, err)
	for _, r := range results {
		assert.NotContains(t, r.Name, "Unaired", "a scan does not index a missing episode")
	}

	episodes, err := c.ShowEpisodes(context.Background(), showID)
	require.NoError(t, err)
	require.NotEmpty(t, episodes)
	for _, ep := range episodes {
		assert.NotEqual(t, id(105), ep.ID, "a show does not queue a missing episode")
	}
}

func TestScannerSkipsUnconfigured(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t, "", "")
	in := []platforms.ScanResult{{Name: "kept", Path: "kept"}}
	for _, l := range Launchers() {
		out, err := l.Scanner(context.Background(), cfg, "", in)
		require.NoError(t, err, l.ID)
		assert.Equal(t, in, out, l.ID)
	}
}

func TestScannerHonoursCancellation(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := scan(ctx, testClient(t, f, ""), kindMovie, nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.recorded())
}

func TestScannerUnreachableServer(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	cfg := testConfig(t, f.URL(), "")
	f.srv.Close()
	_, err := NewJellyfinMovieLauncher().Scanner(context.Background(), cfg, "", nil)
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
}

func TestCollections(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	cfg := testConfig(t, f.URL(), "")

	show, err := NewJellyfinTVShowLauncher().Collection(context.Background(), cfg,
		"jellyfin-show://"+showID+"/Bluey")
	require.NoError(t, err)
	require.Len(t, show, 3)
	assert.Equal(t, "Bluey - S01E01 - Magic Xylophone", show[0].Name)
	assert.Equal(t, "jellyfin-episode://"+ep2ID+"/Bluey%20-%20S01E02%20-%20Hospital;tvdb=6772970", show[1].Path)
	assert.Equal(t, "Bluey - S02E01 - Hammerbarn", show[2].Name)

	album, err := NewJellyfinAlbumLauncher().Collection(context.Background(), cfg,
		"jellyfin-album://"+albumAID+"/x")
	require.NoError(t, err)
	require.Len(t, album, 3)
	assert.Equal(t, "The Beatles - Abbey Road - Come Together", album[0].Name)

	artist, err := NewJellyfinArtistLauncher().Collection(context.Background(), cfg,
		"jellyfin-artist://"+artistID+"/The%20Beatles")
	require.NoError(t, err)
	require.Len(t, artist, 4)
	assert.Equal(t, "The Beatles - Revolver - Taxman", artist[3].Name)

	for _, l := range []platforms.Launcher{
		NewJellyfinMovieLauncher(), NewJellyfinTVEpisodeLauncher(), NewJellyfinSongLauncher(),
	} {
		assert.Nil(t, l.Collection, l.ID)
	}
}

func TestStaleIDResolution(t *testing.T) {
	t.Parallel()
	stale := id(999999)
	tests := []struct {
		setup   func(*fakeJellyfin)
		kind    *mediaKind
		name    string
		path    string
		want    string
		wantErr string
	}{
		{
			name: "plain path uses the ID", kind: kindMovie,
			path: "jellyfin-movie://" + movieID + "/Something%20Else", want: movieID,
		},
		{
			name: "provider id", kind: kindMovie,
			path: "jellyfin-movie://" + stale + "/Old%20Name?imdb=tt0113277", want: movieID,
		},
		{
			name: "provider id ignores a conflicting match", kind: kindMovie,
			setup: func(f *fakeJellyfin) {
				f.addItems(fakeItem{Item: Item{
					ID: id(101), Name: "Heat", Type: typeMovie, ProductionYear: 1986,
					ProviderIDs: map[string]string{"Tmdb": "949", "Imdb": "tt0090035"},
				}})
			},
			path: "jellyfin-movie://" + stale + "/Heat?tmdb=949&imdb=tt0113277", want: movieID,
		},
		{
			name: "stored id now holds another item", kind: kindMovie,
			setup: func(f *fakeJellyfin) {
				f.addItems(fakeItem{Item: Item{
					ID: id(104), Name: "Heat", Type: typeMovie, ProductionYear: 1986,
					ProviderIDs: map[string]string{"Tmdb": "12345"},
				}})
			},
			path: "jellyfin-movie://" + id(104) + "/Heat?tmdb=949", want: movieID,
		},
		{
			name: "name when provider ids are gone", kind: kindMovie,
			path: "jellyfin-movie://" + stale + "/heat%20%281995%29?tmdb=1", want: movieID,
		},
		{
			name: "episode by name", kind: kindEpisode,
			path: "jellyfin-episode://" + stale + "/Bluey%20-%20S02E01%20-%20Hammerbarn", want: ep3ID,
		},
		{
			name: "artist by provider id", kind: kindArtist,
			path: "jellyfin-artist://" + stale + "/The%20Beatles?mb_artist=mb-beatles", want: artistID,
		},
		{
			name: "id of another type falls back", kind: kindShow,
			path: "jellyfin-show://" + movieID + "/Bluey?tvdb=353546", want: showID,
		},
		{
			name: "ambiguous name", kind: kindMovie,
			setup: func(f *fakeJellyfin) {
				f.addItems(fakeItem{Item: Item{ID: id(102), Name: "Heat", Type: typeMovie, ProductionYear: 1995}})
			},
			path: "jellyfin-movie://" + stale + "/Heat%20%281995%29", wantErr: "multiple jellyfin movies",
		},
		{
			name: "ambiguous provider id", kind: kindMovie,
			setup: func(f *fakeJellyfin) {
				f.addItems(fakeItem{Item: Item{
					ID: id(103), Name: "Heat Redux", Type: typeMovie,
					ProviderIDs: map[string]string{"Tmdb": "949"},
				}})
			},
			path: "jellyfin-movie://" + stale + "/Other?tmdb=949", wantErr: "provider IDs",
		},
		{
			name: "gone", kind: kindMovie,
			path: "jellyfin-movie://" + stale + "/Ronin%20%281998%29", wantErr: "no longer available",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeJellyfin(t)
			seedLibrary(f)
			if tt.setup != nil {
				tt.setup(f)
			}
			c := testClient(t, f, "")
			ref, err := parseReference(tt.path, tt.kind)
			require.NoError(t, err)
			item, err := resolveItem(context.Background(), c, tt.kind, ref)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, item.ID)
		})
	}
}

func TestStaleIDProviderQuery(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	ref, err := parseReference("jellyfin-movie://"+id(4242)+"/Heat?tmdb=949&imdb=tt0113277", kindMovie)
	require.NoError(t, err)
	_, err = resolveItem(context.Background(), testClient(t, f, ""), kindMovie, ref)
	require.NoError(t, err)
	var found bool
	for _, r := range f.recorded() {
		if r.path == "/Items" && r.query.Has("AnyProviderIdEquals") {
			found = true
			assert.Equal(t, "Tmdb.949,Imdb.tt0113277", r.query.Get("AnyProviderIdEquals"))
			assert.Equal(t, "Movie", r.query.Get("IncludeItemTypes"))
		}
	}
	assert.True(t, found)
}

// TestStaleIDProviderFilterIgnored guards that the provider ID is still
// matched across every page when the server returns its whole library.
func TestStaleIDProviderFilterIgnored(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.ignoresProviderFilter = true
	for i := range pageSize + 50 {
		f.addItems(fakeItem{Item: Item{ID: id(5000 + i), Name: "Filler " + strconv.Itoa(i), Type: typeMovie}})
	}
	f.addItems(
		fakeItem{Item: Item{
			ID: id(9000), Name: "Heat", Type: typeMovie, ProviderIDs: map[string]string{"Tmdb": "1"},
		}},
		fakeItem{Item: Item{
			ID: movieID, Name: "Heat (Director's Cut)", Type: typeMovie, ProviderIDs: map[string]string{"Tmdb": "949"},
		}},
	)
	ref, err := parseReference("jellyfin-movie://"+id(4242)+"/Heat?tmdb=949", kindMovie)
	require.NoError(t, err)
	item, err := resolveItem(context.Background(), testClient(t, f, ""), kindMovie, ref)
	require.NoError(t, err)
	assert.Equal(t, movieID, item.ID)
}

func TestStaleIDServerErrorDoesNotFallBack(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	seedLibrary(f)
	f.getItemErr = 500
	ref, err := parseReference("jellyfin-movie://"+movieID+"/Heat%20%281995%29", kindMovie)
	require.NoError(t, err)
	_, err = resolveItem(context.Background(), testClient(t, f, ""), kindMovie, ref)
	require.ErrorIs(t, err, platforms.ErrScannerSourceUnreachable)
	for _, r := range f.recorded() {
		assert.NotEqual(t, "/Items", r.path, "a server error must not be read as a stale ID")
	}
}

// runReady runs a readiness probe against the fake server on a fake clock,
// calling step before each poll after the first.
func runReady(
	t *testing.T, f *fakeJellyfin, kind *mediaKind, record *launchRecord, media *models.ActiveMedia,
	polls int, step func(int),
) error {
	t.Helper()
	cfg := testConfig(t, f.URL(), "TV")
	clock := clockwork.NewFakeClock()
	probe := waitForPlaying(
		launcherClient(kind.launcherID, []string{shared.LauncherGroupJellyfin}), kind, clock, record)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// waitCtx ends as soon as the probe returns, so the loop never waits on a
	// timer the finished probe will not create.
	waitCtx, stopWaiting := context.WithCancel(ctx)
	defer stopWaiting()
	done := make(chan error, 1)
	go func() {
		done <- probe(ctx, cfg, media)
		stopWaiting()
	}()
	for i := range polls {
		if err := clock.BlockUntilContext(waitCtx, 1); err != nil {
			require.NoError(t, ctx.Err(), "probe neither finished nor waited")
			return <-done
		}
		step(i)
		clock.Advance(readyPollInterval)
	}
	cancel()
	return <-done
}

func TestWaitForReadyLaunchedItem(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "s", DeviceName: "TV"})
	media := &models.ActiveMedia{Path: "jellyfin-movie://" + movieID + "/Heat"}
	err := runReady(t, f, kindMovie, &launchRecord{}, media, 5, func(i int) {
		if i == 1 {
			f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: movieID}})
		}
	})
	require.NoError(t, err)
}

func TestWaitForReadyItemChanged(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: id(7)}})
	media := &models.ActiveMedia{Path: "jellyfin-show://" + showID + "/Bluey"}
	err := runReady(t, f, kindShow, &launchRecord{}, media, 5, func(i int) {
		if i == 0 {
			f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: ep1ID}})
		}
	})
	require.NoError(t, err)
}

func TestWaitForReadyStaysPendingOnSameItem(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: id(7)}})
	media := &models.ActiveMedia{Path: "jellyfin-movie://" + movieID + "/Heat"}
	err := runReady(t, f, kindMovie, &launchRecord{}, media, 3, func(int) {})
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWaitForReadyPlayerMissing(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	media := &models.ActiveMedia{Path: "jellyfin-movie://" + movieID + "/Heat"}
	err := runReady(t, f, kindMovie, &launchRecord{}, media, 2, func(int) {})
	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestLaunchersMetadata(t *testing.T) {
	t.Parallel()
	ls := Launchers()
	require.Len(t, ls, 6)
	want := map[string]struct{ scheme, system string }{
		shared.LauncherJellyfinMovie:     {shared.SchemeJellyfinMovie, systemdefs.SystemMovie},
		shared.LauncherJellyfinTVEpisode: {shared.SchemeJellyfinEpisode, systemdefs.SystemTVEpisode},
		shared.LauncherJellyfinTVShow:    {shared.SchemeJellyfinShow, systemdefs.SystemTVShow},
		shared.LauncherJellyfinSong:      {shared.SchemeJellyfinSong, systemdefs.SystemMusicTrack},
		shared.LauncherJellyfinAlbum:     {shared.SchemeJellyfinAlbum, systemdefs.SystemMusicAlbum},
		shared.LauncherJellyfinArtist:    {shared.SchemeJellyfinArtist, systemdefs.SystemMusicArtist},
	}
	var cfg *config.Instance
	for _, l := range ls {
		w, ok := want[l.ID]
		require.True(t, ok, l.ID)
		assert.Equal(t, []string{w.scheme}, l.Schemes, l.ID)
		assert.Equal(t, w.system, l.SystemID, l.ID)
		assert.Equal(t, []string{shared.LauncherGroupJellyfin}, l.Groups, l.ID)
		assert.True(t, l.SkipFilesystemScan, l.ID)
		assert.Equal(t, platforms.InstanceJellyfin, l.UsesRunningInstance, l.ID)
		require.NotNil(t, l.Test, l.ID)
		require.NotNil(t, l.WaitForReady, l.ID)
		require.NotNil(t, l.Scanner, l.ID)
		assert.True(t, l.Test(cfg, w.scheme+"://"+id(1)+"/Name?tmdb=1&imdb=tt2"), l.ID)
		assert.True(t, l.Test(cfg, strings.ToUpper(w.scheme)+"://"+id(1)+"/Name"), l.ID)
		assert.False(t, l.Test(cfg, w.scheme+":///Name"), l.ID)
		assert.False(t, l.Test(cfg, "kodi-movie://1/Name"), l.ID)
	}
}

func TestWaitForReadyUsesSentIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		playing string
		kind    *mediaKind
		sent    []string
		ready   bool
	}{
		{
			name: "show already on a launched episode",
			kind: kindShow, path: "jellyfin-show://" + showID + "/Bluey",
			sent: []string{ep1ID, ep2ID}, playing: ep2ID, ready: true,
		},
		{
			name: "movie resolved to a new id by fallback",
			kind: kindMovie, path: "jellyfin-movie://" + id(99) + "/Heat",
			sent: []string{movieID}, playing: movieID, ready: true,
		},
		{
			name: "player moved to an unrelated item",
			kind: kindShow, path: "jellyfin-show://" + showID + "/Bluey",
			sent: []string{ep1ID}, playing: id(7),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeJellyfin(t)
			f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: id(8)}})
			record := &launchRecord{}
			record.set(tt.path, tt.sent)
			err := runReady(t, f, tt.kind, record, &models.ActiveMedia{Path: tt.path}, 3, func(i int) {
				if i == 0 {
					f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: tt.playing}})
				}
			})
			if tt.ready {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestWaitForReadyFirstPollMatchesSentID(t *testing.T) {
	t.Parallel()
	f := newFakeJellyfin(t)
	f.setSessions(Session{ID: "s", DeviceName: "TV", NowPlayingItem: &Item{ID: ep1ID}})
	path := "jellyfin-show://" + showID + "/Bluey"
	record := &launchRecord{}
	record.set(path, []string{ep1ID})
	err := runReady(t, f, kindShow, record, &models.ActiveMedia{Path: path}, 1, func(int) {
		t.Fatal("the first poll should already be ready")
	})
	require.NoError(t, err)
}
