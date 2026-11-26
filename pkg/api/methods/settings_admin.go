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

package methods

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/hostfiles"
)

// Byte limits of the names Core advertises: one DNS label for mDNS, and what
// fits beside the service UUID in a BLE advertisement.
const (
	maxDiscoveryNameBytes = 63
	maxBLENameBytes       = 29
)

var readerDriverIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func soundSettingParam(p *models.AdminSettingsParams, s config.Sound) *models.SoundSettingParam {
	switch s {
	case config.SoundSuccess:
		return p.AudioSuccessSound
	case config.SoundFail:
		return p.AudioFailSound
	case config.SoundLimit:
		return p.AudioLimitSound
	case config.SoundPending:
		return p.AudioPendingSound
	case config.SoundReady:
		return p.AudioReadySound
	default:
		return nil
	}
}

func soundSettingResponse(cfg *config.Instance, s config.Sound) models.SoundSetting {
	value := cfg.SoundSetting(s)
	switch {
	case value == nil:
		return models.SoundSetting{Mode: models.SoundModeDefault}
	case *value == "":
		return models.SoundSetting{Mode: models.SoundModeOff}
	default:
		return models.SoundSetting{Mode: models.SoundModeFile, Path: *value}
	}
}

// redactServerURL drops any credentials written into a launcher's server
// address before it is shown.
func redactServerURL(raw string) string {
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

func publisherEnabled(enabled *bool) bool {
	return enabled == nil || *enabled
}

// adminSettingsResponse builds the settings only a local or admin client
// reads.
func adminSettingsResponse(env *requests.RequestEnv) *models.AdminSettings {
	cfg := env.Config
	scan := cfg.ReadersScan()

	drivers := cfg.ReaderDrivers()
	driverIDs := make([]string, 0, len(drivers))
	for id := range drivers {
		driverIDs = append(driverIDs, id)
	}
	sort.Strings(driverIDs)
	readerDrivers := make([]models.ReaderDriver, 0, len(driverIDs))
	for _, id := range driverIDs {
		d := drivers[id]
		readerDrivers = append(readerDrivers, models.ReaderDriver{
			ID:              id,
			Enabled:         d.Enabled,
			AutoDetect:      d.AutoDetect,
			KeepReading:     d.KeepReading,
			ScanMode:        d.ScanMode,
			LaunchOrder:     d.LaunchOrder,
			AudioCDLauncher: d.AudioCDLauncher,
		})
	}

	defaults := cfg.LauncherDefaults()
	launcherDefaults := make([]models.LauncherDefault, 0, len(defaults))
	for i := range defaults {
		d := &defaults[i]
		launcherDefaults = append(launcherDefaults, models.LauncherDefault{
			Launcher:         d.Launcher,
			RenderScale:      d.RenderScale,
			RenderResolution: d.RenderResolution,
			ScanDuplicates:   d.ScanDuplicates,
			Client:           d.Client,
			Action:           d.Action,
			LoadPath:         d.LoadPath,
			BeforeExit:       d.BeforeExit,
			InstallDir:       d.InstallDir,
			ServerURL:        redactServerURL(d.ServerURL),
		})
	}

	categories := cfg.SystemCategories()
	systemCategories := make([]models.SystemCategory, 0, len(categories))
	for _, c := range categories {
		systemCategories = append(systemCategories, models.SystemCategory{
			Name:    c.Name,
			Systems: append(make([]string, 0, len(c.Systems)), c.Systems...),
		})
	}

	mqtt := cfg.GetMQTTPublishers()
	publishersMQTT := make([]models.MQTTPublisherSetting, 0, len(mqtt))
	for i := range mqtt {
		publishersMQTT = append(publishersMQTT, models.MQTTPublisherSetting{
			Index:   i,
			Enabled: publisherEnabled(mqtt[i].Enabled),
			Broker:  config.RedactBroker(mqtt[i].Broker),
			Topic:   mqtt[i].Topic,
			Filter:  append(make([]string, 0, len(mqtt[i].Filter)), mqtt[i].Filter...),
		})
	}

	pixelcade := cfg.GetPixelCadePublishers()
	publishersPixelcade := make([]models.PixelcadePublisherSetting, 0, len(pixelcade))
	for i := range pixelcade {
		publishersPixelcade = append(publishersPixelcade, models.PixelcadePublisherSetting{
			Index:   i,
			Enabled: publisherEnabled(pixelcade[i].Enabled),
			Host:    pixelcade[i].Host,
			Port:    pixelcade[i].Port,
			Mode:    pixelcade[i].Mode,
			Filter:  append(make([]string, 0, len(pixelcade[i].Filter)), pixelcade[i].Filter...),
		})
	}

	return &models.AdminSettings{
		AudioSuccessSound:          soundSettingResponse(cfg, config.SoundSuccess),
		AudioFailSound:             soundSettingResponse(cfg, config.SoundFail),
		AudioLimitSound:            soundSettingResponse(cfg, config.SoundLimit),
		AudioPendingSound:          soundSettingResponse(cfg, config.SoundPending),
		AudioReadySound:            soundSettingResponse(cfg, config.SoundReady),
		InputGamepadEnabled:        cfg.VirtualGamepadSetting(),
		ReadersScanIgnoreOnConnect: scan.IgnoreOnConnect,
		ReadersScanAllowRelaunch:   scan.AllowRelaunch,
		ReadersScanOnScan:          scan.OnScan,
		ReadersScanOnRemove:        scan.OnRemove,
		ReadersScanHistory:         cfg.ScanHistory(),
		ReadersDrivers:             readerDrivers,
		LaunchersBeforeMediaStart:  cfg.LaunchersBeforeMediaStart(),
		LaunchersOnMediaStart:      cfg.LaunchersOnMediaStart(),
		LaunchersBeforeExit:        cfg.LaunchersBeforeExit(),
		LauncherDefaults:           launcherDefaults,
		SystemCategories:           systemCategories,
		MediaFilenameTags:          cfg.FilenameTags(),
		MediaDefaultRegions:        slices.Clone(cfg.DefaultRegions()),
		MediaDefaultLangs:          slices.Clone(cfg.DefaultLangs()),
		BackupScope:                cfg.BackupScope(),
		DiscoveryEnabled:           cfg.DiscoveryEnabled(),
		DiscoveryInstanceName:      cfg.DiscoveryInstanceName(),
		BLEName:                    cfg.BLEName(),
		InternetCheck:              cfg.InternetCheckEnabled(),
		ServiceOnBoot:              cfg.ServiceOnBoot(),
		ServiceOnReady:             cfg.ServiceOnReady(),
		PublishersMQTT:             publishersMQTT,
		PublishersPixelcade:        publishersPixelcade,
		DeviceID:                   cfg.DeviceID(),
		APIPort:                    cfg.APIPort(),
		APIListen:                  cfg.APIListen(),
	}
}

// adminSettingsPlan holds the admin fields of a settings update once they
// have been checked and converted, ready to apply.
type adminSettingsPlan struct {
	sounds           map[config.Sound]*string
	drivers          map[string]config.DriverConfig
	launcherDefaults []config.LaunchersDefault
	categories       []config.SystemsCategory
	regions          []string
	langs            []string
}

// prepareAdminSettings checks the admin fields that do not depend on the
// config file and converts them. It runs before anything is changed.
func prepareAdminSettings(env *requests.RequestEnv, p *models.AdminSettingsParams) (*adminSettingsPlan, error) {
	plan := &adminSettingsPlan{sounds: make(map[config.Sound]*string)}

	for _, s := range config.Sounds {
		param := soundSettingParam(p, s)
		if param == nil {
			continue
		}
		value, err := resolveSoundParam(env, s, param)
		if err != nil {
			return nil, err
		}
		plan.sounds[s] = value
	}

	if p.DiscoveryInstanceName != nil {
		if err := validateAdvertisedName("discoveryInstanceName", *p.DiscoveryInstanceName,
			maxDiscoveryNameBytes); err != nil {
			return nil, err
		}
	}
	if p.BLEName != nil {
		if err := validateAdvertisedName("bleName", *p.BLEName, maxBLENameBytes); err != nil {
			return nil, err
		}
	}

	if p.MediaDefaultRegions != nil {
		regions, err := canonicalTagList("mediaDefaultRegions", tags.TagTypeRegion, *p.MediaDefaultRegions)
		if err != nil {
			return nil, err
		}
		plan.regions = regions
	}
	if p.MediaDefaultLangs != nil {
		langs, err := canonicalTagList("mediaDefaultLangs", tags.TagTypeLang, *p.MediaDefaultLangs)
		if err != nil {
			return nil, err
		}
		plan.langs = langs
	}

	if p.ReadersDrivers != nil {
		drivers, err := buildReaderDrivers(*p.ReadersDrivers)
		if err != nil {
			return nil, err
		}
		plan.drivers = drivers
	}

	if p.LauncherDefaults != nil {
		defaults, err := buildLauncherDefaults(*p.LauncherDefaults)
		if err != nil {
			return nil, err
		}
		plan.launcherDefaults = defaults
	}

	if p.SystemCategories != nil {
		categories := make([]config.SystemsCategory, 0, len(*p.SystemCategories))
		for _, c := range *p.SystemCategories {
			categories = append(categories, config.SystemsCategory{
				Name:    c.Name,
				Systems: slices.Clone(c.Systems),
			})
		}
		if err := config.ValidateSystemCategories(categories); err != nil {
			return nil, models.ClientErrf("invalid params: systemCategories: %w", err)
		}
		plan.categories = categories
	}

	return plan, nil
}

// resolveSoundParam turns a sound parameter into the value config.toml holds.
// A file is checked to exist and be audio, so a typo is refused now rather
// than found as silence on the next scan.
func resolveSoundParam(env *requests.RequestEnv, s config.Sound, p *models.SoundSettingParam) (*string, error) {
	switch p.Mode {
	case models.SoundModeDefault:
		if p.Root != "" || p.Path != "" {
			return nil, models.ClientErrf("invalid params: %s sound: mode %q takes no file", s, p.Mode)
		}
		return nil, nil //nolint:nilnil // an unset value is how config.toml holds the built-in sound
	case models.SoundModeOff:
		if p.Root != "" || p.Path != "" {
			return nil, models.ClientErrf("invalid params: %s sound: mode %q takes no file", s, p.Mode)
		}
		off := ""
		return &off, nil
	case models.SoundModeFile:
	default:
		return nil, models.ClientErrf("invalid params: %s sound: unknown mode %q", s, p.Mode)
	}

	if p.Path == "" {
		return nil, models.ClientErrf("invalid params: %s sound: a file needs a path", s)
	}
	if p.Root != "" {
		browser, err := audioBrowser(env)
		if err != nil {
			return nil, err
		}
		resolved, err := browser.ResolveAudio(env.Context, p.Root, p.Path)
		if err != nil {
			return nil, fmt.Errorf("%s sound: %w", s, hostFilesClientError(err))
		}
		return &resolved, nil
	}

	if !filepath.IsAbs(p.Path) {
		return nil, models.ClientErrf(
			"invalid params: %s sound: path must be absolute, or name a root from files.browse", s,
		)
	}
	clean := filepath.Clean(p.Path)
	info, err := os.Stat(clean)
	if err != nil {
		return nil, models.ClientErrf("invalid params: %s sound: file not found", s)
	}
	if !info.Mode().IsRegular() || !hostfiles.IsAudioName(clean) {
		return nil, models.ClientErrf("invalid params: %s sound: not an audio file", s)
	}
	return &clean, nil
}

// validateAdvertisedName refuses a name other devices would show garbled or
// that does not fit where it is advertised. Empty means the default name.
func validateAdvertisedName(field, name string, maxBytes int) error {
	if name == "" {
		return nil
	}
	if !utf8.ValidString(name) {
		return models.ClientErrf("invalid params: %s must be valid UTF-8", field)
	}
	if len(name) > maxBytes {
		return models.ClientErrf("invalid params: %s must be at most %d bytes", field, maxBytes)
	}
	if strings.TrimSpace(name) != name {
		return models.ClientErrf("invalid params: %s must not start or end with a space", field)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return models.ClientErrf("invalid params: %s must not hold control characters", field)
		}
	}
	return nil
}

