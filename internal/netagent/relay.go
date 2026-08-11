package netagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
)

type relayStats struct {
	Sent       atomic.Uint64
	Received   atomic.Uint64
	MDNS       atomic.Uint64
	SSDP       atomic.Uint64
	Duplicates atomic.Uint64
}

type Relay struct {
	cfg        *Config
	tunnel     *Tunnel
	conn       *net.UDPConn
	peer       *net.UDPAddr
	reassemble *Reassembler
	dedupe     *Deduper
	stats      relayStats
	mu         sync.Mutex
	pending    map[uint64]pendingSearch
	routes     []string
	routesUp   bool
	lastBeat   time.Time
	state      *State
}

type pendingSearch struct {
	port    uint16
	expires time.Time
}

type heartbeat struct {
	LANCIDRs []string `json:"lan_cidrs"`
}

type discoverySocket struct {
	iface  *net.Interface
	conn   *net.UDPConn
	packet *ipv4.PacketConn
}

func NewRelay(cfg *Config, tunnel *Tunnel) (*Relay, error) {
	local := &net.UDPAddr{IP: net.ParseIP(cfg.Address).To4(), Port: cfg.RelayPort}
	conn, err := net.ListenUDP("udp4", local)
	if err != nil {
		return nil, fmt.Errorf("listen relay %s: %w", local, err)
	}
	peer := &net.UDPAddr{IP: net.ParseIP(cfg.PeerAddress).To4(), Port: cfg.RelayPort}
	return &Relay{
		cfg: cfg, tunnel: tunnel, conn: conn, peer: peer,
		reassemble: NewReassembler(), dedupe: NewDeduper(250 * time.Millisecond),
		pending: make(map[uint64]pendingSearch), lastBeat: time.Now(),
	}, nil
}

func (r *Relay) Close() error { return r.conn.Close() }

func (r *Relay) send(message Message) error {
	if message.ID == 0 {
		message.ID = NewMessageID()
	}
	packets, err := EncodeMessage(message)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if _, err := r.conn.WriteToUDP(packet, r.peer); err != nil {
			return err
		}
		r.stats.Sent.Add(1)
	}
	return nil
}

func (r *Relay) receive(ctx context.Context, handler func(Message)) error {
	buffer := make([]byte, maxDatagram+64)
	for {
		_ = r.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, peer, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return ctx.Err()
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return err
		}
		if !peer.IP.Equal(r.peer.IP) {
			continue
		}
		r.stats.Received.Add(1)
		message, err := r.reassemble.Add(peer, append([]byte(nil), buffer[:n]...))
		if err != nil {
			log.Printf("discard relay packet: %v", err)
			continue
		}
		if message != nil {
			handler(*message)
		}
	}
}

func RunHost(ctx context.Context, cfg *Config) error {
	if !isAdministrator() {
		return errors.New("host run must be executed as root/Administrator")
	}
	tunnel, err := StartTunnel(ctx, cfg)
	if err != nil {
		return err
	}
	defer tunnel.Close(context.Background())
	for {
		state, err := prepareHost(ctx, cfg, tunnel)
		if err != nil {
			return err
		}
		if tunnel.Helper != nil && tunnel.Helper.Process != nil {
			state.HelperPID = tunnel.Helper.Process.Pid
			if err := SaveState(cfg.StateFile, state); err != nil {
				_ = cleanupHost(context.Background(), state)
				return err
			}
		}
		relay, err := NewRelay(cfg, tunnel)
		if err != nil {
			_ = cleanupHost(context.Background(), state)
			return err
		}
		relay.state = state
		innerCtx, cancel := context.WithCancel(ctx)
		result := make(chan error, 1)
		go func() { result <- relay.runHost(innerCtx) }()
		ticker := time.NewTicker(5 * time.Second)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				cancel()
				_ = relay.Close()
				<-result
				ticker.Stop()
				_ = cleanupHost(context.Background(), state)
				_ = os.Remove(cfg.StateFile)
				return nil
			case runErr := <-result:
				cancel()
				_ = relay.Close()
				ticker.Stop()
				_ = cleanupHost(context.Background(), state)
				_ = os.Remove(cfg.StateFile)
				if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
					return nil
				}
				return runErr
			case <-ticker.C:
				interfaces, cidrs, changed, checkErr := refreshedLANState(cfg)
				if checkErr != nil {
					log.Printf("refresh LAN state: %v", checkErr)
					continue
				}
				if changed {
					log.Printf("LAN changed: interfaces=%v cidrs=%v", interfaces, cidrs)
					cfg.Interfaces, cfg.LANCIDRs = interfaces, cidrs
					restart = true
				}
			}
		}
		ticker.Stop()
		cancel()
		_ = relay.Close()
		<-result
		if err := cleanupHost(context.Background(), state); err != nil {
			return err
		}
		_ = os.Remove(cfg.StateFile)
	}
}

