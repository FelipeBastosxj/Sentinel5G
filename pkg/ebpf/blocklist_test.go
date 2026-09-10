package ebpf

import (
	"net"
	"testing"
)

func TestIsIPv4(t *testing.T) {
	cases := map[string]bool{
		"10.42.0.7":       true,
		"::ffff:10.0.0.1": true, // v4-mapped: treated as IPv4, see ipv6Key's doc comment.
		"2001:db8::1":     false,
	}
	for addr, want := range cases {
		if got := isIPv4(net.ParseIP(addr)); got != want {
			t.Errorf("isIPv4(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestIpv4Key(t *testing.T) {
	key, err := ipv4Key(net.ParseIP("10.42.0.7"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 4 {
		t.Fatalf("expected a 4-byte key, got %d bytes", len(key))
	}
}

func TestIpv4Key_RejectsIPv6(t *testing.T) {
	if _, err := ipv4Key(net.ParseIP("2001:db8::1")); err == nil {
		t.Fatal("expected an error for a genuine IPv6 address")
	}
}

func TestIpv6Key(t *testing.T) {
	key, err := ipv6Key(net.ParseIP("2001:db8::1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 16 {
		t.Fatalf("expected a 16-byte key, got %d bytes", len(key))
	}
}

func TestIpv6Key_RejectsIPv4(t *testing.T) {
	if _, err := ipv6Key(net.ParseIP("10.42.0.7")); err == nil {
		t.Fatal("expected an error for a genuine IPv4 address")
	}
}

// Documents the v4-mapped-IPv6 boundary decision (::ffff:a.b.c.d) as a
// test, not just a comment: Go's net.IP.To4() already treats these as
// IPv4, and ipv6Key relies on that to reject them here — callers
// (Loader.Block/Unblock/SignalRate in loader_linux.go) route such an
// address into the IPv4 maps instead, since it really is an IPv4 endpoint
// on the wire and bpf/packet_filter.c's IPv4 path is what will actually
// observe its traffic.
func TestIpv6Key_RejectsV4MappedAddress(t *testing.T) {
	if _, err := ipv6Key(net.ParseIP("::ffff:10.0.0.1")); err == nil {
		t.Fatal("expected an error for a v4-mapped IPv6 address (should route to the IPv4 maps instead)")
	}
}