// canonicalTagList lowercases a region or language list and refuses a value
// the tag set does not define, or one listed twice.
func canonicalTagList(field string, tagType tags.TagType, in []string) ([]string, error) {
	known := tags.CanonicalTagDefinitions[tagType]
	out := make([]string, 0, len(in))
	for _, v := range in {
		value := strings.ToLower(strings.TrimSpace(v))
		if !slices.Contains(known, tags.TagValue(value)) {
			return nil, models.ClientErrf("invalid params: %s: unknown value %q", field, v)
		}
		if slices.Contains(out, value) {
			return nil, models.ClientErrf("invalid params: %s: %q is listed twice", field, v)
		}
		out = append(out, value)
	}
	return out, nil
}

func buildReaderDrivers(in []models.ReaderDriver) (map[string]config.DriverConfig, error) {
	out := make(map[string]config.DriverConfig, len(in))
	for _, d := range in {
		if !readerDriverIDPattern.MatchString(d.ID) {
			return nil, models.ClientErrf("invalid params: readersDrivers: invalid driver id %q", d.ID)
		}
		if _, dup := out[d.ID]; dup {
			return nil, models.ClientErrf("invalid params: readersDrivers: driver %q is listed twice", d.ID)
		}
		scanMode := config.NormalizeScanMode(d.ScanMode)
		if d.ScanMode != "" && scanMode == "" {
			return nil, models.ClientErrf(
				"invalid params: readersDrivers: scanMode %q for driver %q must be %q or %q",
				d.ScanMode, d.ID, config.ScanModeTap, config.ScanModeHold,
			)
		}
		out[d.ID] = config.DriverConfig{
			Enabled:         d.Enabled,
			AutoDetect:      d.AutoDetect,
			KeepReading:     d.KeepReading,
			ScanMode:        scanMode,
			LaunchOrder:     d.LaunchOrder,
			AudioCDLauncher: d.AudioCDLauncher,
		}
	}
	return out, nil
}

