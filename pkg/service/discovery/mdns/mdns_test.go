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

package mdns

import (
	"math"
	"net"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phantomIfaceIndex is an interface index no host can have, so net.Interface
// Addrs() finds nothing for it and the interface has no address to advertise.
// Index 0 does not work: BSD, and so macOS, reads a zero index as "no filter"
// and hands back every address on the machine, which made a phantom built with
// it come back fully addressed there while returning nothing on Linux and
// Windows.
const phantomIfaceIndex = math.MaxInt32

func testService() Service {
	return Service{
		Instance: "DESKTOP-I500VAN",
		Type:     "_zaparoo._tcp",
		Host:     "DESKTOP-I500VAN",
		Port:     7497,
		Text:     []string{""},
	}
}

func testAddrs() linkAddrs {
	return linkAddrs{
		v4: []net.IP{net.ParseIP("10.0.0.241").To4()},
		v6: []net.IP{net.ParseIP("2403:580e:2024::1")},
	}
}

func ipNet(cidr string) *net.IPNet {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return network
}

func TestSelectAddrsDropsIPv4LinkLocal(t *testing.T) {
	t.Parallel()

	// A Windows adapter that is up but not connected keeps an APIPA address.
	// Advertising it sends clients somewhere that cannot answer.
	v4, v6 := selectAddrs([]net.Addr{
		ipNet("169.254.194.210/16"),
		ipNet("10.0.0.241/24"),
	})

	require.Len(t, v4, 1)
	assert.Equal(t, "10.0.0.241", v4[0].String())
	assert.Empty(t, v6)
}

func TestSelectAddrsDropsAPIPAOnlyInterface(t *testing.T) {
	t.Parallel()

	v4, v6 := selectAddrs([]net.Addr{ipNet("169.254.67.14/16")})

	assert.Empty(t, v4)
	assert.Empty(t, v6)
	assert.True(t, linkAddrs{v4: v4, v6: v6}.empty())
}

func TestSelectAddrsPrefersGlobalIPv6(t *testing.T) {
	t.Parallel()

	_, v6 := selectAddrs([]net.Addr{
		ipNet("fe80::d2e8:f3c7:2af0:9cd4/64"),
		ipNet("2403:580e:2024::1/64"),
		ipNet("fd89:8088:c6b4::1/64"),
	})

	require.Len(t, v6, 2, "link-local is dropped when routable addresses exist")
	assert.Equal(t, "2403:580e:2024::1", v6[0].String())
	assert.Equal(t, "fd89:8088:c6b4::1", v6[1].String())
}

func TestSelectAddrsFallsBackToIPv6LinkLocal(t *testing.T) {
	t.Parallel()

	_, v6 := selectAddrs([]net.Addr{ipNet("fe80::d2e8:f3c7:2af0:9cd4/64")})

	require.Len(t, v6, 1)
	assert.Equal(t, "fe80::d2e8:f3c7:2af0:9cd4", v6[0].String())
}

func TestSelectAddrsSkipsLoopbackAndDuplicates(t *testing.T) {
	t.Parallel()

	v4, v6 := selectAddrs([]net.Addr{
		ipNet("127.0.0.1/8"),
		ipNet("::1/128"),
		ipNet("10.0.0.241/24"),
		ipNet("10.0.0.241/24"),
	})

	require.Len(t, v4, 1)
	assert.Empty(t, v6)
}

func TestServiceNames(t *testing.T) {
	t.Parallel()

	svc := testService()

	assert.Equal(t, "_zaparoo._tcp.local.", svc.typeName())
	assert.Equal(t, "DESKTOP-I500VAN._zaparoo._tcp.local.", svc.instanceName())
	assert.Equal(t, "DESKTOP-I500VAN.local.", svc.hostName())
}

func TestInstanceNameEscapesDots(t *testing.T) {
	t.Parallel()

	// RFC 6763 4.1.1 allows dots in an instance name; they are a literal part
	// of the label, not a label separator.
	svc := testService()
	svc.Instance = "Callan's PC v1.2"

	name := svc.instanceName()
	assert.Equal(t, `Callan's PC v1\.2._zaparoo._tcp.local.`, name)

	packed, err := dns.PackDomainName(name, make([]byte, 256), 0, nil, false)
	require.NoError(t, err, "an escaped instance name must still pack")
	assert.Positive(t, packed)
}

func TestBrowsingAnswerCarriesEverythingNeededToConnect(t *testing.T) {
	t.Parallel()

	svc := testService()
	answer, extra := svc.browsingAnswer(testAddrs())

	require.Len(t, answer, 1)
	ptr, ok := answer[0].(*dns.PTR)
	require.True(t, ok)
	assert.Equal(t, "_zaparoo._tcp.local.", ptr.Hdr.Name)
	assert.Equal(t, "DESKTOP-I500VAN._zaparoo._tcp.local.", ptr.Ptr)
	assert.Equal(t, dns.ClassINET, int(ptr.Hdr.Class),
		"a PTR is a shared record and must not set the cache-flush bit")

	types := make([]uint16, len(extra))
	for i, record := range extra {
		types[i] = record.Header().Rrtype
	}
	assert.Equal(t, []uint16{dns.TypeSRV, dns.TypeTXT, dns.TypeA, dns.TypeAAAA}, types)
}

func TestLookupAnswerSetsCacheFlushOnUniqueRecords(t *testing.T) {
	t.Parallel()

	svc := testService()
	answer := svc.lookupAnswer(testAddrs(), serviceTTL, true)

	flushed := map[uint16]bool{}
	for _, record := range answer {
		flushed[record.Header().Rrtype] = record.Header().Class&cacheFlush != 0
	}

	assert.True(t, flushed[dns.TypeSRV])
	assert.True(t, flushed[dns.TypeTXT])
	assert.True(t, flushed[dns.TypeA])
	assert.False(t, flushed[dns.TypePTR], "shared PTR records are never cache-flushed")
}

func TestGoodbyeZeroesEveryTTL(t *testing.T) {
	t.Parallel()

	svc := testService()
	answer := svc.lookupAnswer(testAddrs(), 0, true)

	require.NotEmpty(t, answer)
	for _, record := range answer {
		assert.Zero(t, record.Header().Ttl,
			"a goodbye must expire address records too, not just service records")
	}
}

func TestAddrRecordsAreScopedToOneInterface(t *testing.T) {
	t.Parallel()

	svc := testService()
	wifi := linkAddrs{v4: []net.IP{net.ParseIP("10.0.0.241").To4()}}
	wired := linkAddrs{v4: []net.IP{net.ParseIP("192.168.1.50").To4()}}

	wifiRecords := svc.addrRecords(wifi, hostTTL, false)
	wiredRecords := svc.addrRecords(wired, hostTTL, false)

	require.Len(t, wifiRecords, 1)
	require.Len(t, wiredRecords, 1)
	assert.Equal(t, "10.0.0.241", wifiRecords[0].(*dns.A).A.String())
	assert.Equal(t, "192.168.1.50", wiredRecords[0].(*dns.A).A.String())
}

func TestResponsePacksValidTXTRecord(t *testing.T) {
	t.Parallel()

	svc := testService()
	answer, extra := svc.browsingAnswer(testAddrs())

	wire, err := newReply(0, answer, extra).Pack()
	require.NoError(t, err)

	unpacked := new(dns.Msg)
	require.NoError(t, unpacked.Unpack(wire),
		"a response with a zero-length TXT rdata fails to parse in strict resolvers")

	var txt *dns.TXT
	for _, record := range unpacked.Extra {
		if candidate, ok := record.(*dns.TXT); ok {
			txt = candidate
		}
	}
	require.NotNil(t, txt)
	assert.Equal(t, uint16(1), txt.Hdr.Rdlength,
		"TXT rdata must be one zero-length string, not an empty record")
}

func TestAnswersForRoutesByQuestion(t *testing.T) {
	t.Parallel()

	svc := testService()
	addrs := testAddrs()

	tests := []struct {
		name        string
		qname       string
		wantFirst   uint16
		qtype       uint16
		wantAnswers bool
	}{
		{"service type browse", "_zaparoo._tcp.local.", dns.TypePTR, dns.TypePTR, true},
		{"type enumeration", enumerationName, dns.TypePTR, dns.TypePTR, true},
		{"instance lookup", "DESKTOP-I500VAN._zaparoo._tcp.local.", dns.TypeSRV, dns.TypeSRV, true},
		{"host A", "DESKTOP-I500VAN.local.", dns.TypeA, dns.TypeA, true},
		{"host AAAA", "DESKTOP-I500VAN.local.", dns.TypeAAAA, dns.TypeAAAA, true},
		{"case insensitive", "_ZAPAROO._TCP.local.", dns.TypePTR, dns.TypePTR, true},
		{"someone else's service", "_http._tcp.local.", 0, dns.TypePTR, false},
		{"wrong type for browse", "_zaparoo._tcp.local.", 0, dns.TypeSRV, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			question := dns.Question{Name: tt.qname, Qtype: tt.qtype, Qclass: dns.ClassINET}
			answer, _ := svc.answersFor(question, new(dns.Msg), addrs)

			if !tt.wantAnswers {
				assert.Empty(t, answer)
				return
			}
			require.NotEmpty(t, answer)
			assert.Equal(t, tt.wantFirst, answer[0].Header().Rrtype)
		})
	}
}

