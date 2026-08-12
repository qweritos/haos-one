package netagent

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestProtocolRoundTripFragmented(t *testing.T) {
	payload := bytes.Repeat([]byte("discovery-payload-"), 300)
	want := Message{Kind: KindMDNSResponse, ID: 42, SourceIP: net.IPv4(192, 168, 1, 20), SourcePort: 5353, TargetPort: 5353, TTL: 255, Payload: payload}
	packets, err := EncodeMessage(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 2 {
		t.Fatal("expected fragmented message")
	}
	r := NewReassembler()
	var got *Message
	peer := &net.UDPAddr{IP: net.IPv4(10, 203, 0, 1), Port: DefaultRelayPort}
	for i := len(packets) - 1; i >= 0; i-- {
		got, err = r.Add(peer, packets[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	if got == nil || !bytes.Equal(got.Payload, want.Payload) || !got.SourceIP.Equal(want.SourceIP) || got.Kind != want.Kind {
		t.Fatalf("unexpected reassembly: %#v", got)
	}
}

func TestProtocolRoundTripEmptyPayload(t *testing.T) {
	packets, err := EncodeMessage(Message{Kind: KindHeartbeat, ID: 99})
	if err != nil {
		t.Fatal(err)
	}
	r := NewReassembler()
	got, err := r.Add(&net.UDPAddr{IP: net.IPv4(10, 203, 0, 2), Port: 47821}, packets[0])
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != KindHeartbeat || len(got.Payload) != 0 {
		t.Fatalf("unexpected empty message: %#v", got)
	}
}

func TestDeduper(t *testing.T) {
	d := NewDeduper(time.Second)
	if d.Seen(KindMDNSQuery, []byte("query")) {
		t.Fatal("first packet marked duplicate")
	}
	if !d.Seen(KindMDNSQuery, []byte("query")) {
		t.Fatal("second packet not marked duplicate")
	}
	if d.Seen(KindMDNSResponse, []byte("query")) {
		t.Fatal("kind must be part of duplicate key")
	}
}

func TestDiscoveryClassifiers(t *testing.T) {
	query := make([]byte, 12)
	response := make([]byte, 12)
	response[2] = 0x80
	if !IsMDNSQuery(query) || IsMDNSQuery(response) || !IsMDNSResponse(response) {
		t.Fatal("invalid mDNS classification")
	}
	if !IsSSDPSearch([]byte("M-SEARCH * HTTP/1.1\r\n")) || !IsSSDPNotify([]byte("NOTIFY * HTTP/1.1\r\n")) || !IsSSDPResponse([]byte("HTTP/1.1 200 OK\r\n")) {
		t.Fatal("invalid SSDP classification")
	}
}
