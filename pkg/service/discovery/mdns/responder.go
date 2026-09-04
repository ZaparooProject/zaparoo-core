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

// Package mdns advertises a single DNS-SD service over multicast DNS.
//
// It exists because grandcat/zeroconf picks the outgoing interface with a
// per-packet control message, and golang.org/x/net leaves control messages
// unimplemented on Windows (ipv4/control_windows.go, ipv4/sys_windows.go).
// Every announcement and response there leaves by whichever interface the
// routing table happens to pick, so only one NIC on a multi-homed Windows
// machine is ever discoverable. This package selects the interface with
// IP_MULTICAST_IF instead, which x/net does support on Windows, and gives each
// interface its own address records so a client is never handed an address on
// a network it cannot reach.
package mdns

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// Port is the mDNS port. RFC 6762 section 5.
	Port = 5353

	// multicastHops is the TTL/hop limit mandated by RFC 6762 section 11.
	multicastHops = 255

	// announceCount is how many unsolicited announcements are sent when a
	// service or interface appears. RFC 6762 section 8.3 asks for at least
	// two, one second apart.
	announceCount = 2

	// announceInterval is the gap between those announcements.
	announceInterval = time.Second

	// maxPacketSize bounds a single read. RFC 6762 section 17 caps an mDNS
	// message at the interface MTU, but jumbo frames and IP fragmentation
	// make a generous buffer cheaper than a truncated parse.
	maxPacketSize = 9000
)

var (
	groupIPv4 = net.IPv4(224, 0, 0, 251)
	groupIPv6 = net.ParseIP("ff02::fb")

	// Binding to the group address rather than the wildcard keeps unicast
	// traffic on port 5353 with whichever other mDNS stack is running on
	// this host, instead of competing for it.
	listenAddrIPv4 = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 0), Port: Port}
	listenAddrIPv6 = &net.UDPAddr{IP: net.ParseIP("ff02::"), Port: Port}

	sendAddrIPv4 = &net.UDPAddr{IP: groupIPv4, Port: Port}
	sendAddrIPv6 = &net.UDPAddr{IP: groupIPv6, Port: Port}

	// ErrNoInterfaces means there was nothing to advertise on.
	ErrNoInterfaces = errors.New("no usable network interfaces for mDNS")
	// ErrNoSockets means neither address family could be opened.
	ErrNoSockets = errors.New("could not open an mDNS socket on any address family")
)

// link is one network interface the responder advertises on.
type link struct {
	name     string
	addrs    linkAddrs
	prefixes []*net.IPNet
	iface    net.Interface
	index    int
}

// Responder advertises one DNS-SD service on a set of interfaces.
type Responder struct {
	v4      *ipv4.PacketConn
	v6      *ipv6.PacketConn
	done    chan struct{}
	logf    func(format string, args ...any)
	links   []link
	svc     Service
	wg      sync.WaitGroup
	mu      syncutil.Mutex
	started bool
	stopped bool
}

// New creates a responder for svc. logf, if set, receives non-fatal socket
// errors; the caller decides how loud they should be.
func New(svc *Service, logf func(format string, args ...any)) *Responder {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Responder{
		svc:  *svc,
		logf: logf,
		done: make(chan struct{}),
	}
}

// Start opens sockets, joins the mDNS groups on every usable interface in
// ifaces, and announces the service.
func (r *Responder) Start(ifaces []net.Interface) error {
	links := buildLinks(ifaces)
	if len(links) == 0 {
		return ErrNoInterfaces
	}

	conn4, err4 := listenIPv4(links)
	conn6, err6 := listenIPv6(links)
	if conn4 == nil && conn6 == nil {
		//nolint:errorlint // both causes are reported together for diagnosis
		return fmt.Errorf("%w: ipv4: %v, ipv6: %v", ErrNoSockets, err4, err6)
	}

	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		closeConns(conn4, conn6)
		return ErrNoSockets
	}
	r.v4, r.v6 = conn4, conn6
	r.links = links
	r.started = true
	// Counted under the lock so that a Stop racing with Start cannot reach
	// wg.Wait() while the counter is still zero.
	if conn4 != nil {
		r.wg.Add(1)
	}
	if conn6 != nil {
		r.wg.Add(1)
	}
	r.mu.Unlock()

	if conn4 != nil {
		go r.receiveIPv4(conn4)
	}
	if conn6 != nil {
		go r.receiveIPv6(conn6)
	}

	r.announce(links)

	return nil
}

// Interfaces returns the names of the interfaces currently advertised on.
func (r *Responder) Interfaces() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, len(r.links))
	for i := range r.links {
		names[i] = r.links[i].name
	}
	return names
}