func TestAnswersForSuppressesKnownAnswer(t *testing.T) {
	t.Parallel()

	svc := testService()
	question := dns.Question{Name: svc.typeName(), Qtype: dns.TypePTR, Qclass: dns.ClassINET}

	query := new(dns.Msg)
	query.Answer = []dns.RR{&dns.PTR{
		Hdr: dns.RR_Header{Name: svc.typeName(), Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: serviceTTL},
		Ptr: svc.instanceName(),
	}}

	answer, _ := svc.answersFor(question, query, testAddrs())
	assert.Empty(t, answer, "RFC 6762 7.1: stay quiet when the querier already knows")

	// A nearly expired known answer is not good enough to stay quiet about.
	query.Answer[0].Header().Ttl = serviceTTL/2 - 1
	answer, _ = svc.answersFor(question, query, testAddrs())
	assert.NotEmpty(t, answer)
}

func TestWantsUnicast(t *testing.T) {
	t.Parallel()

	assert.False(t, wantsUnicast(dns.Question{Qclass: dns.ClassINET}))
	assert.True(t, wantsUnicast(dns.Question{Qclass: dns.ClassINET | cacheFlush}))
}

func TestDiffLinks(t *testing.T) {
	t.Parallel()

	wifi := link{index: 22, name: "Wi-Fi", addrs: linkAddrs{v4: []net.IP{net.ParseIP("10.0.0.241").To4()}}}
	wired := link{index: 14, name: "Ethernet 4", addrs: linkAddrs{v4: []net.IP{net.ParseIP("192.168.1.50").To4()}}}
	wifiMoved := link{index: 22, name: "Wi-Fi", addrs: linkAddrs{v4: []net.IP{net.ParseIP("10.0.0.99").To4()}}}

	t.Run("cable plugged in", func(t *testing.T) {
		t.Parallel()
		added, removed, readdressed := diffLinks([]link{wifi}, []link{wifi, wired})
		require.Len(t, added, 1)
		assert.Equal(t, "Ethernet 4", added[0].name)
		assert.Empty(t, removed)
		assert.Empty(t, readdressed)
	})

	t.Run("cable pulled out", func(t *testing.T) {
		t.Parallel()
		added, removed, readdressed := diffLinks([]link{wifi, wired}, []link{wifi})
		assert.Empty(t, added)
		require.Len(t, removed, 1)
		assert.Equal(t, "Ethernet 4", removed[0].name)
		assert.Empty(t, readdressed)
	})

	t.Run("new dhcp lease", func(t *testing.T) {
		t.Parallel()
		added, removed, readdressed := diffLinks([]link{wifi}, []link{wifiMoved})
		assert.Empty(t, added)
		assert.Empty(t, removed)
		require.Len(t, readdressed, 1)
		assert.Equal(t, "Wi-Fi", readdressed[0].name)
	})

	t.Run("nothing changed", func(t *testing.T) {
		t.Parallel()
		added, removed, readdressed := diffLinks([]link{wifi, wired}, []link{wired, wifi})
		assert.Empty(t, added)
		assert.Empty(t, removed)
		assert.Empty(t, readdressed)
	})
}

