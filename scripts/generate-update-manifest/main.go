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

// generate-update-manifest maintains the signed update manifest published to
// the Zaparoo CDN. It is the only writer of that document: releases enter the
// manifest through an explicit promote, leave through a withdraw, and have
// their automatic rollout adjusted in place, with a monotonic generation
// counter stamped on every publish.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	modePromote  = "promote"
	modeRollout  = "rollout"
	modeWithdraw = "withdraw"
)

// options holds the parsed command line.
type options struct {
	mode            string
	manifestPath    string
	checksumsPath   string
	outDir          string
	summaryPath     string
	keyID           string
	tag             string
	channel         string
	minUpgradeFrom  string
	releaseNotes    string
	githubRelease   string
	archivesDir     string
	generationFloor int64
	retainStable    int
	retainBeta      int
	notesLimit      int
	rollout         int
	replace         bool
}

// result is what the run produced, for logging and the CI job summary.
type result struct {
	tag           string
	channel       string
	dropped       []string
	archives      int
	rollout       int
	generation    int64
	releases      int
	checksumBytes int
}

var errUsage = errors.New("invalid arguments")

func parseFlags() *options {
	opts := &options{}

	flag.StringVar(&opts.mode, "mode", modePromote,
		"operation: promote, rollout or withdraw")
	flag.StringVar(&opts.manifestPath, "manifest", "",
		"path to the currently published manifest (omit only to bootstrap)")
	flag.StringVar(&opts.checksumsPath, "checksums", "",
		"path to the currently published checksums.txt, used to carry digests forward")
	flag.StringVar(&opts.outDir, "out-dir", "_publish",
		"directory to write manifest.yaml and checksums.txt into")
	flag.StringVar(&opts.summaryPath, "summary", "",
		"optional path to write a markdown summary of the change")
	flag.StringVar(&opts.keyID, "key-id", "k1",
		"identifier of the signing key the manifest will be signed with")
	flag.Int64Var(&opts.generationFloor, "generation-floor", 0,
		"lower bound for the new generation counter; the published generation plus one is used if higher")
	flag.IntVar(&opts.retainStable, "retain-stable", 5,
		"number of stable releases to keep")
	flag.IntVar(&opts.retainBeta, "retain-beta", 5,
		"number of beta releases to keep")
	flag.IntVar(&opts.notesLimit, "notes-limit", 2000,
		"maximum release notes length for superseded releases, in runes")

	flag.StringVar(&opts.tag, "tag", "",
		"release tag to promote, adjust or withdraw")
	flag.StringVar(&opts.channel, "channel", channelStable,
		"channel to promote into: stable or beta")
	flag.IntVar(&opts.rollout, "rollout", fullRollout,
		"percentage of devices eligible for automatic installation, 0 to 100")
	flag.StringVar(&opts.minUpgradeFrom, "min-upgrade-from", "",
		"minimum version a device must already run to install this release directly")
	flag.StringVar(&opts.releaseNotes, "release-notes", "",
		"release notes text to include in the manifest")
	flag.StringVar(&opts.githubRelease, "github-release", "",
		"GitHub release metadata JSON from gh release view")
	flag.StringVar(&opts.archivesDir, "archives-dir", "",
		"directory containing the downloaded release archives to hash")
	flag.BoolVar(&opts.replace, "replace", false,
		"allow promote to overwrite a release already in the manifest")

	flag.Parse()
	return opts
}

func (o *options) validate() error {
	switch o.mode {
	case modePromote:
		if o.tag == "" || o.archivesDir == "" {
			return fmt.Errorf("%w: promote requires --tag and --archives-dir", errUsage)
		}
		if o.channel != channelStable && o.channel != channelBeta {
			return fmt.Errorf("%w: --channel must be %s or %s", errUsage, channelStable, channelBeta)
		}
	case modeRollout, modeWithdraw:
		if o.tag == "" {
			return fmt.Errorf("%w: %s requires --tag", errUsage, o.mode)
		}
		if o.manifestPath == "" {
			return fmt.Errorf("%w: %s requires --manifest", errUsage, o.mode)
		}
	default:
		return fmt.Errorf("%w: unknown mode %q", errUsage, o.mode)
	}
	return nil
}

// loadCurrent reads the published manifest, or returns an empty one when
// bootstrapping. The second return value is the generation it arrived with,
// captured before any mutation so the new generation can be validated against
// what is actually live on the CDN.
func loadCurrent(fs afero.Fs, path string) (current *manifest, publishedGen int64, err error) {
	if path == "" {
		return &manifest{}, 0, nil
	}

	current, err = loadManifest(fs, path)
	if err != nil {
		return nil, 0, err
	}
	return current, current.Generation, nil
}

func loadPublishedChecksums(fs afero.Fs, path string) ([]checksumEntry, error) {
	if path == "" {
		return nil, nil
	}
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("reading published checksums: %w", err)
	}
	entries, err := parseChecksums(data)
	if err != nil {
		return nil, fmt.Errorf("parsing published checksums: %w", err)
	}
	return entries, nil
}

