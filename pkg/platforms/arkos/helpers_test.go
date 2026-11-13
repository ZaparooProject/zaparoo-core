//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const testCommand = "sudo perfmax %GOVERNOR% %ROM%; nice -n -19 /usr/local/bin/%EMULATOR% " +
	"-L /home/ark/.config/%EMULATOR%/cores/%CORE%_libretro.so %ROM%; sudo perfnorm"

const testESSystems = `<?xml version="1.0"?>
<systemList>
  <system>
    <name>snes</name>
    <fullname>Super Nintendo</fullname>
    <path>/roms/snes/</path>
    <extension>.smc .SMC .sfc .SFC .zip .ZIP</extension>
    <command>` + testCommand + `</command>
    <emulators>
      <emulator name="retroarch">
        <cores>
          <core>snes9x</core>
          <core>snes9x2010</core>
        </cores>
      </emulator>
      <emulator name="retroarch32">
        <cores>
          <core>snes9x2005</core>
        </cores>
      </emulator>
    </emulators>
  </system>
  <system>
    <name>sfc</name>
    <fullname>Super Famicom</fullname>
    <path>/roms/sfc/</path>
    <extension>.sfc .bs</extension>
    <command>` + testCommand + `</command>
    <emulators>
      <emulator name="retroarch">
        <cores>
          <core>snes9x</core>
        </cores>
      </emulator>
    </emulators>
  </system>
  <system>
    <name>nes</name>
    <fullname>Nintendo Entertainment System</fullname>
    <path>/roms/nes/</path>
    <extension>.nes .zip</extension>
    <command>` + testCommand + `</command>
    <emulators>
      <emulator name="retroarch32">
        <cores>
          <core>fceumm</core>
        </cores>
      </emulator>
    </emulators>
  </system>
  <system>
    <name>zzunknown</name>
    <path>/roms/zzunknown/</path>
    <extension>.bin</extension>
    <command>%ROM%</command>
  </system>
</systemList>
`

func newTestFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, userESSystemsPath, []byte(testESSystems), 0o644))
	return fs
}

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
}

func testAccount() *account {
	return &account{name: "ark", home: frontendHome, uid: 1002, gid: 1002, groups: []uint32{4, 27}}
}

func newTestPlatform(t *testing.T, fs afero.Fs, cmd *mocks.MockCommandExecutor) *Platform {
	t.Helper()
	p := &Platform{
		Base:          linuxbase.NewBase(platformids.ArkOS),
		fs:            fs,
		cmd:           cmd,
		fe:            newFrontend(cmd, fs),
		clock:         clockwork.NewFakeClock(),
		lookupAccount: func() (*account, error) { return testAccount(), nil },
		esMatcher:     &esGameMatcher{fs: fs, procDir: "/proc", roots: romRoots},
	}
	p.startGame = func(*launchSpec) (*os.Process, error) {
		t.Error("unexpected game start")
		return nil, errors.New("unexpected game start")
	}
	p.killESGame = func(int) { t.Fatal("unexpected ES game kill") }
	p.scriptTimeout = defaultScriptTimeout
	p.runScript = func(_ context.Context, spec *scriptSpec) error {
		t.Errorf("unexpected event script %s", spec.path)
		return nil
	}
	return p
}

func newMockCmd() *mocks.MockCommandExecutor {
	return helpers.NewMockCommandExecutor()
}

// cmdCalls lists the commands run through the mock as "name arg arg".
func cmdCalls(m *mocks.MockCommandExecutor) []string {
	calls := make([]string, 0, len(m.Calls))
	for i := range m.Calls {
		c := &m.Calls[i]
		if c.Method != "Run" {
			continue
		}
		name := c.Arguments.String(1)
		args, ok := c.Arguments.Get(2).([]string)
		if !ok {
			args = nil
		}
		calls = append(calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}
	return calls
}
