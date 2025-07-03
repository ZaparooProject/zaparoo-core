package playlists

type PlaylistEntry struct {
	ZapScript string
	Name      string
}

type Playlist struct {
	ID      string
	Name    string
	Entries []PlaylistEntry
	Index   int
	Playing bool
}

func NewPlaylist(id string, name string, media []PlaylistEntry) *Playlist {
	return &Playlist{
		ID:      id,
		Name:    name,
		Entries: media,
		Index:   0,
		Playing: false,
	}
}

func Next(p Playlist) *Playlist {
	idx := p.Index + 1
	if idx >= len(p.Entries) {
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Entries: p.Entries,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Previous(p Playlist) *Playlist {
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Entries) - 1
	}
	return &Playlist{
		ID:      p.ID,
		Entries: p.Entries,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Goto(p Playlist, idx int) *Playlist {
	if idx >= len(p.Entries) {
		idx = len(p.Entries) - 1
	} else if idx < 0 {
		idx = 0
	}
	p.Index = idx
	return &Playlist{
		ID:      p.ID,
		Entries: p.Entries,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Play(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Entries: p.Entries,
		Index:   p.Index,
		Playing: true,
	}
}

func Pause(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Entries: p.Entries,
		Index:   p.Index,
		Playing: false,
	}
}

func (p *Playlist) Current() PlaylistEntry {
	return p.Entries[p.Index]
}

type PlaylistController struct {
	Active *Playlist
	Queue  chan<- *Playlist
}
