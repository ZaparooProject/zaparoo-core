//go:build linux || darwin

/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/rs/zerolog/log"
)

type ServiceEntry func() (*service.StartResult, error)

type Service struct {
	pl     platforms.Platform
	cfg    *config.Instance
	start  ServiceEntry
	stop   func() error
	done   <-chan struct{}
	daemon bool
}

type ServiceArgs struct {
	Platform platforms.Platform
	Config   *config.Instance
	Entry    ServiceEntry
	NoDaemon bool
}

type processWaitFunc func(time.Duration) error

type commandWaiter struct {
	cmd  *exec.Cmd
	done chan error
	once sync.Once
}

type serviceBinaryManifest struct {
	SourcePath          string `json:"sourcePath"`
	SourceHash          string `json:"sourceHash"`
	ServicePath         string `json:"servicePath"`
	SourceSize          int64  `json:"sourceSize"`
	SourceModTimeNS     int64  `json:"sourceModTimeNs"`
	SourceChangeTimeNS  int64  `json:"sourceChangeTimeNs"`
	ServiceSize         int64  `json:"serviceSize"`
	ServiceModTimeNS    int64  `json:"serviceModTimeNs"`
	ServiceChangeTimeNS int64  `json:"serviceChangeTimeNs"`
}

type restartExecConfig struct {
	serviceBin string
	binPath    string
	args       []string
	env        []string
}

const (
	serviceStopTimeout        = 10 * time.Second
	serviceKillTimeout        = 3 * time.Second
	serviceStopPollInterval   = 100 * time.Millisecond
	servicePortReleaseTimeout = 3 * time.Second
	daemonReadyTimeout        = 3 * time.Second
	serviceManifestName       = "service_manifest.json"
	serviceHashLength         = 16
	serviceCopiesToKeep       = 2
)

func NewService(args ServiceArgs) (*Service, error) {
	err := os.MkdirAll(args.Platform.Settings().TempDir, 0o750)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Service{
		daemon: !args.NoDaemon,
		cfg:    args.Config,
		start:  args.Entry,
		pl:     args.Platform,
	}, nil
}

