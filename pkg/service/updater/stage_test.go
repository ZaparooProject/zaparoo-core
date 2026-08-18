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

package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// testStageVersion is compiled into the fake binary, so every test that
	// reaches the probe stages this version.
	testStageVersion   = "2.11.0"
	testStagePlatform  = "linux"
	testStageArch      = "amd64"
	testCurrentVersion = "2.10.1"
)

// fakeBinaryTemplate is a stand-in release binary. It answers the version probe
// the way the real one does, and picks a way to misbehave from its own file
// name, which is the only channel available: the probe inherits this process's
// environment and staging deletes anything placed alongside the binary, so
// neither an env var nor a sidecar file can select a behaviour per test.
const fakeBinaryTemplate = `package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	switch strings.ToLower(strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")) {
	case "zaparoo-fail":
		os.Stderr.WriteString("error while loading shared libraries: libz.so.1\n")
		os.Exit(1)
	case "zaparoo-wrong":
		os.Stdout.WriteString(__WRONG__)
	case "zaparoo-chatty":
		os.Stderr.WriteString("warning: config file not found, using defaults\n")
		os.Stdout.WriteString("some future build says something here first\n")
		os.Stdout.WriteString(__GOOD__)
	case "zaparoo-hang":
		time.Sleep(10 * time.Minute)
	default:
		os.Stdout.WriteString(__GOOD__)
	}
}
`

var (
	fakeBinaryPath string
	errFakeBinary  error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zaparoo-updater-fake")
	if err == nil {
		defer func() { _ = os.RemoveAll(dir) }()
		fakeBinaryPath, errFakeBinary = buildFakeBinary(dir)
	} else {
		errFakeBinary = err
	}
	m.Run()
}

// buildFakeBinary compiles the stand-in release binary once for the package.
// Nothing else can honestly test the probe: it has to be a real executable,
// and the failures worth catching are a process that will not start, one that
// answers with the wrong version, and one that never answers.
func buildFakeBinary(dir string) (string, error) {
	// Built from config.VersionLine rather than a literal of its own, so this
	// fixture cannot pass while a real release binary would fail the probe.
	source := strings.NewReplacer(
		"__GOOD__", strconv.Quote(config.VersionLine(testStageVersion, testStagePlatform)+"\n"),
		"__WRONG__", strconv.Quote(config.VersionLine("0.0.1", testStagePlatform)+"\n"),
	).Replace(fakeBinaryTemplate)

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o600); err != nil {
		return "", fmt.Errorf("writing the fake binary source: %w", err)
	}
	gomod := "module zaparoofake\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		return "", fmt.Errorf("writing the fake binary go.mod: %w", err)
	}

	out := filepath.Join(dir, testBinaryName("fake"))
	//nolint:gosec // a fixed command compiling a source file this test just wrote
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0")
	if combined, buildErr := cmd.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("building the fake binary: %w (%s)", buildErr, combined)
	}
	return out, nil
}

// testBinaryName adds the extension Windows needs. Without it exec cannot find
// the staged file at all, because Windows resolves even an absolute path
// through PATHEXT.
func testBinaryName(stem string) string {
	if runtime.GOOS == "windows" {
		return stem + ".exe"
	}
	return stem
}

func fakeBinaryBytes(t *testing.T) []byte {
	t.Helper()

	require.NoError(t, errFakeBinary, "the fake release binary did not build")
	body, err := os.ReadFile(fakeBinaryPath) //nolint:gosec // built by this package into its own temp dir
	require.NoError(t, err)
	return body
}

// releaseArchive builds an archive shaped like a real release: the binary plus
// the licence and readme that must not be extracted.
func releaseArchive(t *testing.T, ext, memberName string, binary []byte) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "release"+ext)
	switch ext {
	case archiveExtTarGz:
		writeTarGz(t, path, []tarMember{
			{name: "LICENSE.txt", body: []byte("gpl")},
			{name: "README.txt", body: []byte("readme")},
			{name: memberName, body: binary, mode: 0o755},
		})
	case archiveExtZip:
		writeZip(t, path, []zipMember{
			{name: "LICENSE.txt", body: []byte("gpl")},
			{name: "README.txt", body: []byte("readme")},
			{name: memberName, body: binary, mode: 0o755},
		})
	default:
		t.Fatalf("unsupported archive extension %q", ext)
	}

	body, err := os.ReadFile(path) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	return body
}