func refreshedLANState(cfg *Config) ([]string, []string, bool, error) {
	interfaces := append([]string(nil), cfg.Interfaces...)
	if cfg.AutoInterface {
		name, err := DefaultInterface()
		if err != nil {
			return nil, nil, false, err
		}
		interfaces = []string{name}
	}
	cidrs := append([]string(nil), cfg.LANCIDRs...)
	if cfg.AutoCIDRs {
		cidrs = nil
		for _, name := range interfaces {
			values, err := InterfaceCIDRs(name)
			if err != nil {
				return nil, nil, false, err
			}
			cidrs = append(cidrs, values...)
		}
		cidrs = uniqueStrings(cidrs)
	}
	changed := !equalStrings(interfaces, cfg.Interfaces) || !equalStrings(cidrs, cfg.LANCIDRs)
	return interfaces, cidrs, changed, nil
}

func (r *Relay) runHost(ctx context.Context) error {
	mdnsSockets, err := openDiscoverySockets(r.cfg.Interfaces, MDNSAddress)
	if err != nil {
		return fmt.Errorf("open mDNS sockets: %w", err)
	}
	defer closeDiscoverySockets(mdnsSockets)
	ssdpSockets, err := openDiscoverySockets(r.cfg.Interfaces, SSDPAddress)
	if err != nil {
		return fmt.Errorf("open SSDP sockets: %w", err)
	}
	defer closeDiscoverySockets(ssdpSockets)

	for _, socket := range mdnsSockets {
		go r.readHostMDNS(ctx, socket)
	}
	for _, socket := range ssdpSockets {
		go r.readHostSSDP(ctx, socket)
	}
	go r.hostHeartbeat(ctx)
	return r.receive(ctx, func(message Message) {
		switch message.Kind {
		case KindHeartbeat:
			r.mu.Lock()
			r.lastBeat = time.Now()
			if r.state != nil {
				r.state.LastGuestHeartbeat = r.lastBeat
			}
			r.mu.Unlock()
		case KindMDNSQuery:
			if r.dedupe.Seen(message.Kind, message.Payload) {
				r.stats.Duplicates.Add(1)
				return
			}
			for _, socket := range mdnsSockets {
				if err := SendMulticast(socket.packet, socket.iface, MDNSAddress, message.Payload, 255); err != nil {
					log.Printf("send mDNS on %s: %v", socket.iface.Name, err)
				}
			}
			r.stats.MDNS.Add(1)
		case KindSSDPSearch:
			lifetime := ssdpSearchLifetime(message.Payload)
			r.mu.Lock()
			r.pending[message.ID] = pendingSearch{port: message.SourcePort, expires: time.Now().Add(lifetime)}
			r.mu.Unlock()
			time.AfterFunc(lifetime, func() {
				r.mu.Lock()
				delete(r.pending, message.ID)
				r.mu.Unlock()
			})
			for _, socket := range ssdpSockets {
				go r.forwardSSDPSearch(ctx, socket.iface, message, lifetime)
			}
			r.stats.SSDP.Add(1)
		}
	})
}

