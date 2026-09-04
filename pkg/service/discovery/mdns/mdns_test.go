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
	"net"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	// Index 0 never matches a real interface, so Addrs() returns nothing and
	// the interface is dropped rather than advertised with no address.
	links := buildLinks([]net.Interface{{Index: 0, Name: "phantom0"}})
	assert.Empty(t, links)
}