// testArchiveName is the only name a release may publish an archive under for
// this platform and version, which is what binds the archive to the version.
func testArchiveName(version, ext string) string {
	return otameta.ArchiveBaseName(testStagePlatform, testStageArch, version) + ext
}

// servedAsset publishes body over HTTP and describes it exactly as a verified
// manifest would.
func servedAsset(t *testing.T, name string, body []byte) *otameta.Asset {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(body)
	return &otameta.Asset{
		Name:   name,
		URL:    srv.URL + "/" + name,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}
}

func testRelease(tag string, assets ...*otameta.Asset) *otameta.Release {
	return &otameta.Release{
		Name:    tag,
		TagName: tag,
		Channel: otameta.ChannelStable,
		Assets:  assets,
		Rollout: 100,
	}
}

func testAssetFetcher(t *testing.T) assetFetcher {
	t.Helper()

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return assetFetcherFor(transport)
}

// unusedFetcher fails loudly, for the stages that must decide before any bytes
// are requested.
func unusedFetcher(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("the archive should not have been fetched")
}

func testStageOptions(t *testing.T, rel *otameta.Release, stem string) *StageOptions {
	t.Helper()

	root := t.TempDir()
	return &StageOptions{
		Release:    rel,
		PlatformID: testStagePlatform,
		Arch:       testStageArch,
		// Pinned rather than taken from the host, so one set of fixtures covers
		// the archive member rules whatever this test is running on.
		OS:             "linux",
		TargetPath:     filepath.Join(root, "install", testBinaryName(stem)),
		StagingRoot:    filepath.Join(root, "updater", stagingSubdir),
		CurrentVersion: testCurrentVersion,
	}
}

