package httpx

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// head builds a canonical request head of exactly total bytes (padding one header).
func head(method, target string, total int, extra ...string) string {
	fixed := method + " " + target + " HTTP/1.1\r\nHost: localhost\r\n" + strings.Join(extra, "") + "X-Pad: \r\n\r\n"
	return strings.Replace(fixed, "X-Pad: ", "X-Pad: "+strings.Repeat("a", total-len(fixed)), 1)
}

func TestTomcatHeadSizeMatchesRawBytes(t *testing.T) {
	for _, raw := range []string{
		head("GET", "/thematiques", 300),
		head("GET", "/qags?x=1&y=%C3%A9", 8192, "Authorization: Bearer abc\r\n", "Accept: */*\r\n", "Accept: text/html\r\n"),
		head("POST", "/profile", 5000, "Content-Type: application/json\r\n", "Content-Length: 2\r\n"),
		"POST /x HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n",
		"GET / HTTP/1.0\r\n\r\n",
	} {
		r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if got := tomcatHeadSize(r); got != len(raw) {
			t.Errorf("%q...: head size %d, raw %d", raw[:30], got, len(raw))
		}
	}
}

// 8192 bytes of head pass (here: 401 of the security chain), 8193 are Tomcat's 400 page;
// the body does not count.
func TestTomcatHeadLimit(t *testing.T) {
	srv := httptest.NewServer(NewServer(Options{}))
	defer srv.Close()
	send := func(raw string) string {
		c, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := io.WriteString(c, raw); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		conn := "keep-alive"
		if resp.Close { // ReadResponse moves "Connection: close" out of the headers
			conn = "close"
		}
		return resp.Status + "|" + conn + "|" + string(b)
	}
	if got := send(head("GET", "/zz-unknown", 8192, "Connection: close\r\n")); strings.HasPrefix(got, "400") {
		t.Fatalf("8192 bytes rejected: %s", got)
	}
	got := send(head("GET", "/zz-unknown", 8193))
	if !strings.HasPrefix(got, "400 Bad Request|close|<!doctype html>") || !strings.Contains(got, "HTTP Status 400 – Bad Request") {
		t.Fatalf("8193 bytes: %s", got)
	}
	body := strings.Repeat("x", 20000)
	post := strings.Replace(head("POST", "/zz-unknown", 8192, "Content-Length: 20000\r\n", "Connection: close\r\n"), "\r\n\r\n", "\r\n\r\n"+body, 1)
	if got := send(post); strings.HasPrefix(got, "400") {
		t.Fatalf("the body counted: %s", got)
	}
}
