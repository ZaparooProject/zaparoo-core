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

package playnite

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// pingInterval is how often Core pings the extension so a dead pipe is
	// noticed without waiting for the next command.
	pingInterval = 5 * time.Second
	// writeTimeout bounds one write to the extension, so an extension that
	// stops reading cannot hold every later command behind it.
	writeTimeout = 5 * time.Second
	// maxLibraryGames bounds how many games one library request may return.
	maxLibraryGames = 500_000
)

var (
	// ErrNotConnected is returned when the extension has no open connection.
	ErrNotConnected = errors.New("the Playnite extension is not connected")
	// ErrStopUnsupported is returned when the extension cannot stop the game
	// because Playnite never told it which process the game is.
	ErrStopUnsupported = errors.New("no process reported by Playnite to stop")
	// errDisconnected fails requests that were waiting when the pipe closed.
	errDisconnected = errors.New("the Playnite extension disconnected")
)

// Handlers receive the extension's unsolicited events. They run on the
// connection's reader, so they must not wait on another message from it.
type Handlers struct {
	Started      func(event *Event)
	Stopped      func(event *Event)
	Write        func(id, name string)
	Disconnected func()
}

// gamesRequest collects the chunks of one library request.
type gamesRequest struct {
	done  chan error
	games []Game
}

// Server talks to the Playnite extension over one connection at a time. The
// extension is the client: it reconnects whenever Playnite restarts, and a
// new connection replaces the previous one.
type Server struct {
	clock    clockwork.Clock
	listener net.Listener
	conn     net.Conn
	writer   *bufio.Writer
	games    map[string]*gamesRequest
	launches map[string]chan Event
	stops    map[string]chan Event
	done     chan struct{}
	handlers Handlers
	hello    Event
	wg       sync.WaitGroup
	nextID   int
	stopOnce sync.Once
	connMu   syncutil.Mutex
	// pendingMu guards the request maps, nextID and hello.
	pendingMu syncutil.Mutex
}

// NewServer builds a server; a nil clock means the real one.
func NewServer(clock clockwork.Clock, handlers Handlers) *Server {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &Server{
		clock:    clock,
		handlers: handlers,
		games:    make(map[string]*gamesRequest),
		launches: make(map[string]chan Event),
		stops:    make(map[string]chan Event),
		done:     make(chan struct{}),
	}
}

// Serve accepts extension connections on listener until Close. It takes
// ownership of the listener.
func (s *Server) Serve(listener net.Listener) {
	s.connMu.Lock()
	s.listener = listener
	s.connMu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.accept(listener)
	}()
}

// Close stops accepting, drops the connection and waits for the readers.
func (s *Server) Close() {
	s.stopOnce.Do(func() {
		close(s.done)
	})
	s.connMu.Lock()
	listener := s.listener
	conn := s.conn
	s.connMu.Unlock()
	if listener != nil {
		if err := listener.Close(); err != nil {
			log.Debug().Err(err).Msg("closing Playnite pipe listener")
		}
	}
	if conn != nil {
		if err := conn.Close(); err != nil {
			log.Debug().Err(err).Msg("closing Playnite pipe connection")
		}
	}
	s.wg.Wait()
}

// Connected reports whether the extension has an open connection.
func (s *Server) Connected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil
}

// Hello returns what the connected extension announced about itself.
func (s *Server) Hello() Event {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return s.hello
}

// Games asks the extension for the whole library and waits for every chunk.
// With details set, each game carries its metadata and image paths.
func (s *Server) Games(ctx context.Context, details bool, timeout time.Duration) ([]Game, error) {
	req := &gamesRequest{done: make(chan error, 1)}
	s.pendingMu.Lock()
	s.nextID++
	requestID := strconv.Itoa(s.nextID)
	s.games[requestID] = req
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.games, requestID)
		s.pendingMu.Unlock()
	}()

	// The request is registered before the send so a fast reply cannot
	// arrive with nobody waiting for it.
	if err := s.send(Command{Command: CommandGetGames, RequestID: requestID, Details: details}); err != nil {
		return nil, err
	}

	select {
	case err := <-req.done:
		if err != nil {
			return nil, err
		}
		// The reader stops touching the request once it has signalled.
		return req.games, nil
	case <-s.clock.After(timeout):
		return nil, fmt.Errorf("timed out after %s waiting for the Playnite library", timeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for the Playnite library: %w", ctx.Err())
	}
}

