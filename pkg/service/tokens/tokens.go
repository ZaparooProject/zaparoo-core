package tokens

import (
	"time"
)

const (
	TypeNTAG           = "NTAG"
	TypeMifare         = "MIFARE"
	TypeAmiibo         = "Amiibo"
	TypeLegoDimensions = "LegoDimensions"
	SourcePlaylist     = "Playlist"
)

type Token struct {
	ScanTime time.Time
	Type     string
	UID      string
	Text     string
	Data     string
	Source   string
	FromAPI  bool
	Unsafe   bool
}