func (r *Relay) forwardSSDPSearch(ctx context.Context, iface *net.Interface, message Message, lifetime time.Duration) {
	ip, err := interfaceIPv4(iface)
	if err != nil {
		log.Printf("SSDP interface %s: %v", iface.Name, err)
		return
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	if err != nil {
		log.Printf("open SSDP search socket on %s: %v", iface.Name, err)
		return
	}
	defer conn.Close()
	_ = enableBroadcast(conn)
	packet := ipv4.NewPacketConn(conn)
	_ = packet.SetMulticastInterface(iface)
	_ = packet.SetMulticastTTL(2)
	if _, err := packet.WriteTo(message.Payload, &ipv4.ControlMessage{IfIndex: iface.Index, TTL: 2}, SSDPAddress); err != nil {
		log.Printf("send SSDP multicast on %s: %v", iface.Name, err)
	}
	_, _ = conn.WriteToUDP(message.Payload, SSDPBroadcastAddress)
	deadline := time.Now().Add(lifetime)
	buffer := make([]byte, maxMessage)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(deadline)
		n, source, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			return
		}
		payload := append([]byte(nil), buffer[:n]...)
		if !IsSSDPResponse(payload) {
			continue
		}
		if err := r.send(Message{Kind: KindSSDPResponse, ID: message.ID, SourceIP: source.IP, SourcePort: uint16(source.Port), TargetPort: message.SourcePort, TTL: 2, InterfaceIndex: uint32(iface.Index), Payload: payload}); err != nil {
			log.Printf("relay SSDP response: %v", err)
			return
		}
		r.stats.SSDP.Add(1)
	}
}