// SetInterfaces reconciles the advertised interface set with ifaces. Interfaces
// that went away get a goodbye, new ones get joined and announced, and ones
// whose addresses changed get re-announced. It is the mechanism that makes a
// cable plugged in after startup discoverable without restarting Core.
func (r *Responder) SetInterfaces(ifaces []net.Interface) (changed bool, err error) {
	next := buildLinks(ifaces)

	r.mu.Lock()
	if !r.started || r.stopped {
		r.mu.Unlock()
		return false, ErrNoSockets
	}
	previous := r.links
	added, removed, readdressed := diffLinks(previous, next)
	if len(added) == 0 && len(removed) == 0 && len(readdressed) == 0 {
		r.mu.Unlock()
		return false, nil
	}
	conn4, conn6 := r.v4, r.v6
	r.mu.Unlock()

	// Say goodbye while the group membership is still in place, otherwise
	// the packet has no interface to leave by.
	if len(removed) > 0 {
		r.sendRecords(removed, func(l link) []dns.RR {
			return r.svc.lookupAnswer(l.addrs, 0, true)
		})
		for i := range removed {
			leaveGroups(conn4, conn6, &removed[i])
		}
	}

	for i := range added {
		joinGroups(conn4, conn6, &added[i], r.logf)
	}

	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return true, nil
	}
	r.links = next
	r.mu.Unlock()

	announceOn := make([]link, 0, len(added)+len(readdressed))
	announceOn = append(announceOn, added...)
	announceOn = append(announceOn, readdressed...)
	if len(announceOn) > 0 {
		r.announce(announceOn)
	}

	return true, nil
}

// Stop sends a goodbye for every interface and closes the sockets.
func (r *Responder) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	links := r.links
	conn4, conn6 := r.v4, r.v6
	started := r.started
	r.mu.Unlock()

	close(r.done)

	if started {
		r.sendRecords(links, func(l link) []dns.RR {
			return r.svc.lookupAnswer(l.addrs, 0, true)
		})
	}

	closeConns(conn4, conn6)
	r.wg.Wait()

	r.mu.Lock()
	r.v4, r.v6 = nil, nil
	r.links = nil
	r.mu.Unlock()
}

// announce sends the unsolicited announcements RFC 6762 section 8.3 requires.
func (r *Responder) announce(links []link) {
	snapshot := make([]link, len(links))
	copy(snapshot, links)

	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		for i := range announceCount {
			if i > 0 {
				timer := time.NewTimer(announceInterval)
				select {
				case <-timer.C:
				case <-r.done:
					timer.Stop()
					return
				}
			}
			r.sendRecords(snapshot, func(l link) []dns.RR {
				return r.svc.lookupAnswer(l.addrs, serviceTTL, true)
			})
		}
	}()
}

// sendRecords multicasts one message per interface, each carrying that
// interface's own address records.
func (r *Responder) sendRecords(links []link, build func(link) []dns.RR) {
	for i := range links {
		msg := new(dns.Msg)
		msg.Response = true
		msg.Authoritative = true
		msg.Compress = true
		msg.Answer = build(links[i])
		r.writeMulticast(msg, &links[i])
	}
}