// Create new PID file using current process PID.
func (s *Service) createPidFile() error {
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	pid := os.Getpid()
	//nolint:gosec // PID path is derived from the configured temp directory and fixed filename.
	file, err := os.OpenFile(
		pidPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("failed to create PID file: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = file.WriteString(strconv.Itoa(pid))
	if err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

func (s *Service) removePidFile() error {
	err := os.Remove(filepath.Join(s.pl.Settings().TempDir, config.PidFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// Pid returns the process ID of the current running service daemon.
func (s *Service) Pid() (int, error) {
	pid := 0
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)

	info, err := os.Lstat(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pid, nil
		}
		return pid, fmt.Errorf("error checking pid file: %w", err)
	}
	if validateErr := validatePIDFileInfo(info); validateErr != nil {
		return pid, validateErr
	}

	//nolint:gosec // Safe: PID file path is validated before reading
	pidFile, err := os.ReadFile(pidPath)
	if err != nil {
		return pid, fmt.Errorf("error reading pid file: %w", err)
	}

	pidInt, err := strconv.Atoi(string(pidFile))
	if err != nil {
		return pid, fmt.Errorf("error parsing pid: %w", err)
	}

	pid = pidInt
	return pid, nil
}

func validatePIDFileInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("pid file is a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("pid file is not a regular file")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("pid file is group or world writable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && int64(stat.Uid) != int64(os.Geteuid()) {
		return fmt.Errorf("pid file is owned by uid %d, expected %d", stat.Uid, os.Geteuid())
	}
	return nil
}

func servicePIDConflictError(pid int) error {
	return fmt.Errorf(
		"service PID file points to live process %d that does not match the Zaparoo service binary",
		pid,
	)
}

// Running returns true if the service is running.
func (s *Service) Running() (bool, error) {
	pid, err := s.Pid()
	if err != nil {
		return false, fmt.Errorf("error reading service PID: %w", err)
	}

	if pid == 0 {
		return false, nil
	}

	if pidRunning(pid) {
		if s.pidMatchesService(pid) {
			return true, nil
		}
		log.Warn().
			Int("pid", pid).
			Msg("service PID file points to live process that does not match service binary")
		return false, servicePIDConflictError(pid)
	}

	if rmErr := s.removePidFile(); rmErr != nil {
		log.Debug().Err(rmErr).Int("pid", pid).Msg("failed to remove stale service PID file")
	}
	return false, nil
}

func (s *Service) stopService() error {
	log.Info().Msgf("stopping service")

	err := s.stop()
	if err != nil {
		log.Error().Err(err).Msg("error stopping service")
		return err
	}

	err = s.removePidFile()
	if err != nil {
		log.Error().Err(err).Msgf("error removing pid file")
		return err
	}

	return nil
}

// prepareBinary keeps a persistent service binary cache in DataDir so the
// package-manager target can be replaced while the daemon is running.
func (s *Service) prepareBinary(binPath string) (string, error) {
	started := time.Now()
	dataDir := helpers.DataDir(s.pl)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("error creating data directory: %w", err)
	}

	sourceInfo, err := os.Stat(binPath) //nolint:gosec // G703: binPath from os.Executable()/ZAPAROO_APP.
	if err != nil {
		return "", fmt.Errorf("error statting binary: %w", err)
	}
	sourceChangeTimeNS := fileChangeTimeNS(sourceInfo)

	manifest, manifestErr := readServiceBinaryManifest(dataDir)
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		log.Debug().Err(manifestErr).Msg("error reading service binary manifest")
	}
	if manifest != nil && manifest.matchesSourceMetadata(binPath, sourceInfo, sourceChangeTimeNS) {
		servicePath := serviceCachePath(dataDir, binPath, manifest.SourceHash)
		if filepath.Clean(manifest.ServicePath) == filepath.Clean(servicePath) &&
			serviceCachePathValid(manifest.ServicePath, dataDir) {
			cachedInfo, statErr := os.Stat(manifest.ServicePath)
			switch {
			case statErr == nil && manifest.matchesServiceMetadata(cachedInfo):
				log.Debug().
					Str("path", manifest.ServicePath).
					Dur("duration", time.Since(started)).
					Msg("using cached service binary from manifest")
				s.cleanupServiceBinaries(manifest.ServicePath, "")
				return manifest.ServicePath, nil
			case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
				log.Debug().Err(statErr).Str("path", manifest.ServicePath).Msg("error checking cached service binary")
			case statErr == nil:
				log.Debug().
					Str("path", manifest.ServicePath).
					Int64("cachedSize", cachedInfo.Size()).
					Int64("cachedModTimeNs", cachedInfo.ModTime().UnixNano()).
					Msg("cached service binary metadata mismatch")
			}
		}
	}

	hashStarted := time.Now()
	sourceHash, err := hashFileSHA256(binPath)
	if err != nil {
		return "", err
	}
	log.Debug().
		Dur("duration", time.Since(hashStarted)).
		Str("hash", shortServiceHash(sourceHash)).
		Msg("hashed service source binary")
	servicePath := serviceCachePath(dataDir, binPath, sourceHash)

	if _, statErr := os.Stat(servicePath); statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("error checking service binary: %w", statErr)
		}
		copyErr := copyServiceBinary(binPath, servicePath)
		if copyErr != nil {
			return "", copyErr
		}
	} else {
		cachedHash, hashErr := hashFileSHA256(servicePath)
		if hashErr != nil {
			return "", hashErr
		}
		if cachedHash != sourceHash {
			copyErr := copyServiceBinary(binPath, servicePath)
			if copyErr != nil {
				return "", copyErr
			}
		} else {
			log.Debug().Str("path", servicePath).Msg("using existing hashed service binary")
		}
	}

	serviceInfo, err := os.Stat(servicePath)
	if err != nil {
		return "", fmt.Errorf("error statting service binary: %w", err)
	}

	newManifest := serviceBinaryManifest{
		SourcePath:          binPath,
		SourceSize:          sourceInfo.Size(),
		SourceModTimeNS:     sourceInfo.ModTime().UnixNano(),
		SourceChangeTimeNS:  sourceChangeTimeNS,
		SourceHash:          sourceHash,
		ServicePath:         servicePath,
		ServiceSize:         serviceInfo.Size(),
		ServiceModTimeNS:    serviceInfo.ModTime().UnixNano(),
		ServiceChangeTimeNS: fileChangeTimeNS(serviceInfo),
	}
	manifestWriteErr := writeServiceBinaryManifest(dataDir, &newManifest)
	if manifestWriteErr != nil {
		return "", manifestWriteErr
	}

	previousPath := ""
	if manifest != nil {
		previousPath = manifest.ServicePath
	}
	s.cleanupServiceBinaries(servicePath, previousPath)

	log.Debug().Str("path", servicePath).Dur("duration", time.Since(started)).Msg("prepared service binary")
	return servicePath, nil
}