func TestLinkForMatchesRequesterSubnet(t *testing.T) {
	t.Parallel()

	wifi := link{
		index: 22, name: "Wi-Fi",
		prefixes: []*net.IPNet{ipNet("10.0.0.241/24")},
	}
	wired := link{
		index: 14, name: "Ethernet 4",
		prefixes: []*net.IPNet{ipNet("192.168.1.50/24")},
	}
	links := []link{wifi, wired}

	assert.Equal(t, "Ethernet 4",
		linkFor(links, &net.UDPAddr{IP: net.ParseIP("192.168.1.7")}).name)
	assert.Equal(t, "Wi-Fi",
		linkFor(links, &net.UDPAddr{IP: net.ParseIP("10.0.0.167")}).name)
	assert.Equal(t, "Wi-Fi",
		linkFor(links, &net.UDPAddr{IP: net.ParseIP("172.16.0.1")}).name,
		"an unmatched requester still gets an answer rather than silence")
}

func TestLinkForUsesIPv6Zone(t *testing.T) {
	t.Parallel()

	links := []link{
		{index: 22, name: "Wi-Fi"},
		{index: 14, name: "Ethernet 4"},
	}

	from := &net.UDPAddr{IP: net.ParseIP("fe80::1"), Zone: "Ethernet 4"}
	assert.Equal(t, "Ethernet 4", linkFor(links, from).name)
}