func TestNewStager_RequiresItsInputs(t *testing.T) {
	t.Parallel()

	valid := &StageOptions{
		Release:        testRelease("v2.11.0"),
		PlatformID:     testStagePlatform,
		TargetPath:     filepath.Join("install", "zaparoo"),
		StagingRoot:    filepath.Join("data", "updater", "staging"),
		CurrentVersion: testCurrentVersion,
	}

	tests := []struct {
		mutate  func(*StageOptions)
		name    string
		wantMsg string
		noFetch bool
	}{
		{
			name:    "no release",
			mutate:  func(o *StageOptions) { o.Release = nil },
			wantMsg: "needs a release",
		},
		{
			name:    "no platform",
			mutate:  func(o *StageOptions) { o.PlatformID = "" },
			wantMsg: "needs a platform id",
		},
		{
			name:    "no running version",
			mutate:  func(o *StageOptions) { o.CurrentVersion = "" },
			wantMsg: "needs the running version",
		},
		{
			name:    "no staging root",
			mutate:  func(o *StageOptions) { o.StagingRoot = "" },
			wantMsg: "needs a staging directory",
		},
		{
			name:    "no target path",
			mutate:  func(o *StageOptions) { o.TargetPath = "" },
			wantMsg: "needs the path of the binary to replace",
		},
		{
			name:    "target path is a directory",
			mutate:  func(o *StageOptions) { o.TargetPath = "." },
			wantMsg: "needs the path of the binary to replace",
		},
		{
			name:    "no fetcher",
			mutate:  func(*StageOptions) {},
			noFetch: true,
			wantMsg: "needs a way to fetch the archive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := *valid
			tt.mutate(&opts)

			fetch := assetFetcher(unusedFetcher)
			if tt.noFetch {
				fetch = nil
			}

			s, err := newStager(&opts, fetch)
			require.Error(t, err)
			assert.Nil(t, s)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestNewStager_DefaultsToThisBuild(t *testing.T) {
	t.Parallel()

	s, err := newStager(&StageOptions{
		Release:        testRelease("v2.11.0"),
		PlatformID:     testStagePlatform,
		TargetPath:     filepath.Join("install", "zaparoo"),
		StagingRoot:    filepath.Join("data", "updater", "staging"),
		CurrentVersion: testCurrentVersion,
	}, unusedFetcher)

	require.NoError(t, err)
	assert.Equal(t, runtime.GOOS, s.goos)
	assert.Equal(t, runtime.GOARCH, s.goarch)
	assert.Equal(t, "zaparoo", s.binaryName)
}

func TestStage_ValidatesBeforeTouchingTheNetwork(t *testing.T) {
	t.Parallel()

	staged, err := Stage(context.Background(), &StageOptions{})
	require.Error(t, err)
	assert.Nil(t, staged)
}

// TestSelectArchive covers the assertions that make a stale or tampered
// manifest unable to install something older than what is running. They are
// made here against the release's own tag rather than trusted from whatever
// offered the update.
func TestSelectArchive(t *testing.T) {
	t.Parallel()

	asset := func(version string) *otameta.Asset {
		return &otameta.Asset{Name: testArchiveName(version, archiveExtTarGz), Size: 1024}
	}

	tests := []struct {
		wantErr     error
		build       func() *otameta.Release
		name        string
		current     string
		wantMsg     string
		wantVersion string
	}{
		{
			name: "newer release",
			build: func() *otameta.Release {
				return testRelease("v2.11.0", asset("2.11.0"))
			},
			current:     "2.10.1",
			wantVersion: "2.11.0",
		},
		{
			name: "draft",
			build: func() *otameta.Release {
				rel := testRelease("v2.11.0", asset("2.11.0"))
				rel.Draft = true
				return rel
			},
			current: "2.10.1",
			wantErr: ErrArchiveRejected,
			wantMsg: "is a draft",
		},
		{
			name: "same version",
			build: func() *otameta.Release {
				return testRelease("v2.10.1", asset("2.10.1"))
			},
			current: "2.10.1",
			wantErr: ErrNotAnUpgrade,
		},
		{
			name: "older version",
			build: func() *otameta.Release {
				return testRelease("v2.9.0", asset("2.9.0"))
			},
			current: "2.10.1",
			wantErr: ErrNotAnUpgrade,
		},
		{
			name: "prerelease of the running version is not an upgrade",
			build: func() *otameta.Release {
				return testRelease("v2.10.1-beta.1", asset("2.10.1-beta.1"))
			},
			current: "2.10.1",
			wantErr: ErrNotAnUpgrade,
		},
		{
			name: "unusable release version",
			build: func() *otameta.Release {
				return testRelease("vnot-a-version", asset("not-a-version"))
			},
			current: "2.10.1",
			wantErr: ErrArchiveRejected,
			wantMsg: "no usable version",
		},
		{
			name: "unusable running version",
			build: func() *otameta.Release {
				return testRelease("v2.11.0", asset("2.11.0"))
			},
			current: "not-a-version",
			wantMsg: "reading the running version",
		},
		{
			name: "upgrade floor above the running version",
			build: func() *otameta.Release {
				rel := testRelease("v2.11.0", asset("2.11.0"))
				rel.MinUpgradeFrom = "2.10.2"
				return rel
			},
			current: "2.10.1",
			wantErr: ErrUpgradeFloor,
			wantMsg: "needs 2.10.2 or newer first",
		},
		{
			name: "upgrade floor equal to the running version",
			build: func() *otameta.Release {
				rel := testRelease("v2.11.0", asset("2.11.0"))
				rel.MinUpgradeFrom = "2.10.1"
				return rel
			},
			current:     "2.10.1",
			wantVersion: "2.11.0",
		},
		{
			name: "upgrade floor below the running version",
			build: func() *otameta.Release {
				rel := testRelease("v2.11.0", asset("2.11.0"))
				rel.MinUpgradeFrom = "2.6.0"
				return rel
			},
			current:     "2.10.1",
			wantVersion: "2.11.0",
		},
		{
			name: "unusable upgrade floor",
			build: func() *otameta.Release {
				rel := testRelease("v2.11.0", asset("2.11.0"))
				rel.MinUpgradeFrom = "sometime"
				return rel
			},
			current: "2.10.1",
			wantErr: ErrArchiveRejected,
			wantMsg: "unusable min_upgrade_from",
		},
		// A manifest claiming a high version while carrying a genuine older
		// archive selects nothing: the candidate name is built from the tag, so
		// relabelling the release stops it matching its own assets.
		{
			name: "relabelled release does not match its own archives",
			build: func() *otameta.Release {
				return testRelease("v99.0.0", asset("2.10.1"))
			},
			current: "2.10.1",
			wantErr: otameta.ErrNoAsset,
		},
		{
			name: "no archive for this platform",
			build: func() *otameta.Release {
				return testRelease("v2.11.0", &otameta.Asset{
					Name: otameta.ArchiveBaseName("windows", "amd64", "2.11.0") + archiveExtZip,
				})
			},
			current: "2.10.1",
			wantErr: otameta.ErrNoAsset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := testStageOptions(t, tt.build(), "zaparoo")
			opts.CurrentVersion = tt.current
			s, err := newStager(opts, unusedFetcher)
			require.NoError(t, err)

			got, version, err := s.selectArchive()
			if tt.wantVersion != "" {
				require.NoError(t, err)
				require.NotNil(t, got)
				assert.Equal(t, tt.wantVersion, version)
				assert.Equal(t, testArchiveName(tt.wantVersion, archiveExtTarGz), got.Name)
				return
			}

			require.Error(t, err)
			assert.Empty(t, version)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestSelectArchive_ArmDoesNotPickUpArm64(t *testing.T) {
	t.Parallel()

	rel := testRelease("v2.11.0", &otameta.Asset{
		Name: otameta.ArchiveBaseName("mister", "arm64", "2.11.0") + archiveExtZip,
	})
	opts := testStageOptions(t, rel, "zaparoo.sh")
	opts.PlatformID = "mister"
	opts.Arch = "arm"

	s, err := newStager(opts, unusedFetcher)
	require.NoError(t, err)

	_, _, err = s.selectArchive()
	require.ErrorIs(t, err, otameta.ErrNoAsset)
}

func TestDownloadArchive(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("zaparoo release archive"), 64)
	sum := sha256.Sum256(body)

	tests := []struct {
		wantErr error
		mutate  func(*otameta.Asset)
		name    string
		wantMsg string
	}{
		{
			name:   "accepts what the manifest describes",
			mutate: func(*otameta.Asset) {},
		},
		{
			name:    "no declared size",
			mutate:  func(a *otameta.Asset) { a.Size = 0 },
			wantErr: ErrArchiveRejected,
			wantMsg: "declares no size",
		},
		{
			name:    "negative declared size",
			mutate:  func(a *otameta.Asset) { a.Size = -1 },
			wantErr: ErrArchiveRejected,
			wantMsg: "declares no size",
		},
		{
			name:    "declared size over the cap",
			mutate:  func(a *otameta.Asset) { a.Size = maxArchiveBytes + 1 },
			wantErr: ErrArchiveRejected,
			wantMsg: "over the",
		},
		{
			name:    "unusable digest",
			mutate:  func(a *otameta.Asset) { a.SHA256 = "not hex" },
			wantErr: ErrArchiveRejected,
			wantMsg: "no usable sha256",
		},
		{
			name:    "digest of the wrong length",
			mutate:  func(a *otameta.Asset) { a.SHA256 = hex.EncodeToString(sum[:16]) },
			wantErr: ErrArchiveRejected,
			wantMsg: "no usable sha256",
		},
		{
			name:    "digest does not match the bytes",
			mutate:  func(a *otameta.Asset) { a.SHA256 = strings.Repeat("ab", sha256.Size) },
			wantErr: ErrChecksumMismatch,
		},
		{
			name:    "shorter than declared",
			mutate:  func(a *otameta.Asset) { a.Size = int64(len(body)) + 10 },
			wantErr: ErrArchiveRejected,
			wantMsg: "the manifest declares",
		},
		{
			name:    "longer than declared",
			mutate:  func(a *otameta.Asset) { a.Size = int64(len(body)) - 10 },
			wantErr: ErrArchiveRejected,
			wantMsg: "the manifest declares",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			asset := servedAsset(t, testArchiveName(testStageVersion, archiveExtTarGz), body)
			tt.mutate(asset)

			opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
			s, err := newStager(opts, testAssetFetcher(t))
			require.NoError(t, err)

			dest := filepath.Join(t.TempDir(), asset.Name)
			err = s.downloadArchive(context.Background(), asset, dest)

			if tt.wantErr == nil && tt.wantMsg == "" {
				require.NoError(t, err)
				got, readErr := os.ReadFile(dest) //nolint:gosec // test path under t.TempDir
				require.NoError(t, readErr)
				assert.Equal(t, body, got)
				return
			}

			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestDownloadArchive_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	asset := &otameta.Asset{
		Name:   testArchiveName(testStageVersion, archiveExtTarGz),
		URL:    srv.URL + "/missing",
		SHA256: strings.Repeat("00", sha256.Size),
		Size:   1024,
	}
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
	s, err := newStager(opts, testAssetFetcher(t))
	require.NoError(t, err)

	dest := filepath.Join(t.TempDir(), asset.Name)
	err = s.downloadArchive(context.Background(), asset, dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NoFileExists(t, dest)
}

// TestDownloadArchive_Stalls proves the guard bounds silence rather than total
// duration: the server answers, sends a little, and then never sends again.
func TestDownloadArchive_Stalls(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("x"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:8])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(body)
	asset := &otameta.Asset{
		Name:   testArchiveName(testStageVersion, archiveExtTarGz),
		URL:    srv.URL + "/archive",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}

	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
	s, err := newStager(opts, testAssetFetcher(t))
	require.NoError(t, err)
	s.stallTimeout = 200 * time.Millisecond

	dest := filepath.Join(t.TempDir(), asset.Name)
	err = s.downloadArchive(context.Background(), asset, dest)
	require.ErrorIs(t, err, ErrDownloadStalled)
	assert.Contains(t, err.Error(), "no progress")
}

func TestDownloadArchive_CallerCancels(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("x"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:8])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(body)
	asset := &otameta.Asset{
		Name:   testArchiveName(testStageVersion, archiveExtTarGz),
		URL:    srv.URL + "/archive",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}

	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
	s, err := newStager(opts, testAssetFetcher(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	dest := filepath.Join(t.TempDir(), asset.Name)
	err = s.downloadArchive(ctx, asset, dest)
	require.Error(t, err)
	// A caller giving up is not a stall, and must not be reported as one.
	require.NotErrorIs(t, err, ErrDownloadStalled)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestDownloadArchive_StallsBeforeTheFirstByte covers the window the guard is
// running in but the body read is not: DNS, the dial, TLS and the response
// headers across every redirect hop. The stall timeout is shortened because the
// transport built for these tests carries no deadlines of its own, which leaves
// the guard as the only thing watching; production gets to the same verdict
// through the transport instead, and TestDownloadArchive_TransportDeadlineStalls
// pins that path at the shipped stall timeout. Either way a caller who has not
// given up must not be told the download was cancelled.
func TestDownloadArchive_StallsBeforeTheFirstByte(t *testing.T) {
	t.Parallel()

	// The handler writes nothing, so net/http sends no response at all and the
	// client blocks waiting for the headers.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	asset := &otameta.Asset{
		Name:   testArchiveName(testStageVersion, archiveExtTarGz),
		URL:    srv.URL + "/archive",
		SHA256: strings.Repeat("00", sha256.Size),
		Size:   4096,
	}
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
	s, err := newStager(opts, testAssetFetcher(t))
	require.NoError(t, err)
	s.stallTimeout = 200 * time.Millisecond

	dest := filepath.Join(t.TempDir(), asset.Name)
	err = s.downloadArchive(context.Background(), asset, dest)
	require.ErrorIs(t, err, ErrDownloadStalled)
	assert.Contains(t, err.Error(), "after 0 bytes")
	assert.NoFileExists(t, dest)
}

// TestDownloadArchive_TransportDeadlineStalls is the same dead network as above
// with nothing about the stager weakened: the shipped stall timeout stands, and
// the deadline that fires is the one the transport owns for the response
// headers. A verdict has to come out of that too, or the sentinel would be
// unreachable for the ordinary case of a link that is up and a server that is
// gone.
func TestDownloadArchive_TransportDeadlineStalls(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	transport := &http.Transport{ResponseHeaderTimeout: 200 * time.Millisecond}
	t.Cleanup(transport.CloseIdleConnections)

	asset := &otameta.Asset{
		Name:   testArchiveName(testStageVersion, archiveExtTarGz),
		URL:    srv.URL + "/archive",
		SHA256: strings.Repeat("00", sha256.Size),
		Size:   4096,
	}
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
	s, err := newStager(opts, assetFetcherFor(transport))
	require.NoError(t, err)
	require.Equal(t, downloadStallTimeout, s.stallTimeout)

	dest := filepath.Join(t.TempDir(), asset.Name)
	err = s.downloadArchive(context.Background(), asset, dest)
	require.ErrorIs(t, err, ErrDownloadStalled)
	// Not the release's fault, so not a verdict on it.
	require.NotErrorIs(t, err, ErrArchiveRejected)
	assert.Contains(t, err.Error(), "after 0 bytes")
	assert.NoFileExists(t, dest)
}

func TestStageRelease_TarGz(t *testing.T) {
	t.Parallel()

	assertStagesCleanly(t, archiveExtTarGz)
}

func TestStageRelease_Zip(t *testing.T) {
	t.Parallel()

	assertStagesCleanly(t, archiveExtZip)
}

func assertStagesCleanly(t *testing.T, ext string) {
	t.Helper()

	binary := fakeBinaryBytes(t)
	name := testArchiveName(testStageVersion, ext)
	asset := servedAsset(t, name, releaseArchive(t, ext, testBinaryName("zaparoo"), binary))
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.NoError(t, err)
	require.NotNil(t, staged)

	assert.Equal(t, testStageVersion, staged.Version)
	assert.Equal(t, filepath.Join(opts.StagingRoot, testStageVersion), staged.Dir)
	assert.Equal(t, filepath.Join(staged.Dir, name), staged.ArchivePath)
	assert.Equal(t, filepath.Join(staged.Dir, payloadSubdir, testBinaryName("zaparoo")), staged.BinaryPath)
	assert.FileExists(t, staged.ArchivePath)

	got, err := os.ReadFile(staged.BinaryPath) //nolint:gosec // path this package built under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, binary, got)

	// The licence and readme are in the archive and stay there.
	entries, err := os.ReadDir(filepath.Join(staged.Dir, payloadSubdir))
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(staged.BinaryPath)
		require.NoError(t, statErr)
		assert.NotZero(t, info.Mode().Perm()&0o111, "the staged binary is not executable")
	}

	// Nothing outside the staging directory was touched.
	assert.NoDirExists(t, filepath.Dir(opts.TargetPath))
}

func TestStageRelease_ReplacesAStaleStagingDirectory(t *testing.T) {
	t.Parallel()

	ext := archiveExtTarGz
	asset := servedAsset(t, testArchiveName(testStageVersion, ext),
		releaseArchive(t, ext, testBinaryName("zaparoo"), fakeBinaryBytes(t)))
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")

	// An attempt that died without cleaning up leaves a directory behind, and
	// the archive is created exclusively, so it has to go.
	stale := filepath.Join(opts.StagingRoot, testStageVersion)
	require.NoError(t, os.MkdirAll(filepath.Join(stale, payloadSubdir), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stale, testArchiveName(testStageVersion, ext)),
		[]byte("half a download"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "junk"), []byte("junk"), 0o600))

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(staged.Dir, "junk"))
}

// TestStageRelease_PrunesOrphanedStagingDirectories covers the directory no
// later attempt is looking for. The failure path only runs when the process
// lives to reach it, so a power cut mid-download leaves a version directory that
// the next release, computing a different name, would never touch.
func TestStageRelease_PrunesOrphanedStagingDirectories(t *testing.T) {
	t.Parallel()

	ext := archiveExtTarGz
	asset := servedAsset(t, testArchiveName(testStageVersion, ext),
		releaseArchive(t, ext, testBinaryName("zaparoo"), fakeBinaryBytes(t)))
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")

	orphan := filepath.Join(opts.StagingRoot, "2.9.0")
	require.NoError(t, os.MkdirAll(filepath.Join(orphan, payloadSubdir), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "half-a-download"),
		bytes.Repeat([]byte("x"), 1024), 0o600))

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.NoError(t, err)

	assert.NoDirExists(t, orphan, "an orphaned version directory was left on disk")
	assert.DirExists(t, staged.Dir, "pruning removed the directory being staged into")
}

func TestStageRelease_FailureLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	ext := archiveExtTarGz
	asset := servedAsset(t, testArchiveName(testStageVersion, ext),
		releaseArchive(t, ext, testBinaryName("zaparoo"), fakeBinaryBytes(t)))
	asset.SHA256 = strings.Repeat("cd", sha256.Size)
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.ErrorIs(t, err, ErrChecksumMismatch)
	assert.Nil(t, staged)
	assert.NoDirExists(t, filepath.Join(opts.StagingRoot, testStageVersion))
}

func TestStageRelease_ArchiveWithoutTheBinary(t *testing.T) {
	t.Parallel()

	ext := archiveExtTarGz
	path := filepath.Join(t.TempDir(), "release"+ext)
	writeTarGz(t, path, []tarMember{{name: "LICENSE.txt", body: []byte("gpl")}})
	body, err := os.ReadFile(path) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)

	asset := servedAsset(t, testArchiveName(testStageVersion, ext), body)
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.Nil(t, staged)
	assert.NoDirExists(t, filepath.Join(opts.StagingRoot, testStageVersion))
}

// TestStageRelease_ProbeFailures is the check that keeps a build which cannot
// run on this device from ever reaching a platform with no supervisor to
// recover it.
func TestStageRelease_ProbeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		stem         string
		wantMsg      string
		binary       []byte
		probeTimeout time.Duration
	}{
		{
			name:    "exits non-zero",
			stem:    "zaparoo-fail",
			wantMsg: "libz.so.1",
		},
		{
			name:    "answers with the wrong version",
			stem:    "zaparoo-wrong",
			wantMsg: "Zaparoo v0.0.1",
		},
		{
			name:         "never answers",
			stem:         "zaparoo-hang",
			probeTimeout: 300 * time.Millisecond,
			wantMsg:      "no answer within",
		},
		{
			name:    "is not an executable",
			stem:    "zaparoo",
			binary:  bytes.Repeat([]byte("not an executable"), 64),
			wantMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			binary := tt.binary
			if binary == nil {
				binary = fakeBinaryBytes(t)
			}

			ext := archiveExtTarGz
			member := testBinaryName(tt.stem)
			asset := servedAsset(t, testArchiveName(testStageVersion, ext),
				releaseArchive(t, ext, member, binary))
			opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), tt.stem)

			s, err := newStager(opts, testAssetFetcher(t))
			require.NoError(t, err)
			if tt.probeTimeout > 0 {
				s.probeTimeout = tt.probeTimeout
			}

			staged, err := s.run(context.Background())
			require.ErrorIs(t, err, ErrProbeFailed)
			assert.Nil(t, staged)
			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
			// Checked immediately, which the code can be held to: these are the
			// subtests that exec a staged binary, and removal is retried for long
			// enough to outlast an image file the OS has not finished releasing.
			assert.NoDirExists(t, filepath.Join(opts.StagingRoot, testStageVersion))
		})
	}
}