// applyPromote gathers the local archives, cross-checks them against the
// GitHub release, and adds the release to the manifest.
func applyPromote(fs afero.Fs, m *manifest, opts *options) (*release, error) {
	archives, err := scanArchives(fs, opts.archivesDir)
	if err != nil {
		return nil, err
	}

	publishedAt := time.Now().UTC()
	releaseURL := ""
	if opts.githubRelease != "" {
		ghRelease, loadErr := loadGithubRelease(fs, opts.githubRelease)
		if loadErr != nil {
			return nil, loadErr
		}
		if validateErr := validateGithubReleaseMetadata(ghRelease, opts.tag, opts.channel); validateErr != nil {
			return nil, validateErr
		}
		if checkErr := crossCheckGithubDigests(ghRelease, archives); checkErr != nil {
			return nil, checkErr
		}
		publishedAt = ghRelease.PublishedAt.UTC()
		releaseURL = ghRelease.URL
	}

	return promote(m, &promoteOptions{
		Tag:            opts.tag,
		Channel:        opts.channel,
		Rollout:        opts.rollout,
		MinUpgradeFrom: opts.minUpgradeFrom,
		ReleaseNotes:   opts.releaseNotes,
		ReleaseURL:     releaseURL,
		PublishedAt:    publishedAt,
		Archives:       archives,
		Replace:        opts.replace,
	})
}

func run(fs afero.Fs, opts *options, now time.Time) (*result, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	m, publishedGen, err := loadCurrent(fs, opts.manifestPath)
	if err != nil {
		return nil, err
	}
	published, err := loadPublishedChecksums(fs, opts.checksumsPath)
	if err != nil {
		return nil, err
	}

	res := &result{}
	switch opts.mode {
	case modePromote:
		rel, promoteErr := applyPromote(fs, m, opts)
		if promoteErr != nil {
			return nil, promoteErr
		}
		res.tag, res.channel, res.rollout = rel.TagName, rel.Channel, rel.Rollout
		res.archives = len(archiveAssets(rel))
	case modeRollout:
		rel, rolloutErr := setRollout(m, opts.tag, opts.rollout)
		if rolloutErr != nil {
			return nil, rolloutErr
		}
		res.tag, res.channel, res.rollout = rel.TagName, rel.Channel, rel.Rollout
	case modeWithdraw:
		rel, withdrawErr := withdraw(m, opts.tag)
		if withdrawErr != nil {
			return nil, withdrawErr
		}
		res.tag, res.channel = rel.TagName, rel.Channel
	}

	backfillDigests(m, published)
	res.dropped = applyRetention(m, opts.retainStable, opts.retainBeta, opts.notesLimit)
	if err := requireDigests(m); err != nil {
		return nil, err
	}

	checksums := renderChecksums(checksumsFromManifest(m))
	setMetadataAssetSizes(m, int64(len(checksums)))

	generation := publishedGen + 1
	if opts.generationFloor > generation {
		generation = opts.generationFloor
	}
	if err := validateGeneration(publishedGen, generation); err != nil {
		return nil, err
	}
	stampManifest(m, generation, opts.keyID, now)

	if err := writeManifest(fs, m, filepath.Join(opts.outDir, manifestName)); err != nil {
		return nil, err
	}
	if err := afero.WriteFile(fs, filepath.Join(opts.outDir, checksumsName), checksums, 0o600); err != nil {
		return nil, fmt.Errorf("writing checksums: %w", err)
	}

	res.generation = m.Generation
	res.releases = len(m.Releases)
	res.checksumBytes = len(checksums)

	return res, nil
}

func writeSummary(fs afero.Fs, path, mode string, res *result) error {
	if path == "" {
		return nil
	}

	var b strings.Builder
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(&b, format, args...)
	}

	write("## Update manifest %s\n\n", mode)
	write("| Field | Value |\n|---|---|\n")
	if res.tag != "" {
		write("| Release | `%s` |\n", res.tag)
		write("| Channel | %s |\n", res.channel)
	}
	if mode == modePromote || mode == modeRollout {
		write("| Rollout | %d%% |\n", res.rollout)
	}
	if res.archives > 0 {
		write("| Archives | %d |\n", res.archives)
	}
	write("| Generation | %d |\n", res.generation)
	write("| Releases in manifest | %d |\n", res.releases)
	write("| checksums.txt size | %d bytes |\n", res.checksumBytes)
	if len(res.dropped) > 0 {
		sorted := make([]string, len(res.dropped))
		copy(sorted, res.dropped)
		sort.Strings(sorted)
		write("\nPruned by retention: `%s`\n", strings.Join(sorted, "`, `"))
	}

	if err := afero.WriteFile(fs, path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}
	return nil
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	opts := parseFlags()
	fs := afero.NewOsFs()

	res, err := run(fs, opts, time.Now())
	if err != nil {
		if errors.Is(err, errUsage) {
			flag.Usage()
		}
		log.Fatal().Err(err).Str("mode", opts.mode).Msg("update manifest generation failed")
	}

	if err := writeSummary(fs, opts.summaryPath, opts.mode, res); err != nil {
		log.Fatal().Err(err).Msg("writing summary failed")
	}

	event := log.Info().
		Str("mode", opts.mode).
		Int64("generation", res.generation).
		Int("releases", res.releases)
	if res.tag != "" {
		event = event.Str("tag", res.tag).Str("channel", res.channel)
	}
	if len(res.dropped) > 0 {
		event = event.Strs("pruned", res.dropped)
	}
	event.Msg("update manifest written")
}
