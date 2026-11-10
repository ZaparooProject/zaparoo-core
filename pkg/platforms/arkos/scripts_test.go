//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	exeStartDir  = filepath.Join(esExeDir, "scripts", eventGameStart)
	homeStartDir = filepath.Join(frontendHome, ".emulationstation", "scripts", eventGameStart)
	homeEndDir   = filepath.Join(frontendHome, ".emulationstation", "scripts", eventGameEnd)
)

type scriptRun struct {
	path string
	args [2]string
}

// recordScripts replaces the script runner with one that records each run and
// returns the error results gives for its path.
func recordScripts(p *Platform, results map[string]error) func() []scriptRun {
	var mu syncutil.Mutex
	var runs []scriptRun
	p.runScript = func(_ context.Context, spec *scriptSpec) error {
		mu.Lock()
		defer mu.Unlock()
		runs = append(runs, scriptRun{path: spec.path, args: spec.args})
		return results[spec.path]
	}
	return func() []scriptRun {
		mu.Lock()
		defer mu.Unlock()
		return append([]scriptRun(nil), runs...)
	}
}

func TestEventScripts(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	assert.Empty(t, eventScripts(fs, frontendHome, eventGameStart))

	writeFile(t, fs, filepath.Join(homeStartDir, "b.sh"), "")
	writeFile(t, fs, filepath.Join(homeStartDir, "a.sh"), "")
	writeFile(t, fs, filepath.Join(homeStartDir, ".hidden"), "")
	writeFile(t, fs, filepath.Join(homeStartDir, "dir", "inner.sh"), "")
	writeFile(t, fs, filepath.Join(exeStartDir, "z.sh"), "")
	writeFile(t, fs, filepath.Join(homeEndDir, "end.sh"), "")

	assert.Equal(t, []string{
		filepath.Join(exeStartDir, "z.sh"),
		filepath.Join(homeStartDir, ".hidden"),
		filepath.Join(homeStartDir, "a.sh"),
		filepath.Join(homeStartDir, "b.sh"),
	}, eventScripts(fs, frontendHome, eventGameStart))
	assert.Equal(t, []string{filepath.Join(homeEndDir, "end.sh")},
		eventScripts(fs, frontendHome, eventGameEnd))
}

func TestEsEscapedPath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/roms/snes/Game.sfc":                  "/roms/snes/Game.sfc",
		"/roms/snes/Super Mario (USA) [!].sfc": `/roms/snes/Super\ Mario\ \(USA\)\ \[\!\].sfc`,
		`/roms/a'b"c$d^e&f*g{h}i?j;k<l>m.sfc`:  `/roms/a\'b\"c\$d\^e\&f\*g\{h\}i\?j\;k\<l\>m.sfc`,
		`/roms/back\slash.sfc`:                 `/roms/back\\slash.sfc`,
		`/roms/already\ escaped.sfc`:           `/roms/already\\ escaped.sfc`,
		"/roms/tick`cmd`.sfc":                  "/roms/tick`cmd`.sfc",
		"/roms/(a)(b).sfc":                     `/roms/\(a\)\(b\).sfc`,
	}
	for in, want := range tests {
		assert.Equal(t, want, esEscapedPath(in), in)
	}
}

// The arguments are what a script sees in "$1" and "$2" after ES's
// system("<script> \"<escaped path>\" \"<stem>\"").
func TestGameStartArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rom  string
		want [2]string
	}{
		{"/roms/snes/Game (USA).sfc", [2]string{`/roms/snes/Game\ \(USA\).sfc`, "Game (USA)"}},
		{
			"/roms/snes/Super Mario (USA) [!].sfc",
			[2]string{`/roms/snes/Super\ Mario\ \(USA\)\ \[\!\].sfc`, "Super Mario (USA) [!]"},
		},
		// Inside double quotes the shell drops the backslash ES put before $
		// and ", and the stem's own backslash before $ too.
		{`/roms/snes/A$B "C".sfc`, [2]string{`/roms/snes/A$B\ "C".sfc`, `A$B "C"`}},
		{`/roms/snes/x\$y.sfc`, [2]string{`/roms/snes/x\$y.sfc`, `x$y`}},
		{"/roms/snes/a`b`.sfc", [2]string{"/roms/snes/a`b`.sfc", "a`b`"}},
		{"/roms/snes/NoExt", [2]string{"/roms/snes/NoExt", "NoExt"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, gameStartArgs(tt.rom), tt.rom)
	}
}

func TestFireEvent_ArgsOrderAndFailures(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	first := filepath.Join(exeStartDir, "first.sh")
	failing := filepath.Join(homeStartDir, "a-fails.sh")
	last := filepath.Join(homeStartDir, "b-last.sh")
	for _, s := range []string{first, failing, last} {
		writeFile(t, fs, s, "")
	}
	p := newTestPlatform(t, fs, newMockCmd())
	runs := recordScripts(p, map[string]error{failing: errors.New("exit status 1")})

	args := gameStartArgs(testROM)
	p.fireEvent(testAccount(), eventGameStart, args)
	assert.Equal(t, []scriptRun{
		{path: first, args: args},
		{path: failing, args: args},
		{path: last, args: args},
	}, runs(), "a failing script does not stop the rest")
}

func TestFireEvent_Timeout(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	slow := filepath.Join(homeStartDir, "a-slow.sh")
	next := filepath.Join(homeStartDir, "b-next.sh")
	writeFile(t, fs, slow, "")
	writeFile(t, fs, next, "")
	p := newTestPlatform(t, fs, newMockCmd())
	p.scriptTimeout = 20 * time.Millisecond

	var ran []string
	var sawDeadline bool
	p.runScript = func(ctx context.Context, spec *scriptSpec) error {
		ran = append(ran, spec.path)
		if spec.path == slow {
			_, sawDeadline = ctx.Deadline()
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}

	done := make(chan struct{})
	go func() {
		p.fireEvent(testAccount(), eventGameStart, [2]string{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fireEvent did not return after the script timed out")
	}
	assert.True(t, sawDeadline)
	assert.Equal(t, []string{slow, next}, ran)
}

func TestScriptCommand(t *testing.T) {
	t.Parallel()

	acct := testAccount()
	spec := &scriptSpec{
		acct: acct,
		path: "/home/ark/.emulationstation/scripts/game-start/batt.sh",
		args: [2]string{`/roms/snes/Game\ \(USA\).sfc`, "Game (USA)"},
	}
	cmd := scriptCommand(context.Background(), spec)

	assert.Equal(t, shellPath, cmd.Path)
	assert.Equal(t, []string{shellPath, "-c", `"$0" "$@"`, spec.path, spec.args[0], spec.args[1]}, cmd.Args)
	assert.Equal(t, frontendHome, cmd.Dir)
	assert.Equal(t, gameEnv(acct), cmd.Env)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setpgid)
	require.NotNil(t, cmd.SysProcAttr.Credential)
	assert.Equal(t, acct.uid, cmd.SysProcAttr.Credential.Uid)
	assert.Equal(t, acct.gid, cmd.SysProcAttr.Credential.Gid)
	assert.Equal(t, acct.groups, cmd.SysProcAttr.Credential.Groups)
	assert.NotNil(t, cmd.Cancel)
	require.NoError(t, cmd.Cancel(), "cancelling an unstarted script is a no-op")
	assert.Equal(t, scriptWaitDelay, cmd.WaitDelay)
}