// validLoadPath refuses a load path that could name a file outside the place
// a launcher looks for it.
func validLoadPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.ContainsAny(p, "\\\x00") || path.IsAbs(p) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return false
	}
	return !slices.Contains(strings.Split(p, "/"), "..")
}

func buildLauncherDefaults(in []models.LauncherDefaultParam) ([]config.LaunchersDefault, error) {
	out := make([]config.LaunchersDefault, 0, len(in))
	for i := range in {
		d := &in[i]
		if !validLoadPath(d.LoadPath) {
			return nil, models.ClientErrf("invalid params: launcherDefaults: invalid loadPath for %q", d.Launcher)
		}
		out = append(out, config.LaunchersDefault{
			Launcher:         d.Launcher,
			RenderScale:      d.RenderScale,
			RenderResolution: d.RenderResolution,
			ScanDuplicates:   d.ScanDuplicates,
			Client:           d.Client,
			Action:           d.Action,
			LoadPath:         d.LoadPath,
			BeforeExit:       d.BeforeExit,
		})
	}
	if err := config.ValidateLauncherDefaults(out); err != nil {
		return nil, models.ClientErrf("invalid params: launcherDefaults: %w", err)
	}
	return out, nil
}

// hookChange is a ZapScript hook a settings update writes.
type hookChange struct {
	field    string
	value    string
	previous []string
}

