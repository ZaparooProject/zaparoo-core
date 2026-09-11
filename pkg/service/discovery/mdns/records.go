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
	"strings"

	"github.com/miekg/dns"
)

const (
	// serviceTTL is the TTL for records naming a service rather than a host.
	// RFC 6762 section 10 recommends 75 minutes.
	serviceTTL uint32 = 4500

	// hostTTL is the TTL for A and AAAA records. RFC 6762 section 10
	// recommends 120 seconds so that address changes propagate quickly.
	hostTTL uint32 = 120

	// cacheFlush is the top bit of an rrclass in a response, telling
	// receivers to replace rather than add to what they have cached.
	// RFC 6762 section 10.2.
	cacheFlush uint16 = 1 << 15

	// enumerationName is the meta-query answered with the service types this
	// responder offers. RFC 6763 section 9.
	enumerationName = "_services._dns-sd._udp.local."
)

// Service describes the DNS-SD service being advertised.
type Service struct {
	// Instance is the human-facing name, usually the device hostname.
	Instance string
	// Type is the service type, e.g. "_zaparoo._tcp".
	Type string
	// Host is the unqualified host label the SRV record targets.
	Host string
	// Text is the TXT record content. One zero-length string means
	// "no data"; an empty slice is not a valid TXT record.
	Text []string
	// Port is the TCP port the service listens on.
	Port uint16
}

// typeName is the fully qualified service type, e.g. "_zaparoo._tcp.local.".
func (s *Service) typeName() string {
	return strings.Trim(s.Type, ".") + ".local."
}

// instanceName is the fully qualified service instance name.
func (s *Service) instanceName() string {
	return dns.Fqdn(escapeInstance(s.Instance) + "." + s.typeName())
}

// hostName is the fully qualified host name the SRV record points at.
func (s *Service) hostName() string {
	return dns.Fqdn(strings.Trim(s.Host, ".") + ".local.")
}

// escapeInstance escapes the label separators DNS-SD allows to appear
// literally in an instance name. RFC 6763 section 4.1.1.
func escapeInstance(instance string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `.`, `\.`)
	return replacer.Replace(instance)
}

// ptr is the shared PTR record that makes the instance show up in a browse.
func (s *Service) ptr(ttl uint32) dns.RR {
	return &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   s.typeName(),
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		Ptr: s.instanceName(),
	}
}

// enumerationPTR advertises the service type in the DNS-SD meta-query.
func (s *Service) enumerationPTR(ttl uint32) dns.RR {
	return &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   enumerationName,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		Ptr: s.typeName(),
	}
}

func (s *Service) srv(ttl uint32, flush bool) dns.RR {
	return &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   s.instanceName(),
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET | flushBit(flush),
			Ttl:    ttl,
		},
		Priority: 0,
		Weight:   0,
		Port:     s.Port,
		Target:   s.hostName(),
	}
}

func (s *Service) txt(ttl uint32, flush bool) dns.RR {
	return &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   s.instanceName(),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET | flushBit(flush),
			Ttl:    ttl,
		},
		Txt: s.Text,
	}
}

// addrRecords returns the A and AAAA records for one interface's addresses.
// Every link answers with only its own addresses, so a client never learns an
// address belonging to an interface it cannot reach.
func (s *Service) addrRecords(addrs linkAddrs, ttl uint32, flush bool) []dns.RR {
	records := make([]dns.RR, 0, len(addrs.v4)+len(addrs.v6))
	host := s.hostName()
	class := dns.ClassINET | flushBit(flush)

	for _, ip := range addrs.v4 {
		records = append(records, &dns.A{
			Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeA, Class: class, Ttl: ttl},
			A:   ip,
		})
	}
	for _, ip := range addrs.v6 {
		records = append(records, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: class, Ttl: ttl},
			AAAA: ip,
		})
	}

	return records
}

// browsingAnswer answers a query for the service type: the PTR in the answer
// section, with everything needed to use the service in the additional section
// so the client does not have to ask again.
func (s *Service) browsingAnswer(addrs linkAddrs) (answer, extra []dns.RR) {
	answer = []dns.RR{s.ptr(serviceTTL)}
	extra = append(extra, s.srv(serviceTTL, false), s.txt(serviceTTL, false))
	extra = append(extra, s.addrRecords(addrs, hostTTL, false)...)
	return answer, extra
}

// lookupAnswer answers a query for the instance name, and is also the record
// set used for unsolicited announcements and goodbyes.
func (s *Service) lookupAnswer(addrs linkAddrs, ttl uint32, flush bool) []dns.RR {
	hostRecordTTL := hostTTL
	if ttl == 0 {
		// A goodbye zeroes every TTL, host records included.
		hostRecordTTL = 0
	}

	addrRecords := s.addrRecords(addrs, hostRecordTTL, flush)
	answer := make([]dns.RR, 0, 4+len(addrRecords))
	answer = append(answer,
		s.srv(ttl, flush),
		s.txt(ttl, flush),
		s.ptr(ttl),
		s.enumerationPTR(ttl),
	)
	return append(answer, addrRecords...)
}

// hostAnswer answers a direct query for the host's A or AAAA records.
func (s *Service) hostAnswer(addrs linkAddrs, qtype uint16) []dns.RR {
	records := s.addrRecords(addrs, hostTTL, true)
	matched := make([]dns.RR, 0, len(records))
	for _, record := range records {
		if record.Header().Rrtype == qtype {
			matched = append(matched, record)
		}
	}
	return matched
}

// linkAddrs holds the addresses of a single network interface.
type linkAddrs struct {
	v4 []net.IP
	v6 []net.IP
}

func (a linkAddrs) empty() bool {
	return len(a.v4) == 0 && len(a.v6) == 0
}

func flushBit(flush bool) uint16 {
	if flush {
		return cacheFlush
	}
	return 0
}

// wantsUnicast reports whether the querier asked for a unicast reply.
// RFC 6762 section 18.12 repurposes the top bit of qclass for this.
func wantsUnicast(q dns.Question) bool {
	return q.Qclass&cacheFlush != 0
}

// isKnownAnswer reports whether the querier already listed our PTR with at
// least half its TTL left, in which case RFC 6762 section 7.1 says stay quiet.
func isKnownAnswer(answer []dns.RR, query *dns.Msg) bool {
	if len(answer) == 0 || len(query.Answer) == 0 {
		return false
	}

	ptr, ok := answer[0].(*dns.PTR)
	if !ok {
		return false
	}

	for _, known := range query.Answer {
		knownPTR, ok := known.(*dns.PTR)
		if !ok {
			continue
		}
		if knownPTR.Ptr == ptr.Ptr && knownPTR.Hdr.Ttl >= ptr.Hdr.Ttl/2 {
			return true
		}
	}

	return false
}
