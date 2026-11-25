//go:build linux && !android

package mister

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withMoonlightDirs points the launcher at a temporary install for one test
// and returns the client and _Other folders.
func withMoonlightDirs(t *testing.T) (clientDir, otherDir string) {
	t.Helper()
	root := t.TempDir()
	clientDir = filepath.Join(root, "MiSTer_moonlight_crt")
	otherDir = filepath.Join(root, "_Other")
	oldClient, oldOther := moonlightDir, moonlightOtherCoresDir
	moonlightDir, moonlightOtherCoresDir = clientDir, otherDir
	t.Cleanup(func() { moonlightDir, moonlightOtherCoresDir = oldClient, oldOther })
	return clientDir, otherDir
}

func installMoonlight(t *testing.T) string {
	t.Helper()
	clientDir, _ := withMoonlightDirs(t)
	writeFile(t, filepath.Join(clientDir, moonlightBinaryName), 0o700)
	writeFile(t, filepath.Join(clientDir, "core", "H264_20260801.rbf"), 0o600)
	return clientDir
}

func TestParseMoonlightPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		want    moonlightTarget
		wantErr bool
	}{
		{name: "bare scheme", path: "moonlight://"},
		{name: "bare scheme slash", path: "moonlight:///"},
		{name: "indexed menu", path: "moonlight:///Moonlight"},
		{name: "upper case scheme", path: "MOONLIGHT:///Moonlight"},
		{
			name: "name and app", path: "moonlight://Gaming%20PC/Steam%20Big%20Picture",
			want: moonlightTarget{pc: "Gaming PC", app: "Steam Big Picture"},
		},
		{
			name: "address and app", path: "moonlight://192.168.1.20/Desktop",
			want: moonlightTarget{pc: "192.168.1.20", app: "Desktop"},
		},
		{
			name: "trailing slash", path: "moonlight://pc/Desktop/",
			want: moonlightTarget{pc: "pc", app: "Desktop"},
		},
		{name: "pc without app", path: "moonlight://pc", wantErr: true},
		{name: "pc with empty app", path: "moonlight://pc/", wantErr: true},
		{name: "other scheme", path: "steam://123/Game", wantErr: true},
		{name: "file path", path: "/media/fat/games/PC/a.exe", wantErr: true},
		{name: "control character", path: "moonlight://pc/Des%00ktop", wantErr: true},
		{name: "long pc", path: "moonlight://" + strings.Repeat("a", 64) + "/Desktop", wantErr: true},
		{name: "long app", path: "moonlight://pc/" + strings.Repeat("a", 256), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMoonlightPath(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, moonlightTest(nil, tt.path))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.True(t, moonlightTest(nil, tt.path))
		})
	}
}

func TestParseMoonlightConfig(t *testing.T) {
	t.Parallel()

	cfg := strings.Join([]string{
		"bitrate=8000",
		"audio=0",
		"quitafter=1",
		"sync=genlock low latency",
		"pcres=1280x720",
		"lastapp=Desktop",
		"lastpc=10.0.0.5",
		"pcs=1",
		"pc=10.0.0.5|Gaming PC",
		"pc=10.0.0.6|",
		"pc=|Nameless",
		"unknown=value",
		"no equals sign",
	}, "\r\n")

	got, err := parseMoonlightConfig(strings.NewReader(cfg))
	require.NoError(t, err)
	assert.Equal(t, moonlightSettings{
		bitrate:   8000,
		audio:     false,
		quitAfter: true,
		sync:      moonlightSyncGenlockLow,
		pcres:     "1280x720",
		pcs: []moonlightPC{
			{host: "10.0.0.5", name: "Gaming PC"},
			{host: "10.0.0.6", name: "10.0.0.6"},
		},
	}, got)
}

