package httpx

import (
	"net/http"
	"strings"
)

// Tomcat 10.1 rejects some requests before any Spring code runs. These rules
// were captured from the reference (see parity/scenarios/00_framework.yaml).

const tomcatPageStyle = `<style type="text/css">body {font-family:Tahoma,Arial,sans-serif;} h1, h2, h3, b {color:white;background-color:#525D76;} h1 {font-size:22px;} h2 {font-size:16px;} h3 {font-size:14px;} p {font-size:12px;} a {color:black;} .line {height:1px;background-color:#525D76;border:none;}</style>`

func tomcatErrorPage(status int, reason string) string {
	title := "HTTP Status " + itoa(status) + " – " + reason
	return `<!doctype html><html lang="en"><head><title>` + title + `</title>` + tomcatPageStyle + `</head><body><h1>` + title + `</h1></body></html>`
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

// knownMethods are the methods Spring Security's StrictHttpFirewall allows.
var knownMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "DELETE": true, "OPTIONS": true, "PATCH": true}

// tomcatInvalidTargetByte reports bytes Tomcat's HttpParser rejects in the
// request-target (path or query).
func tomcatInvalidTargetByte(c byte) bool {
	if c <= 0x20 || c >= 0x7f {
		return true
	}
	switch c {
	case '"', '<', '>', '\\', '^', '`', '{', '|', '}', '[', ']':
		return true
	}
	return false
}

func validPercentEncoding(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
			i += 2
		}
	}
	return true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// tomcatReject writes Tomcat-level rejections and returns true when handled.
func (s *Server) tomcatReject(w http.ResponseWriter, r *http.Request) bool {
	h := w.Header()
	writeEmpty := func(status int) {
		h["Content-Length"] = []string{"0"}
		w.WriteHeader(status)
	}
	writePage := func(status int, reason string) {
		page := tomcatErrorPage(status, reason)
		h["Content-Type"] = []string{"text/html;charset=utf-8"}
		h["Content-Language"] = []string{"en"}
		h["Content-Length"] = []string{itoa(len(page))}
		w.WriteHeader(status)
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte(page))
		}
	}
	if r.Method == "CONNECT" {
		writePage(501, "Not Implemented")
		return true
	}
	raw := r.RequestURI
	for i := 0; i < len(raw); i++ {
		if tomcatInvalidTargetByte(raw[i]) {
			writePage(400, "Bad Request")
			return true
		}
	}
	rawPath := raw
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		rawPath = rawPath[:i]
	}
	lp := strings.ToLower(rawPath)
	if strings.Contains(lp, "%2f") || strings.Contains(lp, "%5c") || strings.Contains(lp, "%00") || !validPercentEncoding(rawPath) {
		writePage(400, "Bad Request")
		return true
	}
	if !knownMethods[r.Method] {
		if r.Method == "TRACE" {
			h["Allow"] = []string{"HEAD, DELETE, POST, GET, OPTIONS, PUT"}
		}
		writeEmpty(400)
		return true
	}
	return false
}