func copyServiceBinary(binPath, servicePath string) error {
	ext := filepath.Ext(binPath)
	name := strings.TrimSuffix(filepath.Base(binPath), ext)

	//nolint:gosec // G304: binPath from os.Executable()
	binFile, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("error opening binary: %w", err)
	}
	defer func() { _ = binFile.Close() }()

	tmpFile, err := os.CreateTemp(filepath.Dir(servicePath), name+".*.tmp")
	if err != nil {
		return fmt.Errorf("error creating temporary service binary: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	defer func() { _ = tmpFile.Close() }()

	chmodErr := tmpFile.Chmod(0o700)
	if chmodErr != nil {
		return fmt.Errorf("error setting service binary permissions: %w", chmodErr)
	}

	_, err = io.Copy(tmpFile, binFile)
	if err != nil {
		return fmt.Errorf("error copying binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("error closing service binary: %w", err)
	}
	if err := os.Rename(tmpPath, servicePath); err != nil {
		return fmt.Errorf("error replacing service binary: %w", err)
	}

	return nil
}

// cleanupServiceBinary is retained for older call paths. Cached service
// binaries are pruned by prepareBinary so normal restarts can reuse them.
func (*Service) cleanupServiceBinary() {
}

func readServiceBinaryManifest(dataDir string) (*serviceBinaryManifest, error) {
	path := filepath.Join(dataDir, serviceManifestName)
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is internal DataDir manifest.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("error reading service binary manifest: %w", err)
	}

	var manifest serviceBinaryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("error parsing service binary manifest: %w", err)
	}
	return &manifest, nil
}

func writeServiceBinaryManifest(dataDir string, manifest *serviceBinaryManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("error encoding service binary manifest: %w", err)
	}
	data = append(data, '\n')

	manifestPath := filepath.Join(dataDir, serviceManifestName)
	tmpFile, err := os.CreateTemp(dataDir, serviceManifestName+".*.tmp")
	if err != nil {
		return fmt.Errorf("error creating temporary service binary manifest: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("error writing service binary manifest: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("error closing service binary manifest: %w", err)
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return fmt.Errorf("error replacing service binary manifest: %w", err)
	}
	return nil
}

func (m *serviceBinaryManifest) matchesSourceMetadata(binPath string, info os.FileInfo, changeTimeNS int64) bool {
	return m.SourcePath == binPath &&
		m.SourceSize == info.Size() &&
		m.SourceModTimeNS == info.ModTime().UnixNano() &&
		m.SourceChangeTimeNS != 0 &&
		m.SourceChangeTimeNS == changeTimeNS &&
		m.SourceHash != "" &&
		m.ServicePath != ""
}

func (m *serviceBinaryManifest) matchesServiceMetadata(info os.FileInfo) bool {
	return m.ServiceSize == info.Size() &&
		m.ServiceModTimeNS == info.ModTime().UnixNano() &&
		m.ServiceChangeTimeNS != 0 &&
		m.ServiceChangeTimeNS == fileChangeTimeNS(info)
}

func fileChangeTimeNS(info os.FileInfo) int64 {
	if info == nil || info.Sys() == nil {
		return 0
	}

	sys := reflect.ValueOf(info.Sys())
	if sys.Kind() == reflect.Pointer {
		sys = sys.Elem()
	}
	if !sys.IsValid() || sys.Kind() != reflect.Struct {
		return 0
	}

	for _, name := range []string{"Ctim", "Ctimespec"} {
		if ns := timespecFieldNS(sys.FieldByName(name)); ns != 0 {
			return ns
		}
	}
	return 0
}

func timespecFieldNS(field reflect.Value) int64 {
	if !field.IsValid() || field.Kind() != reflect.Struct {
		return 0
	}
	sec := field.FieldByName("Sec")
	nsec := field.FieldByName("Nsec")
	if !sec.IsValid() || !nsec.IsValid() || !sec.CanInt() || !nsec.CanInt() {
		return 0
	}
	return sec.Int()*int64(time.Second) + nsec.Int()
}

