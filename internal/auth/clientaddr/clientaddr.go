// Package clientaddr says which address a request came from, for the limits
// that count sign-in attempts per address.
//
// Behind a proxy the connection's peer is the proxy, so every person shares
// one address. X-Forwarded-For says who the proxy forwarded for, but anyone
// can send the header, so it is read only when the peer is a proxy the
// installer named, and only as far as the proxies the installer named wrote
// it. The header grows rightward: each proxy appends the address it received
// the request from. Reading from the right and stopping at the first address
// no trusted proxy has, the result is the address the outermost trusted proxy
// saw. Whatever a client wrote to the left of that is never read.
//
// What cannot be read with certainty is not guessed at. A malformed entry
// in the part that has to be read, or a chain of more trusted hops than a
// real deployment has, falls back to the peer.
package clientaddr

import (
	"net/netip"
	"strings"
)

// MaxHops is how many entries from the right are read before the header is
// given up on. A real chain of proxies is a few hops long.
const MaxHops = 16

// Header is the header a trusted proxy writes.
const Header = "X-Forwarded-For"

// Resolver resolves a request's address against the trusted proxies.
type Resolver struct {
	trusted []netip.Prefix
}

// New returns a resolver that trusts X-Forwarded-For from the given
// networks, and from nowhere when there are none.
func New(trusted []netip.Prefix) Resolver {
	return Resolver{trusted: append([]netip.Prefix(nil), trusted...)}
}

// Resolve returns the address a request came from. peer is the connection's
// remote address, host and port; forwardedFor holds the request's
// X-Forwarded-For lines in order. It returns the zero Addr only when peer
// itself is not an address.
func (r Resolver) Resolve(peer string, forwardedFor []string) netip.Addr {
	from, ok := parse(peer)
	if !ok || !r.trusts(from) {
		return from
	}
	if len(forwardedFor) == 0 {
		return from
	}
	hops := strings.Split(strings.Join(forwardedFor, ","), ",")
	nearest := from
	for read := range len(hops) {
		if read == MaxHops {
			return from
		}
		hop, ok := parse(strings.TrimSpace(hops[len(hops)-1-read]))
		if !ok {
			return from
		}
		if !r.trusts(hop) {
			return hop
		}
		nearest = hop
	}
	// Every hop was a trusted proxy: the farthest one is all that is known.
	return nearest
}

func (r Resolver) trusts(addr netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// parse reads an address as a peer or a proxy writes it: an address, an
// address and port, or a bracketed IPv6 address with or without a port. An
// address with a zone is refused; no proxy forwards one. An IPv4 address
// written in IPv6 form is read as the IPv4 address it is.
func parse(text string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(text); err == nil {
		text = addrPort.Addr().String()
	}
	text = strings.TrimSuffix(strings.TrimPrefix(text, "["), "]")
	addr, err := netip.ParseAddr(text)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// Key is what a limit counts an address under. An IPv4 address is a key of
// its own. An IPv6 address counts under its /64, the smallest network one
// party is given, so a client cannot spread its attempts over the addresses
// of its own network. The zero Addr is one key, for a peer that was not an
// address.
func Key(addr netip.Addr) string {
	switch {
	case !addr.IsValid():
		return "unknown"
	case addr.Is6():
		prefix, _ := addr.Prefix(64)
		return prefix.String()
	default:
		return addr.String()
	}
}