func (h hookChange) changed() bool {
	return h.value != "" && !slices.Contains(h.previous, h.value)
}

// settingsHookChanges lists every hook the update writes, with the values the
// same hook holds now.
func settingsHookChanges(cfg *config.Instance, params *models.UpdateSettingsParams) []hookChange {
	var hooks []hookChange
	add := func(field string, value *string, current string) {
		if value != nil {
			hooks = append(hooks, hookChange{field: field, value: *value, previous: []string{current}})
		}
	}
	p := &params.AdminSettingsParams
	scan := cfg.ReadersScan()
	add("readersScanOnScan", p.ReadersScanOnScan, scan.OnScan)
	add("readersScanOnRemove", p.ReadersScanOnRemove, scan.OnRemove)
	add("launchersBeforeMediaStart", p.LaunchersBeforeMediaStart, cfg.LaunchersBeforeMediaStart())
	add("launchersOnMediaStart", p.LaunchersOnMediaStart, cfg.LaunchersOnMediaStart())
	add("launchersBeforeExit", p.LaunchersBeforeExit, cfg.LaunchersBeforeExit())
	add("serviceOnBoot", p.ServiceOnBoot, cfg.ServiceOnBoot())
	add("serviceOnReady", p.ServiceOnReady, cfg.ServiceOnReady())

	if p.LauncherDefaults != nil {
		existing := cfg.LauncherDefaults()
		for _, d := range *p.LauncherDefaults {
			var previous []string
			for i := range existing {
				if strings.EqualFold(existing[i].Launcher, d.Launcher) {
					previous = append(previous, existing[i].BeforeExit)
				}
			}
			hooks = append(hooks, hookChange{field: "launcherDefaults", value: d.BeforeExit, previous: previous})
		}
	}

	if params.SystemDefaults != nil {
		existing := cfg.SystemDefaults()
		for _, d := range *params.SystemDefaults {
			var previous []string
			system, err := systemdefs.LookupSystem(d.System)
			for i := range existing {
				if err != nil {
					break
				}
				other, otherErr := systemdefs.LookupSystem(existing[i].System)
				if otherErr == nil && other.ID == system.ID {
					previous = append(previous, existing[i].BeforeExit)
				}
			}
			hooks = append(hooks, hookChange{field: "systemDefaults", value: d.BeforeExit, previous: previous})
		}
	}
	return hooks
}

