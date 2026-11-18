// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package plex

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

// Player commands, named as the Companion protocol names them.
const (
	cmdPlay        = "play"
	cmdPause       = "pause"
	cmdStop        = "stop"
	cmdNext        = "skipNext"
	cmdPrevious    = "skipPrevious"
	cmdStepForward = "stepForward"
	cmdStepBack    = "stepBack"
)

const (
	playerKindCompanion = "Companion"
	playerKindCast      = "Cast"
)

// companionTimeout bounds one Companion command. A working player answers
// at once; a wedged one leaves the request open until the server gives up.
const companionTimeout = 10 * time.Second

// accountLookupTimeout bounds the optional plex.tv player lookup, which
// shares a control's deadline with the command it finds a player for.
const accountLookupTimeout = 5 * time.Second

// errPlayerNoAnswer is a Companion command the player never answered.
var errPlayerNoAnswer = errors.New("plex player did not answer")

// commandID numbers Companion commands. Players use it to order commands
// from one controller, so it only ever increases within a process.
var commandID atomic.Int64

func nextCommandID() string {
	return strconv.FormatInt(commandID.Add(1), 10)
}

// serverInfo is how a player reaches the Plex server to fetch media.
type serverInfo struct {
	MachineID string
	Version   string
	Protocol  string
	Address   string
	Token     string
	Port      int
}

func newServerInfo(client PlexClient, id *Identity) (serverInfo, error) {
	u, err := url.Parse(client.ServerURL())
	if err != nil || u.Hostname() == "" {
		return serverInfo{}, fmt.Errorf("invalid plex server URL %q", client.ServerURL())
	}
	protocol := strings.ToLower(u.Scheme)
	port := 80
	if protocol == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return serverInfo{}, fmt.Errorf("invalid plex server port %q: %w", p, err)
		}
	}
	return serverInfo{
		MachineID: id.MachineIdentifier,
		Version:   id.Version,
		Protocol:  protocol,
		Address:   u.Hostname(),
		Port:      port,
		Token:     client.ServerToken(),
	}, nil
}

// playRequest is a queue to start on a player.
type playRequest struct {
	Queue  *PlayQueue
	First  *Metadata
	Kind   *mediaKind
	Server serverInfo
}

// playerState is what a player says it is doing.
type playerState struct {
	RatingKey string
	Playing   bool
	Paused    bool
}

// player is a device a Plex queue can be sent to.
type player interface {
	Name() string
	Kind() string
	Play(ctx context.Context, req *playRequest) error
	Command(ctx context.Context, cmd string, kind *mediaKind) error
	State(ctx context.Context) (*playerState, error)
}

// companionPlayer is a player driven over the Plex Companion HTTP protocol.
type companionPlayer struct {
	http      *http.Client
	name      string
	machineID string
	baseURL   string
	clientID  string
	token     string
	// timeout overrides companionTimeout in tests.
	timeout time.Duration
}

func (p *companionPlayer) Name() string { return p.name }
func (*companionPlayer) Kind() string   { return playerKindCompanion }

func (p *companionPlayer) headers() http.Header {
	h := plexHeaders(p.clientID, p.token)
	h.Set("X-Plex-Target-Client-Identifier", p.machineID)
	return h
}

func (p *companionPlayer) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = companionTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	query.Set("commandID", nextCommandID())
	body, err := doRawRequest(cmdCtx, p.http, http.MethodGet, p.baseURL+path+"?"+query.Encode(), p.headers())
	if err != nil {
		if cmdCtx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("%w: %q gave no answer to %s within %s, restarting the Plex app may help",
				errPlayerNoAnswer, p.name, path, timeout)
		}
		return nil, fmt.Errorf("plex player %q: %w", p.name, err)
	}
	return body, nil
}