// writeMulticast sends msg out a single interface. IP_MULTICAST_IF is what
// makes this work on Windows, where the per-packet interface control message
// x/net would otherwise use is unimplemented.
func (r *Responder) writeMulticast(msg *dns.Msg, l *link) {
	// RFC 6762 section 18.1: a multicast response carries a zero query id.
	msg.Id = 0

	buf, err := msg.Pack()
	if err != nil {
		r.logf("pack mDNS response for %s: %v", l.name, err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.v4 != nil && len(l.addrs.v4) > 0 {
		if err := r.v4.SetMulticastInterface(&l.iface); err != nil {
			r.logf("select ipv4 multicast interface %s: %v", l.name, err)
		} else if _, err := r.v4.WriteTo(buf, nil, sendAddrIPv4); err != nil {
			r.logf("send ipv4 mDNS on %s: %v", l.name, err)
		}
	}

	if r.v6 != nil && len(l.addrs.v6) > 0 {
		if err := r.v6.SetMulticastInterface(&l.iface); err != nil {
			r.logf("select ipv6 multicast interface %s: %v", l.name, err)
		} else if _, err := r.v6.WriteTo(buf, nil, sendAddrIPv6); err != nil {
			r.logf("send ipv6 mDNS on %s: %v", l.name, err)
		}
	}
}

func (r *Responder) writeUnicast(msg *dns.Msg, to net.Addr) {
	buf, err := msg.Pack()
	if err != nil {
		r.logf("pack unicast mDNS response: %v", err)
		return
	}

	udpAddr, ok := to.(*net.UDPAddr)
	if !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if udpAddr.IP.To4() != nil {
		if r.v4 != nil {
			_, _ = r.v4.WriteTo(buf, nil, to)
		}
		return
	}
	if r.v6 != nil {
		_, _ = r.v6.WriteTo(buf, nil, to)
	}
}

func (r *Responder) receiveIPv4(conn *ipv4.PacketConn) {
	defer r.wg.Done()

	buf := make([]byte, maxPacketSize)
	for {
		n, _, from, err := conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		r.handlePacket(buf[:n], from)
	}
}

func (r *Responder) receiveIPv6(conn *ipv6.PacketConn) {
	defer r.wg.Done()

	buf := make([]byte, maxPacketSize)
	for {
		n, _, from, err := conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		r.handlePacket(buf[:n], from)
	}
}

func (r *Responder) handlePacket(packet []byte, from net.Addr) {
	msg := new(dns.Msg)
	if err := msg.Unpack(packet); err != nil {
		return
	}
	if msg.Response || len(msg.Question) == 0 {
		return
	}
	// A query carrying an authority section is a probe for conflict
	// detection. This responder does not participate in probing.
	if len(msg.Ns) > 0 {
		return
	}

	r.mu.Lock()
	links := r.links
	stopped := r.stopped
	r.mu.Unlock()
	if stopped || len(links) == 0 {
		return
	}

	for _, question := range msg.Question {
		r.answerQuestion(question, msg, links, from)
	}
}

func (r *Responder) answerQuestion(q dns.Question, query *dns.Msg, links []link, from net.Addr) {
	if q.Qclass&^cacheFlush != dns.ClassINET && q.Qclass&^cacheFlush != dns.ClassANY {
		return
	}

	if wantsUnicast(q) {
		l := linkFor(links, from)
		answer, extra := r.svc.answersFor(q, query, l.addrs)
		if len(answer) == 0 {
			return
		}
		reply := newReply(query.Id, answer, extra)
		r.writeUnicast(reply, from)
		return
	}

	for i := range links {
		answer, extra := r.svc.answersFor(q, query, links[i].addrs)
		if len(answer) == 0 {
			continue
		}
		reply := newReply(0, answer, extra)
		r.writeMulticast(reply, &links[i])
	}
}

// answersFor returns the answer and additional sections for one question, or
// nothing if the question is not ours.
func (s *Service) answersFor(q dns.Question, query *dns.Msg, addrs linkAddrs) (answer, extra []dns.RR) {
	switch {
	case equalName(q.Name, enumerationName):
		if q.Qtype != dns.TypePTR && q.Qtype != dns.TypeANY {
			return nil, nil
		}
		return []dns.RR{s.enumerationPTR(serviceTTL)}, nil

	case equalName(q.Name, s.typeName()):
		if q.Qtype != dns.TypePTR && q.Qtype != dns.TypeANY {
			return nil, nil
		}
		answer, extra = s.browsingAnswer(addrs)
		if isKnownAnswer(answer, query) {
			return nil, nil
		}
		return answer, extra

	case equalName(q.Name, s.instanceName()):
		switch q.Qtype {
		case dns.TypeSRV, dns.TypeTXT, dns.TypeANY:
			return s.lookupAnswer(addrs, serviceTTL, false), nil
		default:
			return nil, nil
		}

	case equalName(q.Name, s.hostName()):
		switch q.Qtype {
		case dns.TypeA, dns.TypeAAAA:
			return s.hostAnswer(addrs, q.Qtype), nil
		case dns.TypeANY:
			return s.addrRecords(addrs, hostTTL, true), nil
		default:
			return nil, nil
		}
	}

	return nil, nil
}

func newReply(id uint16, answer, extra []dns.RR) *dns.Msg {
	msg := new(dns.Msg)
	msg.Id = id
	msg.Response = true
	msg.Authoritative = true
	msg.Compress = true
	// RFC 6762 section 6: responses must not repeat the question.
	msg.Question = nil
	msg.Answer = answer
	msg.Extra = extra
	return msg
}

// linkFor picks the interface a unicast reply should describe, by matching the
// requester's address against each interface's prefixes. Falling back to the
// first link keeps a reply going out rather than none at all.
func linkFor(links []link, from net.Addr) link {
	udpAddr, ok := from.(*net.UDPAddr)
	if ok {
		if udpAddr.Zone != "" {
			for i := range links {
				if links[i].name == udpAddr.Zone {
					return links[i]
				}
			}
		}
		for i := range links {
			for _, prefix := range links[i].prefixes {
				if prefix.Contains(udpAddr.IP) {
					return links[i]
				}
			}
		}
	}
	return links[0]
}

func equalName(a, b string) bool {
	return dns.CanonicalName(a) == dns.CanonicalName(b)
}

// buildLinks turns interfaces into links, dropping any with no address worth
// advertising.
func buildLinks(ifaces []net.Interface) []link {
	links := make([]link, 0, len(ifaces))
	for i := range ifaces {
		iface := ifaces[i]
		v4, v6, err := InterfaceAddrs(&iface)
		if err != nil {
			continue
		}
		addrs := linkAddrs{v4: v4, v6: v6}
		if addrs.empty() {
			continue
		}
		links = append(links, link{
			index:    iface.Index,
			name:     iface.Name,
			iface:    iface,
			addrs:    addrs,
			prefixes: interfacePrefixes(&iface),
		})
	}
	return links
}

func interfacePrefixes(iface *net.Interface) []*net.IPNet {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	prefixes := make([]*net.IPNet, 0, len(addrs))
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			prefixes = append(prefixes, ipNet)
		}
	}
	return prefixes
}

