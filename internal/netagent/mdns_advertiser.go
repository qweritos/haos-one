package netagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	mdnsHostTTL    = 120
	mdnsServiceTTL = 4500
)

var errDNSSectionDone = dnsmessage.ErrSectionDone

type MDNSAdvertiser struct {
	hostName     dnsmessage.Name
	serviceName  dnsmessage.Name
	instanceName dnsmessage.Name
	port         uint16
}

func NewMDNSAdvertiser(cfg *Config) (*MDNSAdvertiser, error) {
	dnsName, err := NormalizeDNSName(cfg.EffectiveDNSName())
	if err != nil {
		return nil, err
	}
	hostName, err := dnsmessage.NewName(dnsName + ".")
	if err != nil {
		return nil, err
	}
	serviceName, err := dnsmessage.NewName("_home-assistant._tcp.local.")
	if err != nil {
		return nil, err
	}
	label := strings.Split(dnsName, ".")[0]
	instanceName, err := dnsmessage.NewName(label + "._home-assistant._tcp.local.")
	if err != nil {
		return nil, err
	}
	return &MDNSAdvertiser{hostName: hostName, serviceName: serviceName, instanceName: instanceName, port: uint16(cfg.EffectiveHTTPPort())}, nil
}

func (a *MDNSAdvertiser) MatchesQuery(payload []byte) bool {
	var parser dnsmessage.Parser
	header, err := parser.Start(payload)
	if err != nil || header.Response {
		return false
	}
	for {
		question, err := parser.Question()
		if errors.Is(err, errDNSSectionDone) {
			return false
		}
		if err != nil {
			return false
		}
		name := strings.ToLower(question.Name.String())
		if name == strings.ToLower(a.hostName.String()) || name == strings.ToLower(a.serviceName.String()) || name == strings.ToLower(a.instanceName.String()) {
			return true
		}
	}
}

func (a *MDNSAdvertiser) Packet(ip net.IP, ttl uint32) ([]byte, error) {
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("mDNS advertisement requires an IPv4 address")
	}
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	builder.EnableCompression()
	if err := builder.StartAnswers(); err != nil {
		return nil, err
	}
	cacheFlush := dnsmessage.Class(0x8001)
	serviceTTL := uint32(mdnsServiceTTL)
	if ttl == 0 {
		serviceTTL = 0
	}
	if err := builder.AResource(dnsmessage.ResourceHeader{Name: a.hostName, Class: cacheFlush, TTL: ttl}, dnsmessage.AResource{A: [4]byte{v4[0], v4[1], v4[2], v4[3]}}); err != nil {
		return nil, err
	}
	if err := builder.PTRResource(dnsmessage.ResourceHeader{Name: a.serviceName, Class: dnsmessage.ClassINET, TTL: serviceTTL}, dnsmessage.PTRResource{PTR: a.instanceName}); err != nil {
		return nil, err
	}
	if err := builder.SRVResource(dnsmessage.ResourceHeader{Name: a.instanceName, Class: cacheFlush, TTL: ttl}, dnsmessage.SRVResource{Port: a.port, Target: a.hostName}); err != nil {
		return nil, err
	}
	internalURL := fmt.Sprintf("internal_url=http://%s:%d", strings.TrimSuffix(a.hostName.String(), "."), a.port)
	if err := builder.TXTResource(dnsmessage.ResourceHeader{Name: a.instanceName, Class: cacheFlush, TTL: ttl}, dnsmessage.TXTResource{TXT: []string{"location_name=Home Assistant", internalURL}}); err != nil {
		return nil, err
	}
	return builder.Finish()
}

func (a *MDNSAdvertiser) Send(socket discoverySocket, ttl uint32) error {
	ip, err := interfaceIPv4(socket.iface)
	if err != nil {
		return err
	}
	payload, err := a.Packet(ip, ttl)
	if err != nil {
		return err
	}
	return SendMulticast(socket.packet, socket.iface, MDNSAddress, payload, 255)
}

func (a *MDNSAdvertiser) IsOwnAnnouncement(payload []byte, iface *net.Interface) bool {
	ip, err := interfaceIPv4(iface)
	if err != nil {
		return false
	}
	expected, err := a.Packet(ip, mdnsHostTTL)
	return err == nil && bytes.Equal(payload, expected)
}

func (a *MDNSAdvertiser) Run(ctx context.Context, sockets []discoverySocket) {
	lastAddresses := make(map[int]net.IP)
	sendAll := func(ttl uint32) {
		for _, socket := range sockets {
			ip, err := interfaceIPv4(socket.iface)
			if err != nil {
				continue
			}
			payload, err := a.Packet(ip, ttl)
			if err == nil {
				_ = SendMulticast(socket.packet, socket.iface, MDNSAddress, payload, 255)
				lastAddresses[socket.iface.Index] = append(net.IP(nil), ip...)
			}
		}
	}
	sendAll(mdnsHostTTL)
	second := time.NewTimer(time.Second)
	defer second.Stop()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	defer func() {
		for _, socket := range sockets {
			ip := lastAddresses[socket.iface.Index]
			payload, err := a.Packet(ip, 0)
			if err == nil {
				_ = SendMulticast(socket.packet, socket.iface, MDNSAddress, payload, 255)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-second.C:
			sendAll(mdnsHostTTL)
		case <-ticker.C:
			sendAll(mdnsHostTTL)
		}
	}
}
