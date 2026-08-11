package netagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

var (
	MDNSAddress          = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	SSDPAddress          = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	SSDPBroadcastAddress = &net.UDPAddr{IP: net.IPv4bcast, Port: 1900}
)

type Deduper struct {
	mu      sync.Mutex
	entries map[[32]byte]time.Time
	ttl     time.Duration
}

func NewDeduper(ttl time.Duration) *Deduper {
	return &Deduper{entries: make(map[[32]byte]time.Time), ttl: ttl}
}

func (d *Deduper) Seen(kind MessageKind, payload []byte) bool {
	h := sha256.New()
	h.Write([]byte{byte(kind)})
	h.Write(payload)
	var key [32]byte
	copy(key[:], h.Sum(nil))
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for k, expires := range d.entries {
		if now.After(expires) {
			delete(d.entries, k)
		}
	}
	if expires, ok := d.entries[key]; ok && now.Before(expires) {
		return true
	}
	d.entries[key] = now.Add(d.ttl)
	return false
}

func IsMDNSQuery(payload []byte) bool {
	return len(payload) >= 12 && binary.BigEndian.Uint16(payload[2:4])&0x8000 == 0
}

func IsMDNSResponse(payload []byte) bool {
	return len(payload) >= 12 && binary.BigEndian.Uint16(payload[2:4])&0x8000 != 0
}

func SSDPMethod(payload []byte) string {
	line := payload
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.ToUpper(strings.TrimSpace(string(line)))
}

func IsSSDPSearch(payload []byte) bool {
	return strings.HasPrefix(SSDPMethod(payload), "M-SEARCH ")
}

func IsSSDPNotify(payload []byte) bool {
	return strings.HasPrefix(SSDPMethod(payload), "NOTIFY ")
}

func IsSSDPResponse(payload []byte) bool {
	return strings.HasPrefix(SSDPMethod(payload), "HTTP/1.1 200")
}

func OpenMulticast(iface *net.Interface, group *net.UDPAddr) (*net.UDPConn, *ipv4.PacketConn, error) {
	conn, err := net.ListenMulticastUDP("udp4", iface, group)
	if err != nil {
		return nil, nil, err
	}
	_ = conn.SetReadBuffer(256 * 1024)
	_ = enableBroadcast(conn)
	packet := ipv4.NewPacketConn(conn)
	_ = packet.SetControlMessage(ipv4.FlagTTL|ipv4.FlagInterface|ipv4.FlagDst, true)
	_ = packet.SetMulticastLoopback(true)
	return conn, packet, nil
}

func SendMulticast(packet *ipv4.PacketConn, iface *net.Interface, addr *net.UDPAddr, payload []byte, ttl int) error {
	if err := packet.SetMulticastInterface(iface); err != nil {
		return err
	}
	if err := packet.SetMulticastTTL(ttl); err != nil {
		return err
	}
	_, err := packet.WriteTo(payload, &ipv4.ControlMessage{IfIndex: iface.Index, TTL: ttl}, addr)
	return err
}
