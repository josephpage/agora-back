package javacompat

import (
	"encoding/base64"
	"math/rand"
	"net"
	"strconv"
	"testing"

	"agora/parity/oracle"
)

// The Go side receives the peer address as net/http formats it (net.IP.String
// plus the zone); the JVM side formats the same raw address with
// InetAddress.getHostAddress (what Tomcat's getRemoteAddr returns).
func TestInetHostAddressOracle(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(11))
	type in struct {
		b     []byte
		scope int
	}
	inputs := []in{
		{net.ParseIP("::1"), -1}, {net.ParseIP("::"), -1}, {net.ParseIP("127.0.0.1").To4(), -1},
		{net.ParseIP("::ffff:192.168.1.10"), -1}, {net.ParseIP("2001:db8::1"), -1},
		{net.ParseIP("fe80::1"), 2}, {net.ParseIP("::127.0.0.1"), -1}, {net.ParseIP("64:ff9b::1.2.3.4"), -1},
	}
	for k := 0; k < 500; k++ {
		if rnd.Intn(4) == 0 {
			b := make([]byte, 4)
			rnd.Read(b)
			inputs = append(inputs, in{b, -1})
			continue
		}
		b := make([]byte, 16)
		for j := range b { // many zero groups to exercise compression in Go's format
			if rnd.Intn(2) == 0 {
				b[j] = byte(rnd.Intn(256))
			}
		}
		scope := -1
		if rnd.Intn(5) == 0 {
			scope = rnd.Intn(10)
		}
		inputs = append(inputs, in{b, scope})
	}
	bad := 0
	for _, x := range inputs {
		args := map[string]any{"b64": base64.StdEncoding.EncodeToString(x.b)}
		host := net.IP(x.b).String()
		if x.scope >= 0 {
			args["scope"] = x.scope
			host += "%" + strconv.Itoa(x.scope)
		}
		var want string
		oracle.MustCall(t, "javaInetHostAddress", args, &want)
		if got := InetHostAddress(host); got != want {
			bad++
			if bad < 20 {
				t.Errorf("%s: got %q want %q", host, got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d/%d mismatches", bad, len(inputs))
	}
}