func serviceCachePath(dataDir, binPath, sourceHash string) string {
	ext := filepath.Ext(binPath)
	return filepath.Join(dataDir, "zaparoo."+shortServiceHash(sourceHash)+ext)
}

func shortServiceHash(sourceHash string) string {
	if len(sourceHash) <= serviceHashLength {
		return sourceHash
	}
	return sourceHash[:serviceHashLength]
}

func hashFileSHA256(path string) (string, error) {
	//nolint:gosec // G304: path from os.Executable()/ZAPAROO_APP.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error opening binary for hash: %w", err)
	}
	defer func() { _ = file.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("error hashing binary: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func serviceCachePathValid(path, dataDir string) bool {
	if path == "" {
		return false
	}
	cleanDataDir := filepath.Clean(dataDir)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(cleanDataDir, cleanPath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return false
	}
	return filepath.Dir(rel) == "." && isServiceCacheFilename(filepath.Base(rel))
}

func isServiceCacheFilename(name string) bool {
	if name == "" || strings.Contains(name, string(filepath.Separator)) {
		return false
	}
	if name == "zaparoo.service" || name == "zaparoo.service.sh" {
		return true
	}

	const prefix = "zaparoo."
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	hash := strings.TrimPrefix(name, prefix)
	if strings.HasSuffix(hash, ".sh") {
		hash = strings.TrimSuffix(hash, ".sh")
	} else if strings.Contains(hash, ".") {
		return false
	}
	if len(hash) != serviceHashLength {
		return false
	}
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Service) cleanupServiceBinaries(currentPath, previousPath string) {
	dataDir := helpers.DataDir(s.pl)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		log.Debug().Err(err).Msg("error reading service binary cache directory")
		return
	}

	keep := map[string]struct{}{}
	for _, path := range []string{currentPath, previousPath} {
		if serviceCachePathValid(path, dataDir) {
			keep[filepath.Clean(path)] = struct{}{}
		}
	}

	candidates := make([]os.FileInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !isServiceCacheFilename(entry.Name()) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			log.Debug().Err(infoErr).Str("name", entry.Name()).Msg("error checking service binary cache file")
			continue
		}
		candidates = append(candidates, info)
	}

	for len(keep) < serviceCopiesToKeep {
		newestPath := newestUnkeptServiceBinary(dataDir, candidates, keep)
		if newestPath == "" {
			break
		}
		keep[newestPath] = struct{}{}
	}

	for _, info := range candidates {
		path := filepath.Join(dataDir, info.Name())
		cleanPath := filepath.Clean(path)
		if _, ok := keep[cleanPath]; ok {
			continue
		}
		if s.isRunningServiceBinary(cleanPath) {
			log.Debug().Str("path", cleanPath).Msg("skipping cleanup of running service binary")
			continue
		}
		if err := os.Remove(cleanPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Str("path", cleanPath).Msg("error removing stale service binary")
		}
	}
}

func newestUnkeptServiceBinary(dataDir string, candidates []os.FileInfo, keep map[string]struct{}) string {
	newestPath := ""
	var newestModTime time.Time
	for _, info := range candidates {
		path := filepath.Clean(filepath.Join(dataDir, info.Name()))
		if _, ok := keep[path]; ok {
			continue
		}
		if newestPath == "" || info.ModTime().After(newestModTime) {
			newestPath = path
			newestModTime = info.ModTime()
		}
	}
	return newestPath
}

func (s *Service) isRunningServiceBinary(path string) bool {
	pid, err := s.Pid()
	if err != nil || pid <= 0 || runtime.GOOS != "linux" {
		return false
	}
	cleanPath := filepath.Clean(path)
	exePath, err := os.Readlink(filepath.Join(procDir(), strconv.Itoa(pid), "exe"))
	if err == nil && filepath.Clean(exePath) == cleanPath {
		return true
	}

	cmdlinePath := filepath.Join(procDir(), strconv.Itoa(pid), "cmdline")
	cmdline, err := os.ReadFile(cmdlinePath) //nolint:gosec // reads process status for service management
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(cmdline), "\x00") {
		if filepath.Clean(arg) == cleanPath {
			return true
		}
	}
	return false
}