func (p *companionPlayer) Play(ctx context.Context, req *playRequest) error {
	query := url.Values{}
	query.Set("key", "/library/metadata/"+req.First.RatingKey)
	query.Set("containerKey", fmt.Sprintf("/playQueues/%d?own=1&window=200", req.Queue.ID))
	query.Set("machineIdentifier", req.Server.MachineID)
	query.Set("address", req.Server.Address)
	query.Set("port", strconv.Itoa(req.Server.Port))
	query.Set("protocol", req.Server.Protocol)
	query.Set("token", req.Server.Token)
	query.Set("offset", "0")
	query.Set("type", req.Kind.controlType)

	// Some players build their own queue only through createPlayQueue and
	// others do not know it, so its failure is not the launch's.
	create := url.Values{}
	for k, v := range query {
		create[k] = v
	}
	create.Set("uri", libraryURI(req.Server.MachineID, req.First.RatingKey))
	if _, err := p.get(ctx, "/player/playback/createPlayQueue", create); err != nil {
		if errors.Is(err, errPlayerNoAnswer) {
			return err
		}
		log.Debug().Err(err).Str("player", p.name).Msg("plex player did not take createPlayQueue")
	}

	_, err := p.get(ctx, "/player/playback/playMedia", query)
	return err
}

func (p *companionPlayer) Command(ctx context.Context, cmd string, kind *mediaKind) error {
	query := url.Values{}
	query.Set("type", kind.controlType)
	_, err := p.get(ctx, "/player/playback/"+cmd, query)
	return err
}

type timeline struct {
	Type      string `xml:"type,attr"`
	State     string `xml:"state,attr"`
	RatingKey string `xml:"ratingKey,attr"`
	Key       string `xml:"key,attr"`
}

type timelineContainer struct {
	Timelines []timeline `xml:"Timeline"`
}

func (p *companionPlayer) State(ctx context.Context) (*playerState, error) {
	query := url.Values{}
	query.Set("wait", "0")
	body, err := p.get(ctx, "/player/timeline/poll", query)
	if err != nil {
		return nil, err
	}
	var tl timelineContainer
	if err := xml.Unmarshal(body, &tl); err != nil {
		return nil, fmt.Errorf("plex player %q sent an unreadable timeline: %w", p.name, err)
	}
	state := &playerState{}
	for _, t := range tl.Timelines {
		key := t.RatingKey
		if key == "" {
			key = keyFromPath(t.Key)
		}
		switch strings.ToLower(t.State) {
		case "playing", "buffering":
			return &playerState{Playing: true, RatingKey: key}, nil
		case "paused":
			state = &playerState{Paused: true, RatingKey: key}
		}
	}
	return state, nil
}

// keyFromPath takes the rating key off a /library/metadata/<key> path.
func keyFromPath(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	if validKey(path) {
		return path
	}
	return ""
}

// playerFinder finds the configured player among the Companion players the
// server and plex.tv know and the Cast devices on the network.
type playerFinder struct {
	http       *http.Client
	browseCast func(ctx context.Context) ([]castDevice, error)
	dialCast   castDialer
}

func newCompanionFromServer(f *playerFinder, client PlexClient, c *ServerClient) *companionPlayer {
	host := c.Address
	if host == "" {
		host = c.Host
	}
	if host == "" || c.Port <= 0 || c.MachineIdentifier == "" {
		return nil
	}
	if c.ProtocolCapabilities != "" && !strings.Contains(c.ProtocolCapabilities, "playback") {
		return nil
	}
	return &companionPlayer{
		http:      f.http,
		name:      c.Name,
		machineID: c.MachineIdentifier,
		baseURL:   "http://" + net.JoinHostPort(host, strconv.Itoa(int(c.Port))),
		clientID:  client.ClientIdentifier(),
		token:     client.ServerToken(),
	}
}

func newCompanionFromResource(f *playerFinder, client PlexClient, r *Resource) *companionPlayer {
	if r.ClientIdentifier == "" {
		return nil
	}
	conn := pickConnection(r.Connections)
	if conn == nil {
		return nil
	}
	token := r.AccessToken
	if token == "" {
		token = client.AccountToken()
	}
	return &companionPlayer{
		http:      f.http,
		name:      r.Name,
		machineID: r.ClientIdentifier,
		baseURL:   strings.TrimRight(conn.URI, "/"),
		clientID:  client.ClientIdentifier(),
		token:     token,
	}
}

// pickConnection prefers a local connection, then any direct one. Relays
// are never used.
func pickConnection(conns []Connection) *Connection {
	var direct *Connection
	for i := range conns {
		c := &conns[i]
		if c.Relay || c.URI == "" {
			continue
		}
		if c.Local {
			return c
		}
		if direct == nil {
			direct = c
		}
	}
	return direct
}