func TestParseMoonlightConfigDefaultsAndBounds(t *testing.T) {
	t.Parallel()

	got, err := parseMoonlightConfig(strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, defaultMoonlightSettings(), got)
	assert.Equal(t, moonlightSyncGenlock, got.sync, "the menu's default sync is genlock")
	assert.True(t, got.audio)

	var b strings.Builder
	b.WriteString("bitrate=999999\nsync=bogus\npcres=1920x1080\n")
	b.WriteString("pc=10.0.0.1|" + strings.Repeat("n", 200) + "\n") // over the line cap: skipped
	b.WriteString("pc=" + strings.Repeat("h", 70) + "|" + strings.Repeat("n", 40) + "\n")
	for i := range 20 {
		b.WriteString("pc=10.0.1." + string(rune('a'+i)) + "\n")
	}
	got, err = parseMoonlightConfig(strings.NewReader(b.String()))
	require.NoError(t, err)
	assert.Equal(t, moonlightMaxBitrate, got.bitrate)
	assert.Equal(t, moonlightSyncGenlock, got.sync, "an unknown sync keeps the default")
	assert.Empty(t, got.pcres, "an unknown resolution is off")
	require.Len(t, got.pcs, moonlightMaxPCs)
	assert.Len(t, got.pcs[0].host, moonlightMaxHost)
	assert.Len(t, got.pcs[0].name, moonlightMaxPCName)

	got, err = parseMoonlightConfig(strings.NewReader("sync=guardia\nbitrate=-5\n"))
	require.NoError(t, err)
	assert.Equal(t, moonlightSync60Hz, got.sync, "older sync names still map")
	assert.Equal(t, -5, got.bitrate)
	assert.NotContains(t, moonlightStreamArgs("h", "a", &got), "-bitrate")

	_, err = parseMoonlightConfig(strings.NewReader(strings.Repeat("x", moonlightMaxConfigSize+1)))
	require.Error(t, err)
}

func TestMoonlightStreamArgs(t *testing.T) {
	t.Parallel()

	defaults := defaultMoonlightSettings()
	assert.Equal(t,
		[]string{"stream", "10.0.0.5", "-app", "Desktop", "-genlock"},
		moonlightStreamArgs("10.0.0.5", "Desktop", &defaults))

	all := moonlightSettings{
		bitrate: 5000, audio: false, quitAfter: true, sync: moonlightSync60Hz, pcres: "1024x768",
	}
	assert.Equal(t,
		[]string{
			"stream", "pc", "-app", "-Steam", "-bitrate", "5000", "-noaudio", "-quitappafter",
			"-60hz", "-pcres", "1024x768",
		},
		moonlightStreamArgs("pc", "-Steam", &all))

	lowLatency := defaultMoonlightSettings()
	lowLatency.sync = moonlightSyncGenlockLow
	assert.Equal(t,
		[]string{"stream", "pc", "-app", "Desktop", "-genlock-low"},
		moonlightStreamArgs("pc", "Desktop", &lowLatency))
}

func TestResolveMoonlightHost(t *testing.T) {
	t.Parallel()

	pcs := []moonlightPC{
		{host: "10.0.0.5", name: "Gaming PC"},
		{host: "laptop.lan", name: "10.0.0.9"},
		{host: "bad host", name: "Broken"},
	}
	tests := []struct {
		pc     string
		want   string
		wantOK bool
	}{
		{pc: "gaming pc", want: "10.0.0.5", wantOK: true},
		{pc: "10.0.0.9", want: "laptop.lan", wantOK: true}, // names win over addresses
		{pc: "LAPTOP.LAN", want: "laptop.lan", wantOK: true},
		{pc: "192.168.1.2", want: "192.168.1.2", wantOK: true},
		{pc: "fe80::1", want: "fe80::1", wantOK: true},
		{pc: "Broken", want: "bad host"},
		{pc: "-genlock", want: "-genlock"},
		{pc: "Unknown PC", want: "Unknown PC"},
	}
	for _, tt := range tests {
		got, ok := resolveMoonlightHost(pcs, tt.pc)
		assert.Equal(t, tt.want, got, tt.pc)
		assert.Equal(t, tt.wantOK, ok, tt.pc)
	}
}