func (r *Relay) readHostMDNS(ctx context.Context, socket discoverySocket) {
	buffer := make([]byte, maxMessage)
	for ctx.Err() == nil {
		_ = socket.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, cm, source, err := socket.packet.ReadFrom(buffer)
		if err != nil {
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if !IsMDNSResponse(payload) || r.dedupe.Seen(KindMDNSResponse, payload) {
			continue
		}
		udpSource, ok := source.(*net.UDPAddr)
		if !ok {
			continue
		}
		ttl := uint8(255)
		if cm != nil && cm.TTL > 0 && cm.TTL < 256 {
			ttl = uint8(cm.TTL)
		}
		if err := r.send(Message{Kind: KindMDNSResponse, SourceIP: udpSource.IP, SourcePort: uint16(udpSource.Port), TargetPort: 5353, TTL: ttl, InterfaceIndex: uint32(socket.iface.Index), Payload: payload}); err != nil && ctx.Err() == nil {
			log.Printf("relay mDNS response: %v", err)
		}
		r.stats.MDNS.Add(1)
	}
}

func (r *Relay) readHostSSDP(ctx context.Context, socket discoverySocket) {
	buffer := make([]byte, maxMessage)
	for ctx.Err() == nil {
		_ = socket.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, source, err := socket.conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if IsSSDPSearch(payload) || r.dedupe.Seen(KindSSDPResponse, payload) {
			continue
		}
		if IsSSDPNotify(payload) {
			_ = r.send(Message{Kind: KindSSDPNotify, SourceIP: source.IP, SourcePort: uint16(source.Port), TargetPort: 1900, TTL: 2, InterfaceIndex: uint32(socket.iface.Index), Payload: payload})
			r.stats.SSDP.Add(1)
			continue
		}
		if !IsSSDPResponse(payload) {
			continue
		}
		now := time.Now()
		r.mu.Lock()
		pending := make(map[uint64]pendingSearch, len(r.pending))
		for id, search := range r.pending {
			if now.After(search.expires) {
				delete(r.pending, id)
				continue
			}
			pending[id] = search
		}
		r.mu.Unlock()
		for id, search := range pending {
			_ = r.send(Message{Kind: KindSSDPResponse, ID: id, SourceIP: source.IP, SourcePort: uint16(source.Port), TargetPort: search.port, TTL: 2, InterfaceIndex: uint32(socket.iface.Index), Payload: payload})
			r.stats.SSDP.Add(1)
		}
	}
}

func (r *Relay) hostHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	payload, _ := json.Marshal(heartbeat{LANCIDRs: r.cfg.LANCIDRs})
	for {
		_ = r.send(Message{Kind: KindHeartbeat, Payload: payload})
		r.writeState()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Relay) writeState() {
	if r.state == nil || r.cfg.StateFile == "" {
		return
	}
	r.mu.Lock()
	r.state.RelaySent = r.stats.Sent.Load()
	r.state.RelayReceived = r.stats.Received.Load()
	r.state.MDNSRelayed = r.stats.MDNS.Load()
	r.state.SSDPRelayed = r.stats.SSDP.Load()
	r.state.DuplicatesDropped = r.stats.Duplicates.Load()
	state := *r.state
	r.mu.Unlock()
	if err := SaveState(r.cfg.StateFile, &state); err != nil {
		log.Printf("write relay state: %v", err)
	}
}

func RunGuest(ctx context.Context, cfg *Config) error {
	if !isAdministrator() {
		return errors.New("guest run requires root")
	}
	tunnel, err := StartTunnel(ctx, cfg)
	if err != nil {
		return err
	}
	defer tunnel.Close(context.Background())
	relay, err := NewRelay(cfg, tunnel)
	if err != nil {
		return err
	}
	defer relay.Close()
	return relay.runGuest(ctx)
}

func (r *Relay) runGuest(ctx context.Context) error {
	ifaceName, err := DefaultInterface()
	if err != nil {
		return err
	}
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return err
	}
	localIP, err := interfaceIPv4(iface)
	if err != nil {
		return err
	}
	mdnsConn, mdnsPacket, err := OpenMulticast(iface, MDNSAddress)
	if err != nil {
		return err
	}
	defer mdnsConn.Close()
	ssdpConn, _, err := OpenMulticast(iface, SSDPAddress)
	if err != nil {
		return err
	}
	defer ssdpConn.Close()
	go r.captureGuestMDNS(ctx, mdnsPacket)
	go r.captureGuestSSDP(ctx, ssdpConn)
	go r.guestHeartbeat(ctx)
	go r.guestLiveness(ctx)
	go r.refreshEndpoint(ctx)
	return r.receive(ctx, func(message Message) {
		switch message.Kind {
		case KindHeartbeat:
			var beat heartbeat
			if json.Unmarshal(message.Payload, &beat) != nil {
				return
			}
			r.activateRoutes(ctx, uniqueStrings(beat.LANCIDRs))
		case KindMDNSResponse, KindSSDPResponse, KindSSDPNotify:
			port := int(message.TargetPort)
			if port == 0 {
				if message.Kind == KindMDNSResponse {
					port = 5353
				} else {
					port = 1900
				}
			}
			if err := injectUDP(message.SourceIP, int(message.SourcePort), localIP, port, int(message.TTL), message.Payload); err != nil {
				log.Printf("raw discovery injection failed, using tunnel source: %v", err)
				_ = fallbackInject(r.cfg.Address, int(message.SourcePort), localIP, port, message.Payload)
			}
		}
	})
}

