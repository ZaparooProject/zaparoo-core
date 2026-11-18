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
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	castService    = "_googlecast._tcp.local."
	castBrowseWait = 2 * time.Second
	// maxMDNSPacket is the largest mDNS message RFC 6762 allows.
	maxMDNSPacket = 9000
)

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// castDevice is a Cast device found on the network.
type castDevice struct {
	Name  string
	ID    string
	Model string
	Host  string
	Port  int
}

func buildCastQuery() ([]byte, error) {
	name, err := dnsmessage.NewName(castService)
	if err != nil {
		return nil, fmt.Errorf("mdns query name: %w", err)
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	b.EnableCompression()
	if err = b.StartQuestions(); err != nil {
		return nil, fmt.Errorf("mdns query: %w", err)
	}
	if err = b.Question(dnsmessage.Question{
		Name: name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET,
	}); err != nil {
		return nil, fmt.Errorf("mdns query: %w", err)
	}
	msg, err := b.Finish()
	if err != nil {
		return nil, fmt.Errorf("mdns query: %w", err)
	}
	return msg, nil
}

// browseCast asks for Cast devices and collects answers until wait passes
// or ctx ends. The query goes out from an ephemeral port, so responders
// answer it directly (RFC 6762 legacy unicast) and port 5353 is left to the
// system's own mDNS daemon.
func browseCast(ctx context.Context, conn net.PacketConn, dst net.Addr, wait time.Duration) ([]castDevice, error) {
	query, err := buildCastQuery()
	if err != nil {
		return nil, err
	}
	if _, err := conn.WriteTo(query, dst); err != nil {
		return nil, fmt.Errorf("failed to send mdns query: %w", err)
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()

	found := map[string]castDevice{}
	buf := make([]byte, maxMDNSPacket)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			return nil, fmt.Errorf("failed to read mdns answer: %w", err)
		}
		var fromIP net.IP
		if udp, ok := from.(*net.UDPAddr); ok {
			fromIP = udp.IP
		}
		devices, err := parseCastResponse(buf[:n], fromIP)
		if err != nil {
			continue
		}
		for _, d := range devices {
			key := d.ID
			if key == "" {
				key = d.Name + "@" + d.Host
			}
			found[key] = d
		}
	}
	devices := make([]castDevice, 0, len(found))
	for _, d := range found {
		devices = append(devices, d)
	}
	slices.SortFunc(devices, func(a, b castDevice) int { return strings.Compare(a.Name, b.Name) })
	return devices, nil
}

// discoverCast browses the local network for Cast devices.
func discoverCast(ctx context.Context) ([]castDevice, error) {
	var lc net.ListenConfig
	conn, err := lc.ListenPacket(ctx, "udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("failed to open mdns socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return browseCast(ctx, conn, mdnsGroup, castBrowseWait)
}

type castInstance struct {
	txt    map[string]string
	target string
	port   int
}

func lowerName(n *dnsmessage.Name) string {
	return strings.ToLower(n.String())
}

// parseCastResponse reads the Cast devices an mDNS answer describes. A
// device's address comes from its A record, or the sender when there is none.
func parseCastResponse(msg []byte, from net.IP) ([]castDevice, error) {
	var p dnsmessage.Parser
	header, err := p.Start(msg)
	if err != nil {
		return nil, fmt.Errorf("mdns answer: %w", err)
	}
	if !header.Response {
		return nil, nil
	}
	if err = p.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("mdns answer: %w", err)
	}
	var records []dnsmessage.Resource
	answers, err := p.AllAnswers()
	if err != nil {
		return nil, fmt.Errorf("mdns answer: %w", err)
	}
	records = append(records, answers...)
	if err := p.SkipAllAuthorities(); err == nil {
		if extra, err := p.AllAdditionals(); err == nil {
			records = append(records, extra...)
		}
	}

	instances := map[string]*castInstance{}
	order := []string{}
	instance := func(name string) *castInstance {
		if inst, ok := instances[name]; ok {
			return inst
		}
		inst := &castInstance{txt: map[string]string{}}
		instances[name] = inst
		order = append(order, name)
		return inst
	}
	isCastInstance := func(name string) bool {
		return strings.HasSuffix(name, "."+castService) && len(name) > len(castService)+1
	}
	hosts := map[string]net.IP{}
	for i := range records {
		r := &records[i]
		name := lowerName(&r.Header.Name)
		switch body := r.Body.(type) {
		case *dnsmessage.PTRResource:
			if name == castService {
				target := lowerName(&body.PTR)
				if isCastInstance(target) {
					instance(target)
				}
			}
		case *dnsmessage.SRVResource:
			if isCastInstance(name) {
				inst := instance(name)
				inst.target = lowerName(&body.Target)
				inst.port = int(body.Port)
			}
		case *dnsmessage.TXTResource:
			if isCastInstance(name) {
				inst := instance(name)
				for _, kv := range body.TXT {
					k, v, ok := strings.Cut(kv, "=")
					if ok {
						inst.txt[strings.ToLower(k)] = v
					}
				}
			}
		case *dnsmessage.AResource:
			hosts[name] = net.IP(body.A[:])
		}
	}

	devices := make([]castDevice, 0, len(order))
	for _, name := range order {
		inst := instances[name]
		d := castDevice{
			Name:  strings.TrimSpace(inst.txt["fn"]),
			ID:    inst.txt["id"],
			Model: inst.txt["md"],
			Port:  inst.port,
		}
		if d.Name == "" {
			continue
		}
		if ip, ok := hosts[inst.target]; ok {
			d.Host = ip.String()
		} else if from != nil {
			d.Host = from.String()
		}
		if d.Port <= 0 {
			d.Port = castDefaultPort
		}
		devices = append(devices, d)
	}
	return devices, nil
}