func TestIsH264Core(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"H264_20260801.rbf": true,
		"h264_1.RBF":        true,
		"h264.rbf":          false,
		"H264.RBF":          false,
		"H264_20260801.mra": false,
		"H2640.rbf":         false,
		"MyH264_1.rbf":      false,
	} {
		assert.Equal(t, want, isH264Core(name), name)
	}
}

func TestMoonlightInstalled(t *testing.T) {
	clientDir, otherDir := withMoonlightDirs(t)
	var repair *platforms.LaunchRepairError

	require.ErrorAs(t, moonlightInstalled(), &repair)
	assert.Equal(t, platforms.LaunchRepairLauncherNotInstalled, repair.Reason())
	launcher := createMoonlightLauncher(NewPlatform())
	require.NotNil(t, launcher.Detected)
	assert.False(t, *launcher.Detected)
	results, err := scanMoonlight(context.Background(), nil, "", nil)
	require.NoError(t, err)
	assert.Empty(t, results, "nothing is indexed without the client")

	writeFile(t, filepath.Join(clientDir, moonlightBinaryName), 0o700)
	require.ErrorAs(t, moonlightInstalled(), &repair)
	assert.Equal(t, platforms.LaunchRepairLauncherComponentMissing, repair.Reason())

	// A core folder holding other cores, or an undated H264.rbf the client
	// never loads, is still missing the H264 core.
	writeFile(t, filepath.Join(otherDir, "NES_20250101.rbf"), 0o600)
	writeFile(t, filepath.Join(otherDir, "H264.rbf"), 0o600)
	require.ErrorAs(t, moonlightInstalled(), &repair)
	assert.Equal(t, platforms.LaunchRepairLauncherComponentMissing, repair.Reason())

	writeFile(t, filepath.Join(otherDir, "H264_20250101.rbf"), 0o600)
	require.NoError(t, moonlightInstalled())

	require.NoError(t, os.Remove(filepath.Join(otherDir, "H264_20250101.rbf")))
	writeFile(t, filepath.Join(clientDir, "core", "H264_20260801.rbf"), 0o600)
	require.NoError(t, moonlightInstalled())

	launcher = createMoonlightLauncher(NewPlatform())
	assert.Equal(t, tracker.MoonlightLauncherID, launcher.ID)
	assert.True(t, launcher.SkipFilesystemScan)
	assert.Equal(t, platforms.LifecycleTracked, launcher.Lifecycle)
	require.NotNil(t, launcher.Detected)
	assert.True(t, *launcher.Detected)
	require.NoError(t, launcher.Availability(nil))
}

func TestScanMoonlightIndexesMenu(t *testing.T) {
	installMoonlight(t)

	results, err := scanMoonlight(context.Background(), nil, "", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "moonlight:///Moonlight", results[0].Path)
	assert.Equal(t, moonlightMenuName, results[0].Name)
	assert.True(t, results[0].NoExt)

	launcher := createMoonlightLauncher(NewPlatform())
	assert.True(t, launcher.Test(nil, results[0].Path))
	args, err := moonlightArgs(results[0].Path)
	require.NoError(t, err)
	assert.Equal(t, []string{"menu"}, args)
}