// diffLinks reports which links appeared, disappeared, or kept their identity
// but changed addresses.
func diffLinks(previous, next []link) (added, removed, readdressed []link) {
	byIndex := make(map[int]int, len(previous))
	for i := range previous {
		byIndex[previous[i].index] = i
	}

	seen := make(map[int]struct{}, len(next))
	for i := range next {
		seen[next[i].index] = struct{}{}
		old, existed := byIndex[next[i].index]
		switch {
		case !existed:
			added = append(added, next[i])
		case !sameAddrs(previous[old].addrs, next[i].addrs):
			readdressed = append(readdressed, next[i])
		}
	}

	for i := range previous {
		if _, ok := seen[previous[i].index]; !ok {
			removed = append(removed, previous[i])
		}
	}

	return added, removed, readdressed
}

func sameAddrs(a, b linkAddrs) bool {
	return sameIPs(a.v4, b.v4) && sameIPs(a.v6, b.v6)
}

func sameIPs(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func listenIPv4(links []link) (*ipv4.PacketConn, error) {
	udpConn, err := net.ListenUDP("udp4", listenAddrIPv4)
	if err != nil {
		return nil, fmt.Errorf("listen udp4: %w", err)
	}

	conn := ipv4.NewPacketConn(udpConn)
	if err := conn.SetMulticastTTL(multicastHops); err != nil {
		// Not fatal: the default TTL of 1 still reaches the local link.
		_ = err
	}

	joined := 0
	for i := range links {
		if len(links[i].addrs.v4) == 0 {
			continue
		}
		if err := conn.JoinGroup(&links[i].iface, &net.UDPAddr{IP: groupIPv4}); err == nil {
			joined++
		}
	}
	if joined == 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("udp4: %w", ErrNoInterfaces)
	}

	return conn, nil
}

func listenIPv6(links []link) (*ipv6.PacketConn, error) {
	udpConn, err := net.ListenUDP("udp6", listenAddrIPv6)
	if err != nil {
		return nil, fmt.Errorf("listen udp6: %w", err)
	}

	conn := ipv6.NewPacketConn(udpConn)
	if err := conn.SetMulticastHopLimit(multicastHops); err != nil {
		_ = err
	}

	joined := 0
	for i := range links {
		if len(links[i].addrs.v6) == 0 {
			continue
		}
		if err := conn.JoinGroup(&links[i].iface, &net.UDPAddr{IP: groupIPv6}); err == nil {
			joined++
		}
	}
	if joined == 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("udp6: %w", ErrNoInterfaces)
	}

	return conn, nil
}

func joinGroups(conn4 *ipv4.PacketConn, conn6 *ipv6.PacketConn, l *link, logf func(string, ...any)) {
	if conn4 != nil && len(l.addrs.v4) > 0 {
		if err := conn4.JoinGroup(&l.iface, &net.UDPAddr{IP: groupIPv4}); err != nil {
			logf("join ipv4 mDNS group on %s: %v", l.name, err)
		}
	}
	if conn6 != nil && len(l.addrs.v6) > 0 {
		if err := conn6.JoinGroup(&l.iface, &net.UDPAddr{IP: groupIPv6}); err != nil {
			logf("join ipv6 mDNS group on %s: %v", l.name, err)
		}
	}
}

func leaveGroups(conn4 *ipv4.PacketConn, conn6 *ipv6.PacketConn, l *link) {
	if conn4 != nil && len(l.addrs.v4) > 0 {
		_ = conn4.LeaveGroup(&l.iface, &net.UDPAddr{IP: groupIPv4})
	}
	if conn6 != nil && len(l.addrs.v6) > 0 {
		_ = conn6.LeaveGroup(&l.iface, &net.UDPAddr{IP: groupIPv6})
	}
}

func closeConns(conn4 *ipv4.PacketConn, conn6 *ipv6.PacketConn) {
	if conn4 != nil {
		_ = conn4.Close()
	}
	if conn6 != nil {
		_ = conn6.Close()
	}
}
