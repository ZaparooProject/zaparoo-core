package playlists

type PlaylistItem struct {
	ZapScript string
	Name      string
}

type Playlist struct {
	ID      string
	Name    string
	Items   []PlaylistItem
	Index   int
	Playing bool
}

func NewPlaylist(id, name string, item []PlaylistItem) *Playlist {
	return &Playlist{
		ID:      id,
		Name:    name,
		Items:   item,
		Index:   0,
		Playing: false,
	}
}

func Next(p Playlist) *Playlist {
	idx := p.Index + 1
	if idx >= len(p.Items) {
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Previous(p Playlist) *Playlist {
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Items) - 1
	}
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Goto(p Playlist, idx int) *Playlist {
	if idx >= len(p.Items) {
		idx = len(p.Items) - 1
	} else if idx < 0 {
		idx = 0
	}
	p.Index = idx
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Play(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   p.Index,
		Playing: true,
	}
}

func Pause(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Items:   p.Items,
		Index:   p.Index,
		Playing: false,
	}
}

func (p *Playlist) Current() PlaylistItem {
	return p.Items[p.Index]
}

type PlaylistController struct {
	Active *Playlist
	Queue  chan<- *Playlist
}