// filesEqual reports whether the files at pathA and pathB have identical
// contents. Returns false (not an error) if pathB does not exist. A size
// comparison is performed first as a fast pre-filter before streaming.
func filesEqual(pathA, pathB string) (bool, error) {
	infoA, err := os.Stat(pathA) //nolint:gosec // G703: paths from os.Executable() and internal DataDir
	if err != nil {
		return false, fmt.Errorf("error statting source: %w", err)
	}

	infoB, err := os.Stat(pathB) //nolint:gosec // G703: paths from os.Executable() and internal DataDir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("error statting destination: %w", err)
	}

	if infoA.Size() != infoB.Size() {
		return false, nil
	}

	//nolint:gosec // G304: paths from os.Executable() and internal DataDir
	fa, err := os.Open(pathA)
	if err != nil {
		return false, fmt.Errorf("error opening source: %w", err)
	}
	defer func() { _ = fa.Close() }()

	//nolint:gosec // G304: paths from os.Executable() and internal DataDir
	fb, err := os.Open(pathB)
	if err != nil {
		return false, fmt.Errorf("error opening destination: %w", err)
	}
	defer func() { _ = fb.Close() }()

	bufA := make([]byte, 32*1024)
	bufB := make([]byte, 32*1024)
	for {
		nA, errA := fa.Read(bufA)
		nB, errB := fb.Read(bufB)

		if !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false, nil
		}

		if errA == io.EOF && errB == io.EOF {
			return true, nil
		}
		if errA != nil {
			return false, fmt.Errorf("error reading source: %w", errA)
		}
		if errB != nil {
			return false, fmt.Errorf("error reading destination: %w", errB)
		}
	}
}

// Set up signal handler to stop service on SIGINT or SIGTERM.
// Exits the application on signal.
func (s *Service) setupStopService() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs

		err := s.stopService()
		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}()
}

// Starts the service and blocks until the service is stopped.
func (s *Service) startService() {
	running, err := s.Running()
	if err != nil {
		log.Error().Err(err).Msg("service PID file conflict")
		os.Exit(1)
	}
	if running {
		log.Error().Msg("service already running")
		os.Exit(1)
	}

	log.Info().Msg("starting service")

	err = s.createPidFile()
	if err != nil {
		log.Error().Err(err).Msg("error creating pid file")
		os.Exit(1)
	}

	err = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 1)
	if err != nil {
		log.Error().Err(err).Msg("error setting nice level")
	}

	result, err := s.start()
	if err != nil {
		log.Error().Err(err).Msg("error starting service")

		err = s.removePidFile()
		if err != nil {
			log.Error().Err(err).Msg("error removing pid file")
		}

		os.Exit(1)
	}

	s.setupStopService()
	s.stop = result.Stop
	s.done = result.Done

	if !s.daemon {
		if stopErr := s.stopService(); stopErr != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	<-result.Done
	log.Info().Msg("service shut down internally")

	err = s.removePidFile()
	if err != nil {
		log.Error().Err(err).Msg("error removing pid file")
	}

	if result.RestartRequested != nil && result.RestartRequested() {
		if execErr := s.restartServiceBinary(); execErr != nil {
			log.Error().Err(execErr).Msg("failed to re-exec for restart")
			os.Exit(1)
		}
	}

	os.Exit(0)
}

func (s *Service) restartServiceBinary() error {
	cfg, err := s.restartExecConfig(os.Args, os.Environ())
	if err != nil {
		return err
	}

	log.Info().
		Str("binary", cfg.serviceBin).
		Str("source", cfg.binPath).
		Strs("args", os.Args).
		Msg("restart requested, re-executing service binary")

	//nolint:gosec // Safe: serviceBin is prepared from os.Executable() or ZAPAROO_APP.
	err = syscall.Exec(cfg.serviceBin, cfg.args, cfg.env)
	return fmt.Errorf("exec failed: %w", err)
}

func (s *Service) restartExecConfig(
	args []string,
	env []string,
) (restartExecConfig, error) {
	binPath, err := serviceSourceBinaryPath()
	if err != nil {
		return restartExecConfig{}, err
	}
	serviceBin, err := s.prepareBinary(binPath)
	if err != nil {
		return restartExecConfig{}, err
	}
	return restartExecConfig{
		serviceBin: serviceBin,
		binPath:    binPath,
		args:       serviceExecArgs(serviceBin, args),
		env:        serviceExecEnv(env, binPath),
	}, nil
}

