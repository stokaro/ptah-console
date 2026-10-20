package clientaddr_test

import (
	"net/netip"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/clientaddr"
)

// The ingress controller's Pods are 10.244.0.0/16, and a load balancer in
// front of them is 192.0.2.10.
var resolver = clientaddr.New([]netip.Prefix{
	netip.MustParsePrefix("10.244.0.0/16"),
	netip.MustParsePrefix("192.0.2.10/32"),
	netip.MustParsePrefix("fd00:10::/64"),
})

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		peer         string
		forwardedFor []string
		want         string
	}{
		{name: "a client without a proxy", peer: "198.51.100.7:51234", want: "198.51.100.7"},
		{name: "a client without a proxy names someone else",
			peer: "198.51.100.7:51234", forwardedFor: []string{"203.0.113.9"}, want: "198.51.100.7"},
		{name: "a trusted proxy names the client",
			peer: "10.244.1.5:40000", forwardedFor: []string{"198.51.100.7"}, want: "198.51.100.7"},
		{name: "a trusted proxy that says nothing", peer: "10.244.1.5:40000", want: "10.244.1.5"},
		{name: "the client's own entry is left of what the proxy appended",
			peer: "10.244.1.5:40000", forwardedFor: []string{"203.0.113.9, 198.51.100.7"}, want: "198.51.100.7"},
		{name: "a client that wrote a trusted address of its own",
			peer: "10.244.1.5:40000", forwardedFor: []string{"10.244.9.9, 198.51.100.7"}, want: "198.51.100.7"},
		{name: "two trusted proxies in a row",
			peer: "10.244.1.5:40000", forwardedFor: []string{"198.51.100.7, 192.0.2.10"}, want: "198.51.100.7"},
		{name: "the chain across two header lines",
			peer: "10.244.1.5:40000", forwardedFor: []string{"203.0.113.9", "198.51.100.7, 192.0.2.10"}, want: "198.51.100.7"},
		{name: "garbage the client wrote, left of the client",
			peer: "10.244.1.5:40000", forwardedFor: []string{"not-an-address, 198.51.100.7"}, want: "198.51.100.7"},
		{name: "garbage where a trusted proxy should have written",
			peer: "10.244.1.5:40000", forwardedFor: []string{"198.51.100.7, unknown"}, want: "10.244.1.5"},
		{name: "an empty entry where a trusted proxy should have written",
			peer: "10.244.1.5:40000", forwardedFor: []string{"198.51.100.7,"}, want: "10.244.1.5"},
		{name: "an empty header", peer: "10.244.1.5:40000", forwardedFor: []string{""}, want: "10.244.1.5"},
		{name: "every hop a trusted proxy",
			peer: "10.244.1.5:40000", forwardedFor: []string{"192.0.2.10, 10.244.2.2"}, want: "192.0.2.10"},
		{name: "more trusted hops than a deployment has",
			peer:         "10.244.1.5:40000",
			forwardedFor: []string{"198.51.100.7, " + strings.Repeat("10.244.2.2, ", clientaddr.MaxHops) + "10.244.2.2"},
			want:         "10.244.1.5"},
		{name: "a client with a port", peer: "10.244.1.5:40000", forwardedFor: []string{"198.51.100.7:4711"}, want: "198.51.100.7"},
		{name: "an IPv6 client", peer: "10.244.1.5:40000", forwardedFor: []string{"2001:DB8::7"}, want: "2001:db8::7"},
		{name: "a bracketed IPv6 client with a port",
			peer: "10.244.1.5:40000", forwardedFor: []string{"[2001:db8::7]:4711"}, want: "2001:db8::7"},
		{name: "an IPv6 address with a zone", peer: "10.244.1.5:40000", forwardedFor: []string{"fe80::1%eth0"}, want: "10.244.1.5"},
		{name: "an IPv4 client written as IPv6",
			peer: "10.244.1.5:40000", forwardedFor: []string{"::ffff:198.51.100.7"}, want: "198.51.100.7"},
		{name: "a trusted proxy reached over IPv6",
			peer: "[fd00:10::5]:40000", forwardedFor: []string{"198.51.100.7"}, want: "198.51.100.7"},
		{name: "a trusted proxy written as IPv6",
			peer: "[::ffff:10.244.1.5]:40000", forwardedFor: []string{"198.51.100.7"}, want: "198.51.100.7"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := resolver.Resolve(test.peer, test.forwardedFor)

			c.Assert(got, qt.Equals, netip.MustParseAddr(test.want))
		})
	}
}

// Without trusted proxies the header is never read.
func TestNoTrustedProxyMeansTheHeaderIsNeverRead(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	got := clientaddr.New(nil).Resolve("10.244.1.5:40000", []string{"198.51.100.7"})

	c.Assert(got, qt.Equals, netip.MustParseAddr("10.244.1.5"))
}

func TestAPeerThatIsNoAddressResolvesToNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	got := resolver.Resolve("pipe", []string{"198.51.100.7"})

	c.Assert(got.IsValid(), qt.IsFalse)
	c.Assert(clientaddr.Key(got), qt.Equals, "unknown")
}

// An IPv6 network counts as one client; IPv4 addresses each count alone.
func TestKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, addr, want string
	}{
		{name: "an IPv4 address", addr: "198.51.100.7", want: "198.51.100.7"},
		{name: "an IPv6 address", addr: "2001:db8:0:1:aaaa::7", want: "2001:db8:0:1::/64"},
		{name: "another address of the same /64", addr: "2001:db8:0:1:ffff::1", want: "2001:db8:0:1::/64"},
		{name: "the next /64", addr: "2001:db8:0:2::1", want: "2001:db8:0:2::/64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			c.Assert(clientaddr.Key(netip.MustParseAddr(test.addr)), qt.Equals, test.want)
		})
	}
}