func TestMoonlightArgsUsesMenuConfig(t *testing.T) {
	clientDir := installMoonlight(t)

	// No menu.cfg: the menu's defaults.
	args, err := moonlightArgs("moonlight://10.0.0.5/Desktop")
	require.NoError(t, err)
	assert.Equal(t, []string{"stream", "10.0.0.5", "-app", "Desktop", "-genlock"}, args)

	writeFile(t, filepath.Join(clientDir, "menu.cfg"), 0o600)
	require.NoError(t, os.WriteFile(filepath.Join(clientDir, "menu.cfg"),
		[]byte("bitrate=3000\nsync=60 Hz\npc=10.0.0.5|Gaming PC\n"), 0o600))

	args, err = moonlightArgs("moonlight://Gaming%20PC/Steam%20Big%20Picture")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"stream", "10.0.0.5", "-app", "Steam Big Picture", "-bitrate", "3000", "-60hz",
	}, args)

	_, err = moonlightArgs("moonlight://Unknown%20PC/Desktop")
	require.Error(t, err, "a name that is not a PC or an address")

	_, err = moonlightArgs("moonlight://pc")
	require.Error(t, err)

	// An unreadable menu.cfg falls back to the menu's defaults rather than
	// refusing the launch.
	require.NoError(t, os.Remove(filepath.Join(clientDir, "menu.cfg")))
	require.NoError(t, os.Mkdir(filepath.Join(clientDir, "menu.cfg"), 0o700))
	args, err = moonlightArgs("moonlight://10.0.0.5/Desktop")
	require.NoError(t, err)
	assert.Equal(t, []string{"stream", "10.0.0.5", "-app", "Desktop", "-genlock"}, args)
}

func TestLaunchMoonlightNotInstalled(t *testing.T) {
	withMoonlightDirs(t)

	_, err := launchMoonlight(NewPlatform())(nil, "moonlight:///Moonlight", nil)
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
}

func TestBuildMoonlightCommand(t *testing.T) {
	clientDir := installMoonlight(t)

	cmd := buildMoonlightCommand(context.Background(), []string{"menu"})

	assert.Equal(t, []string{filepath.Join(clientDir, moonlightBinaryName), "menu"}, cmd.Args)
	assert.Equal(t, clientDir, cmd.Dir)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setsid)
	assert.NotNil(t, cmd.Cancel, "a cancelled launcher context must not SIGKILL the client")
}

func TestMoonlightReturnToMenu(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		coreName  string
		preempted bool
		want      bool
	}{
		{name: "client exits on its core", coreName: "H264", want: true},
		{name: "core name case", coreName: "h264", want: true},
		{name: "user loaded another core", coreName: "SNES"},
		{name: "already on the menu", coreName: "MENU"},
		{name: "unknown core", coreName: ""},
		{name: "preempted by a launch", coreName: "H264", preempted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			moonlightReturnToMenu(tt.preempted, tt.coreName, func() error {
				called = true
				return errors.New("logged, not returned")
			})
			assert.Equal(t, tt.want, called)
		})
	}
}

func FuzzParseMoonlightConfig(f *testing.F) {
	f.Add("bitrate=8000\naudio=1\nsync=genlock\npcres=640x480\npc=10.0.0.5|PC\n")
	f.Add("pc=|\npc=a|b|c\r\nsync=\n=\n")
	f.Add(strings.Repeat("pc=h|n\n", 40))
	f.Fuzz(func(t *testing.T, data string) {
		settings, err := parseMoonlightConfig(strings.NewReader(data))
		if err != nil {
			return
		}
		if settings.bitrate > moonlightMaxBitrate {
			t.Fatalf("bitrate %d over the cap", settings.bitrate)
		}
		if len(settings.pcs) > moonlightMaxPCs {
			t.Fatalf("%d PCs over the cap", len(settings.pcs))
		}
		for _, pc := range settings.pcs {
			if pc.host == "" || len(pc.host) > moonlightMaxHost || len(pc.name) > moonlightMaxPCName {
				t.Fatalf("PC out of bounds: %q", pc)
			}
		}
		if settings.pcres != "" {
			if _, ok := moonlightPCResolutions[settings.pcres]; !ok {
				t.Fatalf("unknown resolution %q", settings.pcres)
			}
		}
		for _, pc := range settings.pcs {
			host, ok := resolveMoonlightHost(settings.pcs, pc.name)
			if ok && !validMoonlightHost(host) {
				t.Fatalf("resolved an invalid host %q", host)
			}
		}
	})
}

