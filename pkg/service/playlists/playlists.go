package playlists

type PlaylistMedia struct {
	Path string
	Name string
}

type Playlist struct {
	ID      string
	Media   []PlaylistMedia
	Index   int
	Playing bool
}

func NewPlaylist(id string, media []PlaylistMedia) *Playlist {
	return &Playlist{
		ID:      id,
		Media:   media,
		Index:   0,
		Playing: false,
	}
}

func Next(p Playlist) *Playlist {
	idx := p.Index + 1
	if idx >= len(p.Media) {
		idx = 0
	}
	return &Playlist{
		ID:      p.ID,
		Media:   p.Media,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Previous(p Playlist) *Playlist {
	idx := p.Index - 1
	if idx < 0 {
		idx = len(p.Media) - 1
	}
	return &Playlist{
		ID:      p.ID,
		Media:   p.Media,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Goto(p Playlist, idx int) *Playlist {
	if idx >= len(p.Media) {
		idx = len(p.Media) - 1
	} else if idx < 0 {
		idx = 0
	}
	p.Index = idx
	return &Playlist{
		ID:      p.ID,
		Media:   p.Media,
		Index:   idx,
		Playing: p.Playing,
	}
}

func Play(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Media:   p.Media,
		Index:   p.Index,
		Playing: true,
	}
}

func Pause(p Playlist) *Playlist {
	return &Playlist{
		ID:      p.ID,
		Media:   p.Media,
		Index:   p.Index,
		Playing: false,
	}
}

func (p *Playlist) Current() PlaylistMedia {
	return p.Media[p.Index]
}

type PlaylistController struct {
	Active *Playlist
	Queue  chan<- *Playlist
}