func TestBuildLinksDropsInterfacesWithoutUsableAddresses(t *testing.T) {
	t.Parallel()

	// An interface with no address is dropped rather than advertised with none.
	links := buildLinks([]net.Interface{{Index: phantomIfaceIndex, Name: "phantom0"}})
	assert.Empty(t, links)
}

// twoLinks is a machine with a wired and a wireless interface on different
// subnets, which is the shape that exposed the original bug.
func twoLinks() []link {
	return []link{
		{
			index: 14, name: "Ethernet 4",
			addrs:    linkAddrs{v4: []net.IP{net.ParseIP("192.168.1.50").To4()}},
			prefixes: []*net.IPNet{ipNet("192.168.1.50/24")},
		},
		{
			index: 22, name: "Wi-Fi",
			addrs:    linkAddrs{v4: []net.IP{net.ParseIP("10.0.0.241").To4()}},
			prefixes: []*net.IPNet{ipNet("10.0.0.241/24")},
		},
	}
}

func browseQuery() *dns.Msg {
	query := new(dns.Msg)
	query.Id = 0x1234
	query.Question = []dns.Question{{
		Name:   "_zaparoo._tcp.local.",
		Qtype:  dns.TypePTR,
		Qclass: dns.ClassINET,
	}}
	return query
}

// mdnsPeer is a querier that is itself an mDNS responder, so it listens on the
// mDNS port and expects a multicast answer.
func mdnsPeer(ip string) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: Port}
}

// legacyPeer is an ordinary DNS client that happened to ask the multicast
// group from an ephemeral port. RFC 6762 section 6.7.
func legacyPeer(ip string) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: 51000}
}

func addrStrings(records []dns.RR) []string {
	var out []string
	for _, record := range records {
		if a, ok := record.(*dns.A); ok {
			out = append(out, a.A.String())
		}
	}
	return out
}

func TestPlanRepliesAnswersOnEveryInterface(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()

	replies := svc.planReplies(browseQuery(), links, mdnsPeer("10.0.0.167"))

	require.Len(t, replies, 2, "every interface answers, not just the one the OS would route by")
	byName := map[string][]string{}
	for _, reply := range replies {
		require.NotNil(t, reply.link)
		require.Nil(t, reply.unicast)
		assert.Zero(t, reply.msg.Id, "RFC 6762 18.1: a multicast response carries a zero id")
		assert.Empty(t, reply.msg.Question, "RFC 6762 6: a response repeats no questions")
		byName[reply.link.name] = addrStrings(reply.msg.Extra)
	}

	assert.Equal(t, []string{"192.168.1.50"}, byName["Ethernet 4"])
	assert.Equal(t, []string{"10.0.0.241"}, byName["Wi-Fi"],
		"each interface offers only an address reachable through it")
}

func TestPlanRepliesUnicastsWhenQUBitIsSet(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()

	query := browseQuery()
	query.Question[0].Qclass |= cacheFlush // the QU bit
	from := mdnsPeer("192.168.1.7")

	replies := svc.planReplies(query, links, from)

	require.Len(t, replies, 1)
	assert.Equal(t, from, replies[0].unicast)
	assert.Nil(t, replies[0].link)
	assert.Equal(t, query.Id, replies[0].msg.Id, "a unicast reply is matched by id")
	assert.Empty(t, replies[0].msg.Question)
	assert.Equal(t, []string{"192.168.1.50"}, addrStrings(replies[0].msg.Extra),
		"the reply describes the interface the querier is on")
}