// Launch asks the extension to start a game and waits for it to say whether
// Playnite accepted the request. The game starting is reported separately.
func (s *Server) Launch(ctx context.Context, gameID string, timeout time.Duration) error {
	result, err := s.request(ctx, s.launches, Command{Command: CommandLaunch, ID: gameID}, timeout)
	if err != nil {
		return err
	}
	if result.Status != StatusCompleted {
		return resultError("launch", &result)
	}
	return nil
}

// StopGame asks the extension to end a running game and waits for the result.
func (s *Server) StopGame(ctx context.Context, gameID string, timeout time.Duration) error {
	result, err := s.request(ctx, s.stops, Command{Command: CommandStop, ID: gameID}, timeout)
	if err != nil {
		return err
	}
	switch result.Status {
	case StatusCompleted:
		return nil
	case StatusUnsupported:
		return ErrStopUnsupported
	default:
		return resultError("stop", &result)
	}
}

func resultError(action string, result *Event) error {
	if result.Error != "" {
		return fmt.Errorf("%s through Playnite failed: %s", action, result.Error)
	}
	return fmt.Errorf("%s through Playnite reported status %q", action, result.Status)
}

// request sends a command about one game and waits for its result event.
// Only one request per game may be outstanding in each map.
func (s *Server) request(
	ctx context.Context, pending map[string]chan Event, cmd Command, timeout time.Duration,
) (Event, error) {
	result := make(chan Event, 1)
	s.pendingMu.Lock()
	if _, busy := pending[cmd.ID]; busy {
		s.pendingMu.Unlock()
		return Event{}, fmt.Errorf("a %s request to Playnite for this game is already in flight", cmd.Command)
	}
	pending[cmd.ID] = result
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(pending, cmd.ID)
		s.pendingMu.Unlock()
	}()

	if err := s.send(cmd); err != nil {
		return Event{}, err
	}

	select {
	case event := <-result:
		return event, nil
	case <-s.clock.After(timeout):
		return Event{}, fmt.Errorf("timed out after %s waiting for Playnite to answer %s", timeout, cmd.Command)
	case <-ctx.Done():
		return Event{}, fmt.Errorf("waiting for Playnite to answer %s: %w", cmd.Command, ctx.Err())
	}
}

// deliver hands a result to the request waiting on it, if any. The send never
// blocks: this runs on the reader, and a duplicate result must not wedge it.
func (s *Server) deliver(pending map[string]chan Event, event *Event) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	result, ok := pending[event.ID]
	if !ok {
		return
	}
	select {
	case result <- *event:
	default:
		log.Debug().Str("id", event.ID).Str("event", event.Event).Msg("discarding duplicate Playnite result")
	}
}

// send writes one command to the extension.
func (s *Server) send(cmd Command) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("encode Playnite %s command: %w", cmd.Command, err)
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return ErrNotConnected
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		log.Debug().Err(err).Msg("setting Playnite pipe write deadline")
	}
	if _, err := s.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write Playnite %s command: %w", cmd.Command, err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("write Playnite %s command: %w", cmd.Command, err)
	}
	return nil
}

func (s *Server) stopping() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Server) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.stopping() || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Warn().Err(err).Msg("failed to accept Playnite pipe connection")
			// A listener that fails every accept must not spin.
			select {
			case <-s.done:
				return
			case <-s.clock.After(time.Second):
			}
			continue
		}

		s.connMu.Lock()
		if s.stopping() {
			s.connMu.Unlock()
			_ = conn.Close()
			return
		}
		previous := s.conn
		s.conn = conn
		s.writer = bufio.NewWriter(conn)
		s.connMu.Unlock()
		if previous != nil {
			// Closing it ends its reader, which then finds it is no longer
			// the current connection and leaves the new one alone.
			_ = previous.Close()
		}
		log.Info().Msg("Playnite extension connected")

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveConn(conn)
		}()
	}
}

