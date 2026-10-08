package javacompat

import "testing"

func TestInetHostAddress(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1":              "127.0.0.1",
		"::1":                    "0:0:0:0:0:0:0:1",
		"::ffff:10.0.0.1":        "10.0.0.1",
		"2001:db8::ff00:42:8329": "2001:db8:0:0:0:ff00:42:8329",
		"FE80::1%3":              "fe80:0:0:0:0:0:0:1%3",
		"::":                     "0:0:0:0:0:0:0:0",
		"not-an-ip":              "not-an-ip",
		"":                       "",
	} {
		if got := InetHostAddress(in); got != want {
			t.Errorf("InetHostAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
