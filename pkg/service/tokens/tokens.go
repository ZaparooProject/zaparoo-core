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
	Type     string
	UID      string
	Text     string
	Data     string
	ScanTime time.Time
	Remote   bool // TODO: wtf does this even do now
	Source   string
}