// serverPlayers lists the Companion players the Plex server knows.
func (f *playerFinder) serverPlayers(ctx context.Context, client PlexClient) []player {
	var players []player
	clients, err := client.Clients(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to list players known to the plex server")
	}
	for i := range clients {
		if p := newCompanionFromServer(f, client, &clients[i]); p != nil && !hasMachine(players, p.machineID) {
			players = append(players, p)
		}
	}
	return players
}

// withAccountPlayers adds the Companion players registered with plex.tv
// that the server did not list. The lookup gets its own budget so a slow
// plex.tv leaves time for the command the player was found for.
func (f *playerFinder) withAccountPlayers(ctx context.Context, client PlexClient, players []player) []player {
	lookupCtx, cancel := context.WithTimeout(ctx, accountLookupTimeout)
	defer cancel()
	resources, err := client.PlayerResources(lookupCtx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to list players registered with plex.tv")
	}
	for i := range resources {
		if p := newCompanionFromResource(f, client, &resources[i]); p != nil && !hasMachine(players, p.machineID) {
			players = append(players, p)
		}
	}
	return players
}

func hasMachine(players []player, machineID string) bool {
	for _, p := range players {
		if cp, ok := p.(*companionPlayer); ok && cp.machineID == machineID {
			return true
		}
	}
	return false
}

func (f *playerFinder) castPlayers(ctx context.Context, client PlexClient) []player {
	if f.browseCast == nil {
		return nil
	}
	devices, err := f.browseCast(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to look for cast devices")
	}
	players := make([]player, 0, len(devices))
	for i := range devices {
		d := devices[i]
		if d.Name == "" || d.Host == "" {
			continue
		}
		players = append(players, &castPlayer{
			name:     d.Name,
			addr:     net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
			dial:     f.dialCast,
			clientID: client.ClientIdentifier(),
		})
	}
	return players
}

// matchPlayer returns the one player with the given name, or an error when
// several distinct players share it, since picking one could start playback
// on the wrong device.
func matchPlayer(players []player, name string) (player, error) {
	var matches []player
	for _, p := range players {
		if strings.EqualFold(strings.TrimSpace(p.Name()), name) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil //nolint:nilnil // no player has the name
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("several plex players are named %q, give each a unique name; %s",
			name, describePlayers(matches))
	}
}

// find returns the player the client setting names, or the only player
// there is when it names none. A named player is looked for among the
// players the server knows, then those registered with plex.tv, then Cast
// devices, so a player reachable both ways is driven over Companion.
func (f *playerFinder) find(ctx context.Context, client PlexClient) (player, error) {
	name := strings.TrimSpace(client.PlayerName())
	companions := f.serverPlayers(ctx, client)
	if name != "" {
		if p, err := matchPlayer(companions, name); p != nil || err != nil {
			return p, err
		}
	}
	companions = f.withAccountPlayers(ctx, client, companions)
	if name != "" {
		if p, err := matchPlayer(companions, name); p != nil || err != nil {
			return p, err
		}
	}
	casts := f.castPlayers(ctx, client)
	all := append(companions, casts...) //nolint:gocritic // companions is not used after this
	if name != "" {
		if p, err := matchPlayer(casts, name); p != nil || err != nil {
			return p, err
		}
		return nil, fmt.Errorf("plex player %q not found; %s", name, describePlayers(all))
	}
	switch len(all) {
	case 1:
		return all[0], nil
	case 0:
		return nil, errors.New(
			"no plex players found; open Plex on the player or set client in the plex launcher settings")
	default:
		return nil, fmt.Errorf("several plex players found, set client in the plex launcher settings; %s",
			describePlayers(all))
	}
}

func describePlayers(players []player) string {
	if len(players) == 0 {
		return "no players found"
	}
	names := make([]string, 0, len(players))
	for _, p := range players {
		names = append(names, fmt.Sprintf("%q (%s)", p.Name(), p.Kind()))
	}
	return "players found: " + strings.Join(names, ", ")
}

// clientForConfig is the shape of NewClientWithLauncherID, replaced in tests.
type clientForConfig func(cfg *config.Instance, id string, groups []string) PlexClient
