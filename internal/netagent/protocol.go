package netagent

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	protocolVersion = 1
	// ProtocolVersion is reported by host and guest diagnostics so mismatched
	// preview artifacts can fail with an actionable message.
	ProtocolVersion = protocolVersion
	headerSize      = 32
	maxDatagram     = 1100
	maxMessage      = 65535
	reassemblyTTL   = 3 * time.Second
)

var protocolMagic = [4]byte{'H', '1', 'N', '1'}

type MessageKind uint8

const (
	KindHeartbeat MessageKind = iota + 1
	KindMDNSQuery
	KindMDNSResponse
	KindSSDPSearch
	KindSSDPResponse
	KindSSDPNotify
)

type Message struct {
	Kind           MessageKind
	ID             uint64
	SourceIP       net.IP
	SourcePort     uint16
	TargetPort     uint16
	TTL            uint8
	InterfaceIndex uint32
	Payload        []byte
}

func NewMessageID() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint64(b[:])
}

func EncodeMessage(m Message) ([][]byte, error) {
	if len(m.Payload) > maxMessage {
		return nil, fmt.Errorf("payload exceeds %d bytes", maxMessage)
	}
	ip := m.SourceIP.To4()
	if ip == nil {
		ip = net.IPv4zero
	}
	chunkSize := maxDatagram - headerSize
	count := (len(m.Payload) + chunkSize - 1) / chunkSize
	if count == 0 {
		count = 1
	}
	if count > 255 {
		return nil, errors.New("message requires too many fragments")
	}
	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(m.Payload) {
			end = len(m.Payload)
		}
		packet := make([]byte, headerSize+end-start)
		copy(packet[:4], protocolMagic[:])
		packet[4] = protocolVersion
		packet[5] = byte(m.Kind)
		packet[6] = byte(i)
		packet[7] = byte(count)
		binary.BigEndian.PutUint64(packet[8:16], m.ID)
		binary.BigEndian.PutUint16(packet[16:18], m.SourcePort)
		binary.BigEndian.PutUint16(packet[18:20], m.TargetPort)
		packet[20] = m.TTL
		copy(packet[21:25], ip)
		binary.BigEndian.PutUint16(packet[25:27], uint16(len(m.Payload)))
		binary.BigEndian.PutUint32(packet[27:31], m.InterfaceIndex)
		copy(packet[headerSize:], m.Payload[start:end])
		out = append(out, packet)
	}
	return out, nil
}

type partialMessage struct {
	created time.Time
	msg     Message
	parts   [][]byte
	total   int
}

type Reassembler struct {
	mu      sync.Mutex
	partial map[string]*partialMessage
}

func NewReassembler() *Reassembler {
	return &Reassembler{partial: make(map[string]*partialMessage)}
}

func (r *Reassembler) Add(peer net.Addr, packet []byte) (*Message, error) {
	if len(packet) < headerSize || string(packet[:4]) != string(protocolMagic[:]) {
		return nil, errors.New("invalid relay packet")
	}
	if packet[4] != protocolVersion {
		return nil, fmt.Errorf("unsupported relay version %d", packet[4])
	}
	index, count := int(packet[6]), int(packet[7])
	if count < 1 || index >= count {
		return nil, errors.New("invalid fragment index")
	}
	id := binary.BigEndian.Uint64(packet[8:16])
	total := int(binary.BigEndian.Uint16(packet[25:27]))
	if total > maxMessage {
		return nil, errors.New("invalid message length")
	}
	key := fmt.Sprintf("%s/%d", peer.String(), id)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, p := range r.partial {
		if now.Sub(p.created) > reassemblyTTL {
			delete(r.partial, k)
		}
	}
	p := r.partial[key]
	if p == nil {
		p = &partialMessage{
			created: now,
			total:   total,
			parts:   make([][]byte, count),
			msg: Message{
				Kind:           MessageKind(packet[5]),
				ID:             id,
				SourcePort:     binary.BigEndian.Uint16(packet[16:18]),
				TargetPort:     binary.BigEndian.Uint16(packet[18:20]),
				TTL:            packet[20],
				InterfaceIndex: binary.BigEndian.Uint32(packet[27:31]),
				SourceIP:       net.IPv4(packet[21], packet[22], packet[23], packet[24]),
			},
		}
		r.partial[key] = p
	}
	if len(p.parts) != count || p.total != total {
		delete(r.partial, key)
		return nil, errors.New("fragment metadata changed")
	}
	if p.msg.Kind != MessageKind(packet[5]) || p.msg.SourcePort != binary.BigEndian.Uint16(packet[16:18]) ||
		p.msg.TargetPort != binary.BigEndian.Uint16(packet[18:20]) || p.msg.TTL != packet[20] ||
		!p.msg.SourceIP.Equal(net.IPv4(packet[21], packet[22], packet[23], packet[24])) ||
		p.msg.InterfaceIndex != binary.BigEndian.Uint32(packet[27:31]) {
		delete(r.partial, key)
		return nil, errors.New("fragment metadata changed")
	}
	if p.parts[index] == nil {
		p.parts[index] = make([]byte, len(packet)-headerSize)
		copy(p.parts[index], packet[headerSize:])
	}
	size := 0
	for _, part := range p.parts {
		if part == nil {
			return nil, nil
		}
		size += len(part)
	}
	if size != p.total {
		delete(r.partial, key)
		return nil, errors.New("reassembled length mismatch")
	}
	p.msg.Payload = make([]byte, 0, size)
	for _, part := range p.parts {
		p.msg.Payload = append(p.msg.Payload, part...)
	}
	delete(r.partial, key)
	return &p.msg, nil
}