// serveConn reads events from one connection until it closes, pinging it in
// the meantime.
func (s *Server) serveConn(conn net.Conn) {
	readDone := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(readDone)
		s.read(conn)
	}()

	ticker := s.clock.NewTicker(pingInterval)
	defer ticker.Stop()
	for running := true; running; {
		select {
		case <-readDone:
			running = false
		case <-s.done:
			running = false
		case <-ticker.Chan():
			if err := s.send(Command{Command: CommandPing}); err != nil {
				log.Debug().Err(err).Msg("Playnite extension ping failed")
				running = false
			}
		}
	}

	_ = conn.Close()
	<-readDone

	s.connMu.Lock()
	current := s.conn == conn
	if current {
		s.conn = nil
		s.writer = nil
	}
	s.connMu.Unlock()
	if !current {
		return
	}
	log.Info().Msg("Playnite extension disconnected")
	s.failPending()
	if s.handlers.Disconnected != nil {
		s.handlers.Disconnected()
	}
}

func (s *Server) read(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), MaxLineSize)
	for scanner.Scan() {
		if s.stopping() {
			return
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		event, err := ParseEvent(line)
		if err != nil {
			log.Warn().Err(err).Msg("ignoring malformed Playnite event")
			continue
		}
		s.handle(&event)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, io.ErrClosedPipe) {
		log.Warn().Err(err).Msg("error reading from Playnite pipe")
	}
}

// failPending releases every request that was waiting on the lost connection.
func (s *Server) failPending() {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.hello = Event{}
	for _, req := range s.games {
		select {
		case req.done <- errDisconnected:
		default:
		}
	}
	failed := Event{Status: StatusFailed, Error: errDisconnected.Error()}
	for _, pending := range []map[string]chan Event{s.launches, s.stops} {
		for _, result := range pending {
			select {
			case result <- failed:
			default:
			}
		}
	}
}

func (s *Server) handle(event *Event) {
	switch event.Event {
	case EventHello:
		s.pendingMu.Lock()
		s.hello = *event
		s.pendingMu.Unlock()
		log.Info().
			Str("pluginVersion", event.PluginVersion).
			Str("playniteVersion", event.PlayniteVersion).
			Str("mode", event.Mode).
			Int("protocolVersion", event.ProtocolVersion).
			Msg("Playnite extension handshake received")
	case EventGames:
		s.handleGames(event)
	case EventLaunchResult:
		s.deliver(s.launches, event)
	case EventMediaStopResult:
		s.deliver(s.stops, event)
	case EventMediaStarted:
		if s.handlers.Started != nil {
			s.handlers.Started(event)
		}
	case EventMediaStopped:
		if s.handlers.Stopped != nil {
			s.handlers.Stopped(event)
		}
	case EventWrite:
		if s.handlers.Write != nil {
			s.handlers.Write(event.ID, event.Name)
		}
	case EventError:
		s.handleError(event)
	default:
		log.Debug().Str("event", event.Event).Msg("unknown Playnite event")
	}
}

// handleGames adds a chunk to the request it answers and completes the
// request on the final chunk or on an error.
func (s *Server) handleGames(event *Event) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	req, ok := s.games[event.RequestID]
	if !ok {
		return
	}
	var result error
	switch {
	case event.Error != "":
		result = fmt.Errorf("library request to Playnite failed: %s", event.Error)
	case len(req.games)+len(event.Games) > maxLibraryGames:
		result = fmt.Errorf("library from Playnite exceeds %d games", maxLibraryGames)
	default:
		req.games = append(req.games, event.Games...)
		if !event.Final {
			return
		}
	}
	// Forget the request first so a chunk sent after the last one cannot
	// append to a slice the caller already owns.
	delete(s.games, event.RequestID)
	select {
	case req.done <- result:
	default:
	}
}

// handleError routes a rejected command to whoever is waiting on it, so a
// refusal does not leave the caller blocked until its timeout.
func (s *Server) handleError(event *Event) {
	log.Warn().Str("command", event.Command).Str("id", event.ID).Str("error", event.Error).
		Msg("Playnite extension reported a command failure")
	reason := event.Error
	if reason == "" {
		reason = "the extension rejected the command"
	}
	failed := Event{Event: event.Event, ID: event.ID, Status: StatusFailed, Error: reason}
	switch event.Command {
	case CommandLaunch:
		s.deliver(s.launches, &failed)
	case CommandStop:
		s.deliver(s.stops, &failed)
	case CommandGetGames:
		s.handleGames(&Event{RequestID: event.RequestID, Error: reason})
	}
}
