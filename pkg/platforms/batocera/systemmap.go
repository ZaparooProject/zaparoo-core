package batocera

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
)

var SystemMap = map[string]string{
	"3do": systemdefs.System3DO,
	//"abuse":        systemdefs.SystemDOS,
	//"adam":         systemdefs.SystemColecoVision,
	"advision":  systemdefs.SystemAdventureVision,
	"amiga1200": systemdefs.SystemAmiga,
	"amiga500":  systemdefs.SystemAmiga,
	"amigacd32": systemdefs.SystemAmigaCD32,
	//"amigacdtv":    systemdefs.SystemAmiga,
	//"amstradcpc":   systemdefs.SystemAmstrad,
	//"apfm1000":     systemdefs.SystemArcade,
	//"apple2":       systemdefs.SystemAppleII,
	//"apple2gs":     systemdefs.SystemAppleII,
	"arcadia": systemdefs.SystemArcadia,
	//"archimedes":   systemdefs.SystemPC,
	"arduboy": systemdefs.SystemArduboy,
	//"astrocde":     systemdefs.SystemAstrocade,
	"atari2600": systemdefs.SystemAtari2600,
	"atari5200": systemdefs.SystemAtari5200,
	"atari7800": systemdefs.SystemAtari7800,
	"atari800":  systemdefs.SystemAtari800,
	//"atarist":      systemdefs.SystemPC,
	"atom": systemdefs.SystemAcornAtom,
	//"atomiswave":   systemdefs.SystemArcade,
	"bbc": systemdefs.SystemBBCMicro,
	//"c128":         systemdefs.SystemC64,
	//"c20":          systemdefs.SystemVIC20,
	"c64": systemdefs.SystemC64,
	//"camplynx":     systemdefs.SystemLynx48,
	//"cannonball":   systemdefs.SystemArcade,
	//"cavestory":    systemdefs.SystemPC,
	"cdi": systemdefs.SystemCDI,
	//"cdogs":        systemdefs.SystemDOS,
	//"cgenius":      systemdefs.SystemDOS,
	"channelf":     systemdefs.SystemChannelF,
	"coco":         systemdefs.SystemCoCo2,
	"colecovision": systemdefs.SystemColecoVision,
	//"commanderx16": systemdefs.SystemPC,
	//"corsixth":     systemdefs.SystemPC,
	//"cplus4":       systemdefs.SystemC16,
	//"crvision":     systemdefs.SystemCreatiVision,
	//"daphne":       systemdefs.SystemArcade,
	//"devilutionx":  systemdefs.SystemPC,
	//"doom3":        systemdefs.SystemPC,
	"dos":       systemdefs.SystemDOS,
	"dreamcast": systemdefs.SystemDreamcast,
	//"easyrpg":      systemdefs.SystemPC,
	//"ecwolf":       systemdefs.SystemDOS,
	//"eduke32":      systemdefs.SystemDOS,
	"electron": systemdefs.SystemAcornElectron,
	//"fallout1-ce":  systemdefs.SystemDOS,
	//"fallout2-ce":  systemdefs.SystemDOS,
	//"fbneo":        systemdefs.SystemArcade,
	"fds": systemdefs.SystemFDS,
	//"flash":        systemdefs.SystemPC,
	//"fm7":          systemdefs.SystemPC,
	//"fmtowns":      systemdefs.SystemPC,
	"gamate":       systemdefs.SystemGamate,
	"gameandwatch": systemdefs.SystemGameNWatch,
	"gamecom":      systemdefs.SystemGameCom,
	"gamecube":     systemdefs.SystemGameCube,
	"gamegear":     systemdefs.SystemGameGear,
	//"gamepock":     systemdefs.SystemPocketChallengeV2,
	"gb":         systemdefs.SystemGameboy,
	"gb2players": systemdefs.SystemGameboy2P,
	"gba":        systemdefs.SystemGBA,
	"gbc":        systemdefs.SystemGameboyColor,
	//"gbc2players":  systemdefs.SystemGameboy2P,
	//"gmaster":      systemdefs.SystemGameGear,
	//"gp32":         systemdefs.SystemPSP,
	//"gx4000":       systemdefs.SystemAmstrad,
	//"gzdoom":       systemdefs.SystemDOS,
	//"hcl":          systemdefs.SystemPC,
	"intellivision": systemdefs.SystemIntellivision,
	"jaguar":        systemdefs.SystemJaguar,
	"jaguarcd":      systemdefs.SystemJaguarCD,
	//"laser310":     systemdefs.SystemLaser,
	//"lcdgames":     systemdefs.SystemGameNWatch,
	"lynx":         systemdefs.SystemAtariLynx,
	"macintosh":    systemdefs.SystemMacOS,
	"mame":         systemdefs.SystemArcade,
	"mastersystem": systemdefs.SystemMasterSystem,
	"megadrive":    systemdefs.SystemGenesis,
	"megaduck":     systemdefs.SystemMegaDuck,
	//"model3":       systemdefs.SystemArcade,
	//"moonlight":    systemdefs.SystemPC,
	//"msx1":         systemdefs.SystemMSX,
	//"msx2":         systemdefs.SystemMSX,
	//"msx2+":        systemdefs.SystemMSX,
	//"msxturbor":    systemdefs.SystemMSX,
	"n64": systemdefs.SystemNintendo64,
	//"n64dd":        systemdefs.SystemNintendo64,
	//"naomi":        systemdefs.SystemArcade,
	//"naomi2":       systemdefs.SystemArcade,
	"nds":      systemdefs.SystemNDS,
	"neogeo":   systemdefs.SystemNeoGeo,
	"neogeocd": systemdefs.SystemNeoGeoCD,
	"nes":      systemdefs.SystemNES,
	"ngp":      systemdefs.SystemNeoGeoPocket,
	"ngpc":     systemdefs.SystemNeoGeoPocketColor,
	//"o2em":         systemdefs.SystemOdyssey2,
	//"pc88":         systemdefs.SystemPC,
	//"pc98":         systemdefs.SystemPC,
	"pcengine":   systemdefs.SystemTurboGrafx16,
	"pcenginecd": systemdefs.SystemTurboGrafx16CD,
	//"pcfx":         systemdefs.SystemPC,
	"pdp1": systemdefs.SystemPDP1,
	"pet":  systemdefs.SystemPET2001,
	//"pico":         systemdefs.SystemGenesis,
	//"pico8":        systemdefs.SystemPC,
	"pokemini": systemdefs.SystemPokemonMini,
	"psp":      systemdefs.SystemPSP,
	"psx":      systemdefs.SystemPSX,
	"pv1000":   systemdefs.SystemCasioPV1000,
	"samcoupe": systemdefs.SystemSAMCoupe,
	//"satellaview":  systemdefs.SystemSNES,
	"saturn": systemdefs.SystemSaturn,
	//"scummvm":      systemdefs.SystemPC,
	//"scv":          systemdefs.SystemSG1000,
	"sega32x": systemdefs.SystemSega32X,
	"segacd":  systemdefs.SystemMegaCD,
	"sg1000":  systemdefs.SystemSG1000,
	"sgb":     systemdefs.SystemSuperGameboy,
	"snes":    systemdefs.SystemSNES,
	//"snes-msu1":    systemdefs.SystemSNESMusic,
	"supergrafx":  systemdefs.SystemSuperGrafx,
	"supervision": systemdefs.SystemSuperVision,
	"ti99":        systemdefs.SystemTI994A,
	//"tutor":        systemdefs.SystemTomyTutor,
	"vc4000":     systemdefs.SystemVC4000,
	"vectrex":    systemdefs.SystemVectrex,
	"virtualboy": systemdefs.SystemVirtualBoy,
	"wii":        systemdefs.SystemWii,
	"wswan":      systemdefs.SystemWonderSwan,
	"wswanc":     systemdefs.SystemWonderSwanColor,
	//"x1":           systemdefs.SystemPC,
	"x68000":     systemdefs.SystemX68000,
	"xegs":       systemdefs.SystemAtariXEGS,
	"zx81":       systemdefs.SystemZX81,
	"zxspectrum": systemdefs.SystemZXSpectrum,
}

var ReverseSystemMap = map[string]string{}

func init() {
	for k, v := range SystemMap {
		ReverseSystemMap[v] = k
	}
}

func fromBatoceraSystem(batoceraSystem string) (string, error) {
	v, ok := SystemMap[batoceraSystem]
	if !ok {
		return "", fmt.Errorf("unknown system: %s", batoceraSystem)
	}
	return v, nil
}

func toBatoceraSystem(zaparooSystem string) (string, error) {
	v, ok := ReverseSystemMap[zaparooSystem]
	if !ok {
		return "", fmt.Errorf("unknown system: %s", zaparooSystem)
	}
	return v, nil
}