func FuzzParseMoonlightPath(f *testing.F) {
	f.Add("moonlight://")
	f.Add("moonlight:///Moonlight")
	f.Add("moonlight://Gaming%20PC/Steam%20Big%20Picture;x=1")
	f.Add("moonlight://-pc/-app")
	f.Fuzz(func(t *testing.T, path string) {
		target, err := parseMoonlightPath(path)
		if err != nil || target.menu() {
			return
		}
		if target.app == "" || len(target.pc) > moonlightMaxHost || len(target.app) > moonlightMaxAppName {
			t.Fatalf("stream target out of bounds: %+v", target)
		}
		host, ok := resolveMoonlightHost(nil, target.pc)
		if ok && strings.HasPrefix(host, "-") {
			t.Fatalf("host %q would be read as an option", host)
		}
	})
}

func TestProcessPreempted(t *testing.T) {
	t.Parallel()

	proc := &os.Process{Pid: 1234}
	other := &os.Process{Pid: 5678}
	for _, tt := range []struct {
		requested *os.Process
		name      string
		intent    platforms.StopIntent
		want      bool
	}{
		{name: "preempted", requested: proc, intent: platforms.StopForPreemption, want: true},
		{name: "stopped for the menu", requested: proc, intent: platforms.StopForMenu},
		{name: "another process", requested: other, intent: platforms.StopForPreemption},
		{name: "not stopped", intent: platforms.StopForPreemption},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewPlatform()
			p.stopRequestedProc = tt.requested
			p.stopIntent = tt.intent
			assert.Equal(t, tt.want, p.processPreempted(proc))
		})
	}
}

// installFakeMoonlightClient installs a client that records its arguments
// and exits, and returns the file it writes them to.
func installFakeMoonlightClient(t *testing.T) string {
	t.Helper()
	clientDir := installMoonlight(t)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > args.txt\n"
	bin := filepath.Join(clientDir, moonlightBinaryName)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700)) //nolint:gosec // G306: test client must be executable
	return filepath.Join(clientDir, "args.txt")
}

func TestLaunchMoonlightRunsClient(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "menu", path: "moonlight://", want: []string{"menu"}},
		{
			name: "stream", path: "moonlight://10.0.0.250/Desktop",
			want: []string{"stream", "10.0.0.250", "-app", "Desktop", "-genlock"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := installFakeMoonlightClient(t)
			et := newExitTestPlatform(tracker.MoonlightLauncherID)
			et.pl.launcherManager = state.NewLauncherManager()

			proc, err := launchMoonlight(et.pl)(nil, tt.path, nil)
			require.NoError(t, err)
			require.NotNil(t, proc)
			et.waitUntrackedAfter(t)

			data, err := os.ReadFile(argsFile) //nolint:gosec // G304: test temp file
			require.NoError(t, err, "the client runs in its own folder")
			assert.Equal(t, tt.want, strings.Fields(string(data)))
			assert.Equal(t, 1, et.clearedCount(), "the client exiting on its own ends the media")
		})
	}
}

func TestLaunchMoonlightRefusals(t *testing.T) {
	withMoonlightDirs(t)
	_, err := launchMoonlight(NewPlatform())(nil, "moonlight://", nil)
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair, "a missing client is refused")

	installMoonlight(t)
	_, err = launchMoonlight(NewPlatform())(nil, "moonlight://pc", nil)
	require.Error(t, err, "a malformed path is refused")
	_, err = launchMoonlight(NewPlatform())(nil, "moonlight://bad%20host!/Desktop", nil)
	require.Error(t, err, "an unknown PC that is not an address is refused")
}

// The coordinator runs Preflight before it stops the running media, so a
// path the launch would refuse must fail here.
func TestMoonlightPreflight(t *testing.T) {
	installMoonlight(t)

	require.NoError(t, moonlightPreflight(nil, "moonlight://", nil))
	require.NoError(t, moonlightPreflight(nil, "moonlight://192.168.1.20/Desktop", nil))
	require.Error(t, moonlightPreflight(nil, "moonlight://-genlock/Desktop", nil),
		"a PC that is neither listed nor an address is refused")
	require.Error(t, moonlightPreflight(nil, "moonlight://pc", nil))
}
