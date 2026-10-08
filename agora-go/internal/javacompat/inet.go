package javacompat

import (
	"net"
	"strconv"
	"strings"
)

// InetHostAddress formats a socket peer address like Tomcat's
// request.getRemoteAddr(), i.e. java.net.InetAddress.getHostAddress() of the
// accepted socket: an IPv4 (or IPv4-mapped IPv6) address in dotted decimal, an
// IPv6 address as eight lowercase hexadecimal groups without zero compression
// ("0:0:0:0:0:0:0:1" for "::1"), followed by "%<scope id>" for a scoped address
// (the JVM keeps the numeric scope of a socket address). Anything that is not
// an IP address is returned unchanged.
func InetHostAddress(host string) string {
	addr, zone, scoped := strings.Cut(host, "%")
	ip := net.ParseIP(addr)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	var b strings.Builder
	for i := 0; i < net.IPv6len; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(strconv.FormatUint(uint64(ip[i])<<8|uint64(ip[i+1]), 16))
	}
	if scoped && zone != "" {
		if _, err := strconv.Atoi(zone); err != nil {
			// Go names the zone after the interface, the JVM prints its index
			if ifi, err := net.InterfaceByName(zone); err == nil {
				zone = strconv.Itoa(ifi.Index)
			}
		}
		b.WriteString("%" + zone)
	}
	return b.String()
}