func TestPlanRepliesUsesLegacyShapeForNonMDNSPort(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()

	// No QU bit: a legacy resolver does not know to set one, and would never
	// see a multicast answer sent to port 5353.
	query := browseQuery()
	from := legacyPeer("10.0.0.167")

	replies := svc.planReplies(query, links, from)

	require.Len(t, replies, 1)
	assert.Equal(t, from, replies[0].unicast)
	assert.Equal(t, query.Id, replies[0].msg.Id)
	assert.Equal(t, query.Question, replies[0].msg.Question,
		"RFC 6762 6.7: a legacy client matches the answer by its question")
	assert.Equal(t, []string{"10.0.0.241"}, addrStrings(replies[0].msg.Extra))

	for _, record := range append(replies[0].msg.Answer, replies[0].msg.Extra...) {
		assert.LessOrEqual(t, record.Header().Ttl, uint32(legacyTTL),
			"legacy TTLs are capped because the client never sees the goodbye")
		assert.Zero(t, record.Header().Class&cacheFlush,
			"a legacy client has no mDNS cache to flush")
	}
}

func TestPlanRepliesLegacyShapeWinsOverQUBit(t *testing.T) {
	t.Parallel()

	// The source port decides. A legacy client that happens to set the QU bit
	// still needs its question echoed back.
	svc := testService()
	query := browseQuery()
	query.Question[0].Qclass |= cacheFlush

	replies := svc.planReplies(query, twoLinks(), legacyPeer("10.0.0.167"))

	require.Len(t, replies, 1)
	assert.Equal(t, query.Question, replies[0].msg.Question)
}

func TestPlanRepliesIgnoresWhatIsNotAQuery(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()
	from := mdnsPeer("10.0.0.167")

	t.Run("another responder's announcement", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Response = true
		assert.Empty(t, svc.planReplies(query, links, from))
	})

	t.Run("a probe", func(t *testing.T) {
		t.Parallel()
		// An authority section means the sender is probing for a name
		// conflict. This responder does not take part in probing, and
		// answering would look like a conflict to the prober.
		query := browseQuery()
		query.Ns = []dns.RR{&dns.SRV{Hdr: dns.RR_Header{
			Name: svc.instanceName(), Rrtype: dns.TypeSRV, Class: dns.ClassINET,
		}}}
		assert.Empty(t, svc.planReplies(query, links, from))
	})

	t.Run("no questions", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Question = nil
		assert.Empty(t, svc.planReplies(query, links, from))
	})

	t.Run("a class we do not serve", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Question[0].Qclass = dns.ClassCHAOS
		assert.Empty(t, svc.planReplies(query, links, from))
	})

	t.Run("somebody else's service", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Question[0].Name = "_http._tcp.local."
		assert.Empty(t, svc.planReplies(query, links, from))
	})

	t.Run("no interfaces left", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, svc.planReplies(browseQuery(), nil, from))
	})
}

func TestPlanRepliesAcceptsClassANY(t *testing.T) {
	t.Parallel()

	svc := testService()
	query := browseQuery()
	query.Question[0].Qclass = dns.ClassANY

	assert.Len(t, svc.planReplies(query, twoLinks(), mdnsPeer("10.0.0.167")), 2)
}

func TestPlanRepliesAnswersEveryQuestionInOneMessage(t *testing.T) {
	t.Parallel()

	svc := testService()
	query := browseQuery()
	query.Question = append(query.Question, dns.Question{
		Name:   svc.hostName(),
		Qtype:  dns.TypeA,
		Qclass: dns.ClassINET,
	})

	replies := svc.planReplies(query, twoLinks(), mdnsPeer("10.0.0.167"))

	// Two interfaces, two questions.
	assert.Len(t, replies, 4)
}