// checkSettingsHooks holds a hook to the bar a script run from the network
// meets: a hook runs with the trust of Core itself, so only a local or admin
// client may change one, and a remote one only to a script service.allow_run
// permits when that list is set.
func checkSettingsHooks(env *requests.RequestEnv, params *models.UpdateSettingsParams) error {
	for _, h := range settingsHookChanges(env.Config, params) {
		if !h.changed() {
			continue
		}
		if !isLocalOrAdmin(env) {
			return models.ClientErrf("%s: changing a hook requires a local or admin client: %w", h.field, ErrForbidden)
		}
		if !env.IsLocal && env.Config.HasAllowRun() && !env.Config.IsRunAllowed(h.value) {
			return models.ClientErrf("%s: script is not permitted by service.allow_run: %w", h.field, ErrForbidden)
		}
	}
	return nil
}

// checkAdminSettingsAgainstConfig checks the admin fields that depend on the
// config just read back from disk. It runs before anything is changed.
func checkAdminSettingsAgainstConfig(
	env *requests.RequestEnv,
	p *models.AdminSettingsParams,
	plan *adminSettingsPlan,
) error {
	if p.PublishersMQTT != nil {
		if err := checkPublisherUpdates("publishersMqtt", *p.PublishersMQTT,
			len(env.Config.GetMQTTPublishers()), false); err != nil {
			return err
		}
	}
	if p.PublishersPixelcade != nil {
		if err := checkPublisherUpdates("publishersPixelcade", *p.PublishersPixelcade,
			len(env.Config.GetPixelCadePublishers()), true); err != nil {
			return err
		}
	}

	if plan.drivers != nil {
		existing := env.Config.ReaderDrivers()
		for id, d := range plan.drivers {
			if d.AudioCDLauncher == "" || strings.EqualFold(existing[id].AudioCDLauncher, d.AudioCDLauncher) {
				continue
			}
			if err := requireLauncherRef(env, d.AudioCDLauncher); err != nil {
				return fmt.Errorf("readersDrivers %q audioCdLauncher: %w", id, err)
			}
		}
	}

	if plan.launcherDefaults != nil {
		existing := env.Config.LauncherDefaults()
		for i := range plan.launcherDefaults {
			ref := plan.launcherDefaults[i].Launcher
			if slices.ContainsFunc(existing, func(e config.LaunchersDefault) bool {
				return strings.EqualFold(e.Launcher, ref)
			}) {
				continue
			}
			if err := requireLauncherRef(env, ref); err != nil {
				return fmt.Errorf("launcherDefaults: %w", err)
			}
		}
	}
	return nil
}

