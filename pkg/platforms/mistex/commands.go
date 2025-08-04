//go:build linux

package mistex

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
)

var commandsMappings = map[string]func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error){
	"mister.ini":  mister.CmdIni,
	"mister.core": mister.CmdLaunchCore,
	// "mister.script": cmdMisterScript,
	"mister.mgl": mister.CmdMisterMgl,

	"ini": mister.CmdIni, // DEPRECATED
}