func TestPlanRepliesHonoursKnownAnswerSuppression(t *testing.T) {
	t.Parallel()

	svc := testService()
	query := browseQuery()
	query.Answer = []dns.RR{&dns.PTR{
		Hdr: dns.RR_Header{
			Name: svc.typeName(), Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: serviceTTL,
		},
		Ptr: svc.instanceName(),
	}}

	assert.Empty(t, svc.planReplies(query, twoLinks(), mdnsPeer("10.0.0.167")))
}

func TestStartWithoutUsableInterfaces(t *testing.T) {
	t.Parallel()

	r := New(&Service{Instance: "test", Type: "_zaparoo._tcp", Host: "test", Port: 7497, Text: []string{""}}, nil)
	t.Cleanup(r.Stop)

	require.ErrorIs(t, r.Start(nil), ErrNoInterfaces)
	// An interface that contributes no address leaves nothing to advertise on.
	require.ErrorIs(t, r.Start([]net.Interface{{Index: phantomIfaceIndex, Name: "phantom0"}}), ErrNoInterfaces)
	assert.Empty(t, r.Interfaces())
}

func TestStopIsSafeBeforeStartAndRepeatable(t *testing.T) {
	t.Parallel()

	r := New(&Service{Instance: "test", Type: "_zaparoo._tcp", Host: "test", Port: 7497, Text: []string{""}}, nil)

	r.Stop()
	r.Stop()
	r.Stop()

	assert.Empty(t, r.Interfaces())
}

func TestSetInterfacesBeforeStart(t *testing.T) {
	t.Parallel()

	r := New(&Service{Instance: "test", Type: "_zaparoo._tcp", Host: "test", Port: 7497, Text: []string{""}}, nil)
	t.Cleanup(r.Stop)

	changed, err := r.SetInterfaces(nil)
	assert.False(t, changed)
	assert.ErrorIs(t, err, ErrNoSockets)
}

func TestStartAfterStopOpensNothing(t *testing.T) {
	t.Parallel()

	usable := usableInterface(t)

	r := New(&Service{Instance: "test", Type: "_zaparoo._tcp", Host: "test", Port: 7497, Text: []string{""}}, nil)
	r.Stop()

	// The guard has to come before the sockets are opened, or a Stop racing
	// with a Start leaves a listener behind on port 5353.
	require.ErrorIs(t, r.Start([]net.Interface{usable}), ErrNoSockets)
	assert.Empty(t, r.Interfaces())
}

// usableInterface returns a real interface that has an address worth
// advertising, skipping the test when the machine has none.
func usableInterface(t *testing.T) net.Interface {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	for i := range ifaces {
		if ifaces[i].Flags&net.FlagLoopback != 0 || ifaces[i].Flags&net.FlagUp == 0 {
			continue
		}
		v4, v6, addrErr := InterfaceAddrs(&ifaces[i])
		if addrErr == nil && (len(v4) > 0 || len(v6) > 0) {
			return ifaces[i]
		}
	}
	t.Skip("no network interface with a usable address")
	return net.Interface{}
}

func TestPlanRepliesRejectsWrongTypeForAName(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()
	from := mdnsPeer("10.0.0.167")

	tests := []struct {
		name  string
		qname string
		qtype uint16
	}{
		{"instance asked for an address", svc.instanceName(), dns.TypeA},
		{"host asked for a service record", svc.hostName(), dns.TypeSRV},
		{"enumeration asked for a service record", enumerationName, dns.TypeSRV},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query := browseQuery()
			query.Question[0] = dns.Question{Name: tt.qname, Qtype: tt.qtype, Qclass: dns.ClassINET}
			assert.Empty(t, svc.planReplies(query, links, from))
		})
	}
}

func TestPlanRepliesAnswersTypeANY(t *testing.T) {
	t.Parallel()

	svc := testService()
	links := twoLinks()
	from := mdnsPeer("10.0.0.167")

	t.Run("instance", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Question[0] = dns.Question{Name: svc.instanceName(), Qtype: dns.TypeANY, Qclass: dns.ClassINET}
		assert.Len(t, svc.planReplies(query, links, from), 2)
	})

	t.Run("host", func(t *testing.T) {
		t.Parallel()
		query := browseQuery()
		query.Question[0] = dns.Question{Name: svc.hostName(), Qtype: dns.TypeANY, Qclass: dns.ClassINET}
		replies := svc.planReplies(query, links, from)
		require.Len(t, replies, 2)
		assert.Equal(t, []string{"192.168.1.50"}, addrStrings(replies[0].msg.Answer))
	})
}