func serviceSourceBinaryPath() (string, error) {
	if appPath := os.Getenv(config.AppEnv); appPath != "" {
		return appPath, nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("error getting absolute binary path: %w", err)
	}
	return exePath, nil
}

func serviceExecEnv(env []string, appPath string) []string {
	appEnv := fmt.Sprintf("%s=%s", config.AppEnv, appPath)
	for i, value := range env {
		if strings.HasPrefix(value, config.AppEnv+"=") {
			env[i] = appEnv
			return env
		}
	}
	return append(env, appEnv)
}

func serviceExecArgs(serviceBin string, args []string) []string {
	if len(args) == 0 {
		return []string{serviceBin}
	}
	execArgs := append([]string(nil), args...)
	execArgs[0] = serviceBin
	return execArgs
}

// Start a new service daemon in the background.
func (s *Service) Start() error {
	started := time.Now()
	running, err := s.Running()
	if err != nil {
		return err
	}
	if running {
		return errors.New("service already running")
	}

	binPath, err := serviceSourceBinaryPath()
	if err != nil {
		return err
	}

	prepareStarted := time.Now()
	serviceBin, err := s.prepareBinary(binPath)
	if err != nil {
		return err
	}
	log.Debug().Dur("duration", time.Since(prepareStarted)).Msg("service binary prepared")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	//nolint:gosec // Safe: executes copy of current binary for service restart
	cmd := exec.CommandContext(ctx, serviceBin, "-service", "exec")
	cmd.Env = serviceExecEnv(os.Environ(), binPath)

	// Detach from parent: create new session
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// point new binary to existing config file
	configPath := filepath.Join(helpers.ConfigDir(s.pl), config.CfgFile)

	if _, statErr := os.Stat(configPath); statErr == nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", config.CfgEnv, configPath))
	}

	cmdStarted := time.Now()
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting service: %w", err)
	}
	log.Debug().Dur("duration", time.Since(cmdStarted)).Msg("service process start command completed")

	err = cmd.Process.Release()
	if err != nil {
		return fmt.Errorf("error releasing service process: %w", err)
	}

	// Give process a moment to write PID file
	time.Sleep(500 * time.Millisecond)

	pid, pidErr := s.Pid()
	if pidErr != nil {
		log.Error().Err(pidErr).Msg("PID file not found after service start - process may have failed")
		return fmt.Errorf("service started but PID file not found: %w", pidErr)
	}

	log.Info().Int("pid", pid).Dur("duration", time.Since(started)).Msg("service process started")

	running, err = s.Running()
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("service process %d started but is no longer running", pid)
	}

	return nil
}

// Stop the service daemon.
func (s *Service) Stop() error {
	pid, err := s.Pid()
	if err != nil {
		return err
	}
	if pid == 0 {
		return errors.New("service not running")
	}
	if !pidRunning(pid) {
		return s.removePidFile()
	}
	if !s.pidMatchesService(pid) {
		return fmt.Errorf("refusing to stop PID %d because it does not match the Zaparoo service binary", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := stopProcess(process, pid, func(timeout time.Duration) error {
		return s.waitForServiceExit(pid, timeout)
	}); err != nil {
		return err
	}

	if err := s.removePidFile(); err != nil {
		return err
	}
	return waitForAPIPortRelease(s.cfg, servicePortReleaseTimeout, serviceStopPollInterval)
}

func (s *Service) waitForServiceExit(pid int, timeout time.Duration) error {
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	return waitForPIDExit(pid, timeout, serviceStopPollInterval, func(pid int) bool {
		if _, err := os.Stat(pidPath); errors.Is(err, os.ErrNotExist) {
			return false
		}
		return pidRunning(pid)
	})
}

func stopProcess(process *os.Process, pid int, wait processWaitFunc) error {
	if process == nil {
		return errors.New("process is nil")
	}

	if err := signalProcess(process, pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	stopErr := wait(serviceStopTimeout)
	if stopErr == nil {
		return nil
	}
	log.Warn().Err(stopErr).Int("pid", pid).Msg("process did not stop after SIGTERM, sending SIGKILL")

	if err := signalProcess(process, pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
	}
	return wait(serviceKillTimeout)
}

func signalProcess(process *os.Process, pid int, sig syscall.Signal) error {
	if pid > 0 {
		if err := syscall.Kill(-pid, sig); err == nil {
			return nil
		} else if errors.Is(err, syscall.EPERM) {
			log.Debug().Err(err).Int("pid", pid).Msg("failed to signal process group, falling back to process signal")
		} else if !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("failed to signal process group %d: %w", pid, err)
		}
	}
	if err := process.Signal(sig); err != nil {
		return fmt.Errorf("failed to signal process %d: %w", pid, err)
	}
	return nil
}

func pidRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, syscall.Signal(0))
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	return !pidIsZombie(pid)
}