func requireLauncherRef(env *requests.RequestEnv, ref string) error {
	if env.LauncherCache == nil {
		return errors.New("launcher cache unavailable, cannot validate launcher reference")
	}
	if !launcherRefExists(env.LauncherCache, ref) {
		return models.ClientErrf("unknown launcher %q", ref)
	}
	return nil
}

func checkPublisherUpdates(field string, updates []models.PublisherUpdate, count int, modes bool) error {
	seen := make(map[int]bool, len(updates))
	for _, u := range updates {
		if u.Index == nil {
			return models.ClientErrf("invalid params: %s: index is required", field)
		}
		index := *u.Index
		if index < 0 || index >= count {
			return models.ClientErrf("invalid params: %s: no publisher at index %d", field, index)
		}
		if seen[index] {
			return models.ClientErrf("invalid params: %s: index %d is listed twice", field, index)
		}
		seen[index] = true
		if u.Mode != nil && !modes {
			return models.ClientErrf("invalid params: %s: mode is not a setting of this publisher", field)
		}
	}
	return nil
}

// adminSettingsChanges records what applying the admin fields changed, for
// the follow-up work and the result.
type adminSettingsChanges struct {
	needs            map[string][]string
	kinds            []config.ChangeKind
	soundsChanged    bool
	launchersChanged bool
}

func (c *adminSettingsChanges) need(field, what string) {
	c.needs[field] = append(c.needs[field], what)
}

// carryLauncherInstallSettings keeps the install_dir and server_url no client
// can write. The k-th entry for a launcher takes them from the k-th entry for
// the same launcher it replaces.
func carryLauncherInstallSettings(existing, updated []config.LaunchersDefault) {
	used := make(map[int]bool, len(existing))
	for i := range updated {
		for j := range existing {
			if used[j] || !strings.EqualFold(existing[j].Launcher, updated[i].Launcher) {
				continue
			}
			used[j] = true
			updated[i].InstallDir = existing[j].InstallDir
			updated[i].ServerURL = existing[j].ServerURL
			break
		}
	}
}

// scanDuplicatesChanged reports whether an entry's scan_duplicates was set,
// cleared or changed. Removing an explicit false can expose a group entry's
// true, so a key going away counts as a change even when it held false.
func scanDuplicatesChanged(existing, updated []config.LaunchersDefault) bool {
	resolve := func(list []config.LaunchersDefault) map[string]bool {
		out := make(map[string]bool)
		for i := range list {
			if list[i].ScanDuplicates != nil {
				out[strings.ToLower(list[i].Launcher)] = *list[i].ScanDuplicates
			}
		}
		return out
	}
	return !maps.Equal(resolve(existing), resolve(updated))
}

func clonePublisherFilter(f []string) []string {
	if f == nil {
		return nil
	}
	return slices.Clone(f)
}