// TestStageRelease_AcceptsProbeOutputBesideTheVersionLine is the compatibility
// half of the probe. The comparison is made by the build already installed
// against one that did not exist when it shipped, so a future release that prints
// a warning alongside its version must not be condemned as unrunnable for it.
func TestStageRelease_AcceptsProbeOutputBesideTheVersionLine(t *testing.T) {
	t.Parallel()

	ext := archiveExtTarGz
	asset := servedAsset(t, testArchiveName(testStageVersion, ext),
		releaseArchive(t, ext, testBinaryName("zaparoo-chatty"), fakeBinaryBytes(t)))
	opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo-chatty")

	staged, err := stageRelease(context.Background(), opts, testAssetFetcher(t))
	require.NoError(t, err)
	require.NotNil(t, staged)
	assert.Equal(t, testStageVersion, staged.Version)
}

// TestStageRelease_ChmodFailureIsLeftToTheProbe covers the filesystem MiSTer and
// MiSTeX install to, which no test host has: /media/fat is vfat or exFAT, it has
// no mode bits, and depending on which driver mounted it chmod either silently
// does nothing or returns an error while the mount's mask has already made the
// file executable. Refusing on the error would reject a release that runs.
//
// Both halves are asserted together, because the first is only safe while the
// second holds: a chmod error on its own does not condemn the release, and the
// probe still does when the binary genuinely cannot execute.
func TestStageRelease_ChmodFailureIsLeftToTheProbe(t *testing.T) {
	t.Parallel()

	chmodFailed := errors.New("operation not supported")

	tests := []struct {
		wantErr     error
		chmod       func(*testing.T) func(string, os.FileMode) error
		name        string
		wantSuccess bool
	}{
		{
			name: "mask already granted the exec bit",
			chmod: func(t *testing.T) func(string, os.FileMode) error {
				t.Helper()
				return func(path string, mode os.FileMode) error {
					// Standing in for the mount mask: the file ends up executable
					// without this call being what did it. On a filesystem that has
					// mode bits, doing the chmod for real is how that is reproduced.
					//nolint:gosec // G703: staging path under t.TempDir
					require.NoError(t, os.Chmod(path, mode))
					return chmodFailed
				}
			},
			wantSuccess: true,
		},
		{
			name: "nothing made the binary executable",
			chmod: func(t *testing.T) func(string, os.FileMode) error {
				t.Helper()
				return func(string, os.FileMode) error { return chmodFailed }
			},
			wantErr: ErrProbeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ext := archiveExtTarGz
			asset := servedAsset(t, testArchiveName(testStageVersion, ext),
				releaseArchive(t, ext, testBinaryName("zaparoo"), fakeBinaryBytes(t)))
			opts := testStageOptions(t, testRelease("v"+testStageVersion, asset), "zaparoo")
			s, err := newStager(opts, testAssetFetcher(t))
			require.NoError(t, err)
			s.chmod = tt.chmod(t)

			staged, runErr := s.run(context.Background())
			if tt.wantSuccess {
				require.NoError(t, runErr)
				require.NotNil(t, staged)
				assert.Equal(t, testStageVersion, staged.Version)
				return
			}
			require.ErrorIs(t, runErr, tt.wantErr)
			assert.Nil(t, staged)
		})
	}
}

