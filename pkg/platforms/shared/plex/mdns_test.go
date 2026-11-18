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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func mustName(t testing.TB, s string) dnsmessage.Name {
	t.Helper()
	n, err := dnsmessage.NewName(s)
	require.NoError(t, err)
	return n
}

// castAnswer builds an mDNS response describing two Cast devices: one with
// an A record and one without, plus an unrelated service.
func castAnswer(t testing.TB) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	b.EnableCompression()
	require.NoError(t, b.StartAnswers())
	hdr := func(name string, typ dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: mustName(t, name), Type: typ, Class: dnsmessage.ClassINET, TTL: 120}
	}
	require.NoError(t, b.PTRResource(hdr(castService, dnsmessage.TypePTR),
		dnsmessage.PTRResource{PTR: mustName(t, "Chromecast-abc."+castService)}))
	require.NoError(t, b.PTRResource(hdr(castService, dnsmessage.TypePTR),
		dnsmessage.PTRResource{PTR: mustName(t, "Speaker-def."+castService)}))
	require.NoError(t, b.PTRResource(hdr("_airplay._tcp.local.", dnsmessage.TypePTR),
		dnsmessage.PTRResource{PTR: mustName(t, "Mac._airplay._tcp.local.")}))
	require.NoError(t, b.StartAdditionals())
	require.NoError(t, b.SRVResource(hdr("Chromecast-abc."+castService, dnsmessage.TypeSRV),
		dnsmessage.SRVResource{Target: mustName(t, "abc.local."), Port: 8009}))
	require.NoError(t, b.TXTResource(hdr("Chromecast-abc."+castService, dnsmessage.TypeTXT),
		dnsmessage.TXTResource{TXT: []string{"id=abc123", "md=Chromecast", "fn=Living Room TV", "bogus"}}))
	require.NoError(t, b.AResource(hdr("abc.local.", dnsmessage.TypeA),
		dnsmessage.AResource{A: [4]byte{192, 168, 1, 50}}))
	require.NoError(t, b.SRVResource(hdr("Speaker-def."+castService, dnsmessage.TypeSRV),
		dnsmessage.SRVResource{Target: mustName(t, "def.local."), Port: 8010}))
	require.NoError(t, b.TXTResource(hdr("Speaker-def."+castService, dnsmessage.TypeTXT),
		dnsmessage.TXTResource{TXT: []string{"fn=Kitchen", "id=def456"}}))
	msg, err := b.Finish()
	require.NoError(t, err)
	return msg
}

func TestParseCastResponse(t *testing.T) {
	t.Parallel()
	devices, err := parseCastResponse(castAnswer(t), net.IPv4(192, 168, 1, 60))
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, castDevice{
		Name: "Living Room TV", ID: "abc123", Model: "Chromecast", Host: "192.168.1.50", Port: 8009,
	}, devices[0])
	assert.Equal(t, castDevice{
		Name: "Kitchen", ID: "def456", Host: "192.168.1.60", Port: 8010,
	}, devices[1], "a device with no A record is reached at the sender's address")
}

func TestParseCastResponseIgnoresQueries(t *testing.T) {
	t.Parallel()
	query, err := buildCastQuery()
	require.NoError(t, err)
	devices, err := parseCastResponse(query, nil)
	require.NoError(t, err)
	assert.Empty(t, devices)

	_, err = parseCastResponse([]byte{1, 2, 3}, nil)
	require.Error(t, err)
}

// TestBrowseCastLoopback covers the same flow over a real UDP socket.
func TestBrowseCastLoopback(t *testing.T) {
	t.Parallel()
	var lc net.ListenConfig
	responder, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = responder.Close() })
	conn, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	answer := castAnswer(t)
	go func() {
		buf := make([]byte, maxMDNSPacket)
		n, from, rerr := responder.ReadFrom(buf)
		if rerr != nil {
			return
		}
		var p dnsmessage.Parser
		if _, perr := p.Start(buf[:n]); perr != nil {
			return
		}
		q, qerr := p.Question()
		if qerr != nil || q.Type != dnsmessage.TypePTR || q.Name.String() != castService {
			return
		}
		_, _ = responder.WriteTo([]byte("garbage"), from)
		_, _ = responder.WriteTo(answer, from)
		_, _ = responder.WriteTo(answer, from)
	}()

	devices, err := browseCast(context.Background(), conn, responder.LocalAddr(), 500*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, devices, 2, "repeated answers are merged")
	assert.Equal(t, "Kitchen", devices[0].Name)
	assert.Equal(t, "127.0.0.1", devices[0].Host)
	assert.Equal(t, "Living Room TV", devices[1].Name)
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// scriptedConn is a net.PacketConn that delivers a fixed list of datagrams
// and then reports a read timeout.
type scriptedConn struct {
	from    net.Addr
	reads   [][]byte
	written [][]byte
}

func (c *scriptedConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if len(c.reads) == 0 {
		return 0, nil, timeoutError{}
	}
	n := copy(p, c.reads[0])
	c.reads = c.reads[1:]
	return n, c.from, nil
}

func (c *scriptedConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.written = append(c.written, append([]byte(nil), p...))
	return len(p), nil
}

func (*scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr            { return c.from }
func (*scriptedConn) SetDeadline(time.Time) error      { return nil }
func (*scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (*scriptedConn) SetWriteDeadline(time.Time) error { return nil }

func TestBrowseCastScripted(t *testing.T) {
	t.Parallel()
	answer := castAnswer(t)
	conn := &scriptedConn{
		from:  &net.UDPAddr{IP: net.IPv4(192, 168, 1, 60), Port: 5353},
		reads: [][]byte{[]byte("garbage"), answer, answer},
	}
	devices, err := browseCast(context.Background(), conn, mdnsGroup, time.Second)
	require.NoError(t, err)
	require.Len(t, conn.written, 1, "one query is sent")
	query, err := buildCastQuery()
	require.NoError(t, err)
	assert.Equal(t, query, conn.written[0])
	assert.Empty(t, conn.reads, "every datagram is read before the timeout")
	require.Len(t, devices, 2, "a malformed datagram is skipped and a repeated answer is merged")
	assert.Equal(t, castDevice{Name: "Kitchen", ID: "def456", Host: "192.168.1.60", Port: 8010}, devices[0])
	assert.Equal(t, "Living Room TV", devices[1].Name)
}

func TestBrowseCastStopsOnCancel(t *testing.T) {
	t.Parallel()
	var lc net.ListenConfig
	conn, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	devices, err := browseCast(ctx, conn, conn.LocalAddr(), time.Hour)
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func FuzzParseCastResponse(f *testing.F) {
	f.Add(castAnswer(f))
	query, _ := buildCastQuery()
	f.Add(query)
	f.Add([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, msg []byte) {
		devices, err := parseCastResponse(msg, net.IPv4(10, 0, 0, 1))
		if err != nil {
			return
		}
		for _, d := range devices {
			if d.Name == "" || d.Host == "" || d.Port <= 0 {
				t.Fatalf("incomplete device %+v", d)
			}
		}
	})
}