// applyAdminSettings writes the admin fields into the in-memory config. It
// runs after every check, so a refusal leaves nothing half applied.
func applyAdminSettings(
	env *requests.RequestEnv,
	p *models.AdminSettingsParams,
	plan *adminSettingsPlan,
) (*adminSettingsChanges, error) {
	cfg := env.Config
	changes := &adminSettingsChanges{needs: make(map[string][]string)}

	for _, s := range config.Sounds {
		value, ok := plan.sounds[s]
		if !ok {
			continue
		}
		previous := cfg.SoundSetting(s)
		if err := cfg.SetSoundSetting(s, value); err != nil {
			return nil, fmt.Errorf("setting %s sound: %w", s, err)
		}
		if (previous == nil) != (value == nil) || (previous != nil && *previous != *value) {
			changes.soundsChanged = true
		}
	}

	if p.InputGamepadEnabled.Sent {
		if p.InputGamepadEnabled.Value == nil {
			cfg.ClearVirtualGamepadSetting()
		} else {
			cfg.SetVirtualGamepadEnabled(*p.InputGamepadEnabled.Value)
		}
	}

	if p.ReadersScanIgnoreOnConnect != nil {
		if *p.ReadersScanIgnoreOnConnect && !cfg.ScanIgnoreOnConnect() {
			changes.kinds = append(changes.kinds, config.ChangeIgnoreOnConnect)
		}
		cfg.SetScanIgnoreOnConnect(*p.ReadersScanIgnoreOnConnect)
	}
	if p.ReadersScanAllowRelaunch != nil {
		cfg.SetScanAllowRelaunch(*p.ReadersScanAllowRelaunch)
	}
	if p.ReadersScanOnScan != nil {
		cfg.SetScanOnScan(*p.ReadersScanOnScan)
	}
	if p.ReadersScanOnRemove != nil {
		cfg.SetScanOnRemove(*p.ReadersScanOnRemove)
	}
	if p.ReadersScanHistory != nil {
		if cfg.ScanHistory() != *p.ReadersScanHistory {
			changes.kinds = append(changes.kinds, config.ChangeScanHistory)
		}
		cfg.SetScanHistory(*p.ReadersScanHistory)
	}
	if plan.drivers != nil {
		cfg.SetReaderDrivers(plan.drivers)
	}

	if p.LaunchersBeforeMediaStart != nil {
		cfg.SetLaunchersBeforeMediaStart(*p.LaunchersBeforeMediaStart)
	}
	if p.LaunchersOnMediaStart != nil {
		cfg.SetLaunchersOnMediaStart(*p.LaunchersOnMediaStart)
	}
	if p.LaunchersBeforeExit != nil {
		cfg.SetLaunchersBeforeExit(*p.LaunchersBeforeExit)
	}
	if plan.launcherDefaults != nil {
		existing := cfg.LauncherDefaults()
		carryLauncherInstallSettings(existing, plan.launcherDefaults)
		if err := cfg.SetLauncherDefaults(plan.launcherDefaults); err != nil {
			return nil, models.ClientErrf("invalid params: launcherDefaults: %w", err)
		}
		if scanDuplicatesChanged(existing, plan.launcherDefaults) {
			changes.need("launcherDefaults", models.SettingNeedsReindex)
		}
		changes.launchersChanged = true
	}

	if plan.categories != nil {
		if err := cfg.SetSystemCategories(plan.categories); err != nil {
			return nil, models.ClientErrf("invalid params: systemCategories: %w", err)
		}
	}

	if p.MediaFilenameTags != nil {
		if cfg.FilenameTags() != *p.MediaFilenameTags {
			changes.need("mediaFilenameTags", models.SettingNeedsReindex)
		}
		cfg.SetFilenameTags(*p.MediaFilenameTags)
	}
	if plan.regions != nil {
		previous := cfg.DefaultRegions()
		cfg.SetDefaultRegions(plan.regions)
		if !slices.Equal(previous, cfg.DefaultRegions()) {
			changes.need("mediaDefaultRegions", models.SettingNeedsRescrape)
		}
	}
	if plan.langs != nil {
		previous := cfg.DefaultLangs()
		cfg.SetDefaultLangs(plan.langs)
		if !slices.Equal(previous, cfg.DefaultLangs()) {
			changes.need("mediaDefaultLangs", models.SettingNeedsRescrape)
		}
	}

	if p.BackupScope != nil {
		cfg.SetBackupScope(*p.BackupScope)
	}

	discoveryChanged, bleNameChanged := false, false
	if p.DiscoveryEnabled != nil {
		discoveryChanged = cfg.DiscoveryEnabled() != *p.DiscoveryEnabled
		cfg.SetDiscoveryEnabled(*p.DiscoveryEnabled)
	}
	if p.DiscoveryInstanceName != nil && cfg.DiscoveryInstanceName() != *p.DiscoveryInstanceName {
		discoveryChanged, bleNameChanged = true, true
		cfg.SetDiscoveryInstanceName(*p.DiscoveryInstanceName)
	}
	if p.BLEName != nil && cfg.BLEName() != *p.BLEName {
		bleNameChanged = true
		cfg.SetBLEName(*p.BLEName)
	}
	if discoveryChanged {
		changes.kinds = append(changes.kinds, config.ChangeDiscovery)
	}
	if bleNameChanged {
		changes.kinds = append(changes.kinds, config.ChangeBLEName)
	}

	if p.InternetCheck != nil {
		cfg.SetInternetCheck(*p.InternetCheck)
	}
	if p.ServiceOnBoot != nil {
		cfg.SetServiceOnBoot(*p.ServiceOnBoot)
	}
	if p.ServiceOnReady != nil {
		cfg.SetServiceOnReady(*p.ServiceOnReady)
	}

	if p.PublishersMQTT != nil || p.PublishersPixelcade != nil {
		applyPublisherUpdates(cfg, p)
		changes.kinds = append(changes.kinds, config.ChangePublishers)
	}

	return changes, nil
}