func TestIsKnownAnswerIgnoresUnrelatedRecords(t *testing.T) {
	t.Parallel()

	svc := testService()
	answer := []dns.RR{svc.ptr(serviceTTL)}

	query := new(dns.Msg)
	query.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: svc.hostName(), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: hostTTL},
		A:   net.ParseIP("10.0.0.241").To4(),
	}}
	assert.False(t, isKnownAnswer(answer, query),
		"an address the querier already has says nothing about the service record")

	assert.False(t, isKnownAnswer(nil, query))
	assert.False(t, isKnownAnswer(answer, new(dns.Msg)))

	// Suppression only applies to the shared PTR; an answer led by anything
	// else is not a known-answer candidate.
	assert.False(t, isKnownAnswer([]dns.RR{svc.srv(serviceTTL, false)}, query))
}

func TestSelectAddrsIgnoresNonPrefixAddresses(t *testing.T) {
	t.Parallel()

	// Interface address tables can yield plain *net.IPAddr entries, which
	// carry no prefix and are not what an A record is built from.
	v4, v6 := selectAddrs([]net.Addr{
		&net.IPAddr{IP: net.ParseIP("10.0.0.241")},
		ipNet("10.0.0.241/24"),
	})

	require.Len(t, v4, 1)
	assert.Empty(t, v6)
}

func TestSameIPsComparesLength(t *testing.T) {
	t.Parallel()

	one := []net.IP{net.ParseIP("10.0.0.241").To4()}
	two := append([]net.IP{}, one...)
	two = append(two, net.ParseIP("10.0.0.242").To4())

	assert.False(t, sameIPs(one, two))
	assert.True(t, sameIPs(one, one))
}

func TestDiffLinksSeesAnAddressAppearing(t *testing.T) {
	t.Parallel()

	before := link{index: 22, name: "Wi-Fi", addrs: linkAddrs{v4: []net.IP{net.ParseIP("10.0.0.241").To4()}}}
	after := before
	after.addrs = linkAddrs{
		v4: []net.IP{net.ParseIP("10.0.0.241").To4()},
		v6: []net.IP{net.ParseIP("2403:580e:2024::1")},
	}

	_, _, readdressed := diffLinks([]link{before}, []link{after})
	require.Len(t, readdressed, 1)
	assert.Equal(t, "Wi-Fi", readdressed[0].name)
}

func TestHandlePacketSurvivesRubbishOnTheWire(t *testing.T) {
	t.Parallel()

	// Anything on the LAN can send to the multicast group, so a packet that
	// does not parse must be dropped rather than take the responder down.
	r := New(&Service{Instance: "test", Type: "_zaparoo._tcp", Host: "test", Port: 7497, Text: []string{""}}, nil)
	t.Cleanup(r.Stop)

	from := &net.UDPAddr{IP: net.ParseIP("10.0.0.167"), Port: Port}
	r.handlePacket([]byte{0x00}, from)
	r.handlePacket(nil, from)
	r.handlePacket([]byte("not a dns message at all"), from)
}

func TestSendingWithoutSocketsIsHarmless(t *testing.T) {
	t.Parallel()

	// Stop closes the sockets while an announcement may still be in flight.
	// That path has to fall through quietly rather than panic.
	svc := testService()
	r := New(&svc, nil)
	t.Cleanup(r.Stop)

	links := twoLinks()
	r.sendRecords(links, func(l link) []dns.RR {
		return r.svc.lookupAnswer(l.addrs, serviceTTL, true)
	})
	r.writeUnicast(newReply(1, []dns.RR{svc.ptr(serviceTTL)}, nil),
		&net.UDPAddr{IP: net.ParseIP("10.0.0.167"), Port: Port})
	r.writeUnicast(newReply(1, []dns.RR{svc.ptr(serviceTTL)}, nil),
		&net.UDPAddr{IP: net.ParseIP("fd89:8088:c6b4::1"), Port: Port})
}

func TestAnnounceAfterStopStartsNoGoroutine(t *testing.T) {
	t.Parallel()

	// goleak in TestMain is the real assertion here: an announcement queued
	// after Stop would outlive the responder and race its WaitGroup.
	svc := testService()
	r := New(&svc, nil)
	r.Stop()

	r.announce(twoLinks())
}