// TestProbeBinary_CallerCancellationIsNotAProbeFailure keeps a shutdown from
// condemning a build. A probe that was interrupted never reached a verdict, and
// reporting one would mark a perfectly good release as unrunnable on this device.
func TestProbeBinary_CallerCancellationIsNotAProbeFailure(t *testing.T) {
	t.Parallel()

	opts := testStageOptions(t, testRelease("v"+testStageVersion), "zaparoo")
	s, err := newStager(opts, unusedFetcher)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	err = s.probeBinary(ctx, executableCopy(t, "zaparoo-hang"), testStageVersion)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProbeFailed)
	assert.Contains(t, err.Error(), "cancelled")
}

// executableCopy puts the fake release binary somewhere named stem, which is how
// its behaviour is chosen.
func executableCopy(t *testing.T, stem string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), testBinaryName(stem))
	//nolint:gosec // it has to be executable to be probed at all
	require.NoError(t, os.WriteFile(path, fakeBinaryBytes(t), 0o755))
	return path
}

func TestHasLine(t *testing.T) {
	t.Parallel()

	const want = "Zaparoo v2.11.0 (mister)"

	assert.True(t, hasLine(want+"\n", want))
	assert.True(t, hasLine(want, want), "output without a trailing newline")
	assert.True(t, hasLine(want+"\r\n", want), "output through a CRLF channel")
	assert.True(t, hasLine("warning: something\n"+want+"\n", want))
	assert.True(t, hasLine(want+"\nnote: something\n", want))

	assert.False(t, hasLine("", want))
	assert.False(t, hasLine("Zaparoo v2.11.0 (linux)\n", want))
	assert.False(t, hasLine("prefix "+want+"\n", want), "a line that merely contains it")
	assert.False(t, hasLine(want+" suffix\n", want), "a line that merely starts with it")
}

func TestStagingRootFor(t *testing.T) {
	t.Parallel()

	assert.Empty(t, stagingRootFor(""))
	assert.Equal(t,
		filepath.Join("data", "updater", stagingSubdir),
		stagingRootFor("data"))
}

func TestClip(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", clip("  short\n", 32))
	assert.Equal(t, "abc…", clip("abcdef", 3))
}