func applyPublisherUpdates(cfg *config.Instance, p *models.AdminSettingsParams) {
	if p.PublishersMQTT != nil {
		current := cfg.GetMQTTPublishers()
		pubs := make([]config.MQTTPublisher, len(current))
		for i := range current {
			pubs[i] = current[i]
			pubs[i].Filter = clonePublisherFilter(current[i].Filter)
		}
		for _, u := range *p.PublishersMQTT {
			if u.Enabled != nil {
				enabled := *u.Enabled
				pubs[*u.Index].Enabled = &enabled
			}
			if u.Filter != nil {
				pubs[*u.Index].Filter = clonePublisherFilter(*u.Filter)
			}
		}
		cfg.SetMQTTPublishers(pubs)
	}
	if p.PublishersPixelcade != nil {
		current := cfg.GetPixelCadePublishers()
		pubs := make([]config.PixelCadePublisher, len(current))
		for i := range current {
			pubs[i] = current[i]
			pubs[i].Filter = clonePublisherFilter(current[i].Filter)
		}
		for _, u := range *p.PublishersPixelcade {
			if u.Enabled != nil {
				enabled := *u.Enabled
				pubs[*u.Index].Enabled = &enabled
			}
			if u.Filter != nil {
				pubs[*u.Index].Filter = clonePublisherFilter(*u.Filter)
			}
			if u.Mode != nil {
				pubs[*u.Index].Mode = *u.Mode
			}
		}
		cfg.SetPixelCadePublishers(pubs)
	}
}

// settingsUpdateResult names every field the request set with what it still
// needs to take effect.
func settingsUpdateResult(
	params *models.UpdateSettingsParams,
	restartRequired []string,
	needs map[string][]string,
) models.UpdateSettingsResult {
	applied := make(map[string][]string)
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		t := v.Type()
		for i := range t.NumField() {
			field, value := t.Field(i), v.Field(i)
			if field.Anonymous && value.Kind() == reflect.Struct {
				walk(value)
				continue
			}
			if !models.ParamSent(value) {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			list := append([]string{}, needs[name]...)
			if slices.Contains(restartRequired, name) {
				list = append(list, models.SettingNeedsRestart)
			}
			applied[name] = list
		}
	}
	walk(reflect.ValueOf(params).Elem())
	return models.UpdateSettingsResult{Applied: applied}
}
