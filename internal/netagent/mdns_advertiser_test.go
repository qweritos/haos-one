package netagent

import (
	"errors"
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestMDNSAdvertisementUsesHostAddressAndConfiguredName(t *testing.T) {
	advertiser, err := NewMDNSAdvertiser(&Config{DNSName: "my-home.local", HTTPPort: 8123})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := advertiser.Packet(net.IPv4(192, 0, 2, 10), mdnsHostTTL)
	if err != nil {
		t.Fatal(err)
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(payload)
	if err != nil || !header.Response || !header.Authoritative {
		t.Fatalf("invalid response header: %+v %v", header, err)
	}
	if err := parser.SkipAllQuestions(); err != nil {
		t.Fatal(err)
	}
	answers, err := parser.AllAnswers()
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 4 {
		t.Fatalf("got %d records", len(answers))
	}
	a, ok := answers[0].Body.(*dnsmessage.AResource)
	if !ok || net.IP(a.A[:]).String() != "192.0.2.10" || answers[0].Header.Name.String() != "my-home.local." {
		t.Fatalf("unexpected A record: %#v", answers[0])
	}
}

func TestMDNSAdvertisementMatchesQueriesAndSendsGoodbye(t *testing.T) {
	advertiser, err := NewMDNSAdvertiser(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	name, _ := dnsmessage.NewName("homeassistant.local.")
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	_ = builder.StartQuestions()
	_ = builder.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	query, _ := builder.Finish()
	if !advertiser.MatchesQuery(query) {
		t.Fatal("advertiser did not match its host query")
	}
	goodbye, err := advertiser.Packet(net.IPv4(192, 0, 2, 10), 0)
	if err != nil {
		t.Fatal(err)
	}
	var parser dnsmessage.Parser
	_, _ = parser.Start(goodbye)
	_ = parser.SkipAllQuestions()
	for {
		answer, err := parser.Answer()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil || answer.Header.TTL != 0 {
			t.Fatalf("invalid goodbye record: %#v %v", answer, err)
		}
	}
}