func (r *Relay) captureGuestMDNS(ctx context.Context, packet *ipv4.PacketConn) {
	buffer := make([]byte, maxMessage)
	for ctx.Err() == nil {
		_ = packet.SetReadDeadline(time.Now().Add(time.Second))
		n, _, source, err := packet.ReadFrom(buffer)
		if err != nil {
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if !IsMDNSQuery(payload) || r.dedupe.Seen(KindMDNSQuery, payload) {
			continue
		}
		udpSource, ok := source.(*net.UDPAddr)
		if !ok {
			continue
		}
		_ = r.send(Message{Kind: KindMDNSQuery, SourceIP: udpSource.IP, SourcePort: uint16(udpSource.Port), TargetPort: 5353, TTL: 255, Payload: payload})
	}
}

func (r *Relay) captureGuestSSDP(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, maxMessage)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, source, err := conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if !IsSSDPSearch(payload) || r.dedupe.Seen(KindSSDPSearch, payload) {
			continue
		}
		_ = r.send(Message{Kind: KindSSDPSearch, SourceIP: source.IP, SourcePort: uint16(source.Port), TargetPort: 1900, TTL: 2, Payload: payload})
	}
}

func (r *Relay) guestHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		_ = r.send(Message{Kind: KindHeartbeat})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Relay) activateRoutes(ctx context.Context, cidrs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastBeat = time.Now()
	if equalStrings(r.routes, cidrs) && r.routesUp {
		return
	}
	if r.routesUp {
		_ = removeGuestRoutes(ctx, r.tunnel.Name, r.routes)
		r.routesUp = false
	}
	if err := r.tunnel.UpdateAllowedIPs(cidrs); err != nil {
		log.Printf("update WireGuard LAN prefixes: %v", err)
		return
	}
	if err := addGuestRoutes(ctx, r.tunnel.Name, cidrs); err != nil {
		log.Printf("activate LAN routes: %v", err)
		return
	}
	r.routes = append([]string(nil), cidrs...)
	r.routesUp = true
	log.Printf("activated LAN routes: %v", cidrs)
}

func (r *Relay) guestLiveness(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.routesUp {
			_ = removeGuestRoutes(context.Background(), r.tunnel.Name, r.routes)
			r.routesUp = false
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			if r.routesUp && time.Since(r.lastBeat) > 15*time.Second {
				_ = removeGuestRoutes(ctx, r.tunnel.Name, r.routes)
				r.routesUp = false
				log.Printf("host heartbeat expired; restored ordinary container routing")
			}
			r.mu.Unlock()
		}
	}
}

func (r *Relay) refreshEndpoint(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.tunnel.RefreshEndpoint(ctx); err != nil {
				log.Printf("refresh host endpoint: %v", err)
			}
		}
	}
}

func openDiscoverySockets(names []string, group *net.UDPAddr) ([]discoverySocket, error) {
	var result []discoverySocket
	for _, name := range names {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			closeDiscoverySockets(result)
			return nil, err
		}
		conn, packet, err := OpenMulticast(iface, group)
		if err != nil {
			closeDiscoverySockets(result)
			return nil, err
		}
		result = append(result, discoverySocket{iface: iface, conn: conn, packet: packet})
	}
	return result, nil
}

func closeDiscoverySockets(sockets []discoverySocket) {
	for _, socket := range sockets {
		_ = socket.conn.Close()
	}
}

func interfaceIPv4(iface *net.Interface) (net.IP, error) {
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.To4(), nil
		}
	}
	return nil, fmt.Errorf("interface %s has no IPv4 address", iface.Name)
}

func fallbackInject(sourceIP string, sourcePort int, destination net.IP, destinationPort int, payload []byte) error {
	local := &net.UDPAddr{IP: net.ParseIP(sourceIP).To4(), Port: sourcePort}
	conn, err := net.ListenUDP("udp4", local)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.WriteToUDP(payload, &net.UDPAddr{IP: destination, Port: destinationPort})
	return err
}

func ssdpSearchLifetime(payload []byte) time.Duration {
	duration := 5 * time.Second
	for _, line := range strings.Split(string(payload), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "MX") {
			if seconds, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && seconds >= 0 && seconds <= 120 {
				duration = time.Duration(seconds+2) * time.Second
			}
		}
	}
	return duration
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