func pidIsZombie(pid int) bool {
	statPath := filepath.Join(procDir(), strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(statPath) //nolint:gosec // reads process status for service management
	if err != nil {
		return false
	}

	fieldsStart := strings.LastIndexByte(string(data), ')')
	if fieldsStart == -1 || fieldsStart+2 >= len(data) {
		return false
	}
	return data[fieldsStart+2] == 'Z'
}

func (s *Service) pidMatchesService(pid int) bool {
	if runtime.GOOS != "linux" {
		return true
	}

	dataDir := helpers.DataDir(s.pl)
	exePath, err := os.Readlink(filepath.Join(procDir(), strconv.Itoa(pid), "exe"))
	if err == nil && pathLooksLikeServiceBinary(exePath, dataDir) {
		return true
	}

	cmdlinePath := filepath.Join(procDir(), strconv.Itoa(pid), "cmdline")
	cmdline, err := os.ReadFile(cmdlinePath) //nolint:gosec // reads process status for service management
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(cmdline), "\x00") {
		if pathLooksLikeServiceBinary(arg, dataDir) {
			return true
		}
	}
	return false
}

func procDir() string {
	return string(filepath.Separator) + "proc"
}

func pathLooksLikeServiceBinary(path, dataDir string) bool {
	return serviceCachePathValid(path, dataDir)
}

func waitForPIDExit(
	pid int,
	timeout time.Duration,
	pollInterval time.Duration,
	isRunning func(int) bool,
) error {
	if pid <= 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)
	for isRunning(pid) {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for process %d to stop", pid)
		}
		time.Sleep(pollInterval)
	}

	return nil
}

func waitForAPIPortRelease(cfg *config.Instance, timeout, pollInterval time.Duration) error {
	if cfg == nil || cfg.APIPort() == 0 {
		return nil
	}

	addrs := apiDialAddresses(cfg)
	deadline := time.Now().Add(timeout)
	for {
		released := true
		for _, addr := range addrs {
			ctx, cancel := context.WithTimeout(context.Background(), pollInterval)
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
			cancel()
			if err != nil {
				continue
			}
			released = false
			_ = conn.Close()
		}
		if released {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for API port %s to release", strings.Join(addrs, ", "))
		}
		time.Sleep(pollInterval)
	}
}

func newCommandWaiter(cmd *exec.Cmd) *commandWaiter {
	return &commandWaiter{
		cmd:  cmd,
		done: make(chan error, 1),
	}
}

func (w *commandWaiter) wait(timeout time.Duration) error {
	w.once.Do(func() {
		go func() { w.done <- w.cmd.Wait() }()
	})

	select {
	case err := <-w.done:
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return nil
			}
			return err
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for process %d to stop", w.cmd.Process.Pid)
	}
}

func apiDialAddresses(cfg *config.Instance) []string {
	host, port, err := net.SplitHostPort(cfg.APIListen())
	if err != nil {
		return []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.APIPort()))}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return []string{net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port)}
	}
	return []string{net.JoinHostPort(host, port)}
}

// Restart is intentionally idempotent: if Running reports no live PID, Stop and
// waitForPIDExit are skipped and Start is called directly. When a PID is still
// active, Restart follows the full Stop -> waitForPIDExit -> Start sequence so
// pidRunning has confirmed the old process is gone before the replacement starts.
func (s *Service) Restart() error {
	oldPID := 0
	running, err := s.Running()
	if err != nil {
		return err
	}
	if running {
		oldPID, err = s.Pid()
		if err != nil {
			return err
		}

		err = s.Stop()
		if err != nil {
			return err
		}
	}

	if waitErr := s.waitForServiceExit(oldPID, serviceStopTimeout); waitErr != nil {
		return waitErr
	}
	if waitErr := waitForAPIPortRelease(s.cfg, servicePortReleaseTimeout, serviceStopPollInterval); waitErr != nil {
		return waitErr
	}

	err = s.Start()
	if err != nil {
		return err
	}

	return nil
}

