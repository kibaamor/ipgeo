package ipgeo

import (
	"net/netip"
	"strconv"
	"strings"
)

func finalize(name string, addr netip.Addr, r Result) (Result, error) {
	r.IP = addr
	r.Source = name
	if r.IsEmpty() {
		return Result{}, ErrNotFound
	}
	return r, nil
}

func parseASN(s string) uint32 {
	s = strings.TrimPrefix(s, "AS")
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n) //nolint:gosec // ASN is 32-bit per RFC 6793
}