// WaitForAPI waits for the service API to become available with health monitoring.
// Returns nil if API became available, error if timeout or process crashed.
func (s *Service) WaitForAPI(cfg *config.Instance, maxWait, checkInterval time.Duration) error {
	if client.WaitForAPI(cfg, maxWait, checkInterval) {
		log.Info().Msg("API is now available")
		return nil
	}

	running, err := s.Running()
	if err != nil {
		log.Error().Err(err).Msg("service PID file conflict")
		return err
	}
	if !running {
		log.Error().Msg("service process is no longer running")
		return errors.New("service process crashed during startup")
	}

	log.Warn().Msg("service process is running but API is not responding")
	return errors.New("API did not become available within timeout")
}

// SpawnDaemon spawns a daemon subprocess for service isolation (e.g., TUI mode).
// It waits for the service API to become available and returns a cleanup function.
// The cleanup function sends SIGTERM and waits for graceful shutdown with timeout.
// Returns a no-op cleanup function if service was already running.
func SpawnDaemon(cfg *config.Instance) (cleanup func(), err error) {
	if client.IsServiceRunning(cfg) {
		log.Info().
			Int("port", cfg.APIPort()).
			Msg("connecting to existing service instance")
		return func() {}, nil // no-op cleanup when using existing service
	}

	log.Info().Msg("spawning daemon subprocess")

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	//nolint:gosec // exe is from os.Executable()
	cmd := exec.CommandContext(context.Background(), exe, "-daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}
	waiter := newCommandWaiter(cmd)
	var cleanupOnce sync.Once
	log.Info().Int("pid", cmd.Process.Pid).Msg("daemon subprocess started")

	// Wait for service to be ready
	deadline := time.Now().Add(daemonReadyTimeout)
	for time.Now().Before(deadline) {
		if client.IsServiceRunning(cfg) {
			log.Info().Int("pid", cmd.Process.Pid).Msg("daemon API is ready")
			return func() {
				cleanupOnce.Do(func() {
					if cmd.Process == nil {
						return
					}

					process := cmd.Process
					pid := process.Pid
					defer func() { cmd.Process = nil }()

					log.Info().Int("pid", pid).Msg("stopping daemon subprocess")
					if err := stopProcess(process, pid, waiter.wait); err != nil {
						log.Error().Err(err).Int("pid", pid).Msg("error stopping daemon subprocess")
					}
					if err := waitForAPIPortRelease(
						cfg, servicePortReleaseTimeout, serviceStopPollInterval,
					); err != nil {
						log.Warn().Err(err).Msg("daemon subprocess stopped but API port is still in use")
					}
				})
			}, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := stopProcess(cmd.Process, cmd.Process.Pid, waiter.wait); err != nil {
		log.Warn().
			Err(err).
			Int("pid", cmd.Process.Pid).
			Msg("failed to clean up daemon subprocess after startup timeout")
	}
	return nil, errors.New("daemon failed to start within 3 seconds")
}

func (s *Service) ServiceHandler(cmd *string) error {
	switch *cmd {
	case "exec":
		s.startService()
		return nil
	case "start":
		err := s.Start()
		if err != nil {
			log.Error().Err(err).Msg("error starting service")
			os.Exit(1)
		}
		os.Exit(0)
	case "stop":
		err := s.Stop()
		if err != nil {
			log.Error().Err(err).Msg("error stopping service")
			os.Exit(1)
		}
		os.Exit(0)
	case "restart":
		err := s.Restart()
		if err != nil {
			log.Error().Err(err).Msg("error restarting service")
			os.Exit(1)
		}
		os.Exit(0)
	case "status":
		running, err := s.Running()
		if err != nil {
			log.Error().Err(err).Msg("service PID file conflict")
			os.Exit(1)
		}
		if running {
			_, _ = fmt.Println("started")
			os.Exit(0)
		}
		_, _ = fmt.Println("stopped")
		os.Exit(1)
	case "":
		return nil // no command provided, do nothing
	default:
		return fmt.Errorf("unknown service argument: %s", *cmd)
	}
	return nil
}
