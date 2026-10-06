package auth_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"agora/internal/auth"
	"agora/parity/oracle"
)

func env(t *testing.T) map[string]string {
	b, err := os.ReadFile("../../parity/env/common.env")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = v
		}
	}
	return m
}

func TestOracleJWTInterop(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	e := env(t)
	j := auth.NewJWT(e["JWT_SECRET"], nil)
	uid := "00000000-0000-4000-9000-000000000001"
	tok, exp, err := j.Generate(uid)
	if err != nil {
		t.Fatal(err)
	}
	if exp-time.Now().UnixMilli() < 86_399_000 {
		t.Errorf("bad exp %d", exp)
	}
	var res struct {
		Valid  bool   `json:"valid"`
		UserID string `json:"userId"`
	}
	oracle.MustCall(t, "jwtParse", map[string]any{"token": tok}, &res)
	if !res.Valid || res.UserID != uid {
		t.Errorf("java parse of go token: %+v", res)
	}
	var gen struct {
		Token string `json:"token"`
		Exp   int64  `json:"expirationEpochMilli"`
	}
	oracle.MustCall(t, "jwtGenerate", map[string]any{"userId": uid}, &gen)
	got, err := j.Parse(gen.Token)
	if err != nil || got != uid {
		t.Errorf("go parse of java token: %v %v", got, err)
	}
	// same header & payload structure (signature differs only by iat)
	if strings.Split(gen.Token, ".")[0] != strings.Split(tok, ".")[0] {
		t.Errorf("header differs: %s vs %s", gen.Token, tok)
	}
	// tampered / malformed tokens: both reject
	for _, bad := range []string{"null", "abc", tok + "x", tok[:len(tok)-2], "a.b.c", "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0."} {
		_, gerr := j.Parse(bad)
		jerr := oracle.Call("jwtParse", map[string]any{"token": bad}, &res)
		if gerr == nil || jerr == nil {
			t.Errorf("token %q: go err=%v java err=%v", bad, gerr, jerr)
		}
	}
}

func TestOracleLoginTokenInterop(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	e := env(t)
	lt := auth.NewLoginTokens(auth.LoginTokenConfig{
		EncodeSecret: e["LOGIN_TOKEN_ENCODE_SECRET"], EncodeTransformation: e["LOGIN_TOKEN_ENCODE_TRANSFORMATION"], EncodeAlgorithm: e["LOGIN_TOKEN_ENCODE_ALGORITHM"],
		DecodeSecret: e["LOGIN_TOKEN_DECODE_SECRET"], DecodeTransformation: e["LOGIN_TOKEN_DECODE_TRANSFORMATION"], DecodeAlgorithm: e["LOGIN_TOKEN_DECODE_ALGORITHM"],
	})
	for _, uid := range []string{"00000000-0000-4000-9000-000000000001", "x", "é\"\\😀", ""} {
		tok, err := lt.Build(uid)
		if err != nil {
			t.Fatal(err)
		}
		var jt struct {
			Token string `json:"token"`
		}
		oracle.MustCall(t, "loginTokenEncode", map[string]any{"userId": uid}, &jt)
		if jt.Token != tok {
			t.Errorf("token(%q): go %s java %s", uid, tok, jt.Token)
		}
		got, err := lt.Decode(jt.Token)
		if err != nil || got != uid {
			t.Errorf("decode(%q): %q %v", uid, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "!!!!", "AAAAAAAAAAAAAAAAAAAAAA==", "AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAA=", " AAAAAAAAAAAAAAAAAAAAAA=="} {
		_, gerr := lt.Decode(bad)
		var r struct{ UserID string `json:"userId"` }
		jerr := oracle.Call("loginTokenDecode", map[string]any{"token": bad}, &r)
		if (gerr == nil) != (jerr == nil) {
			t.Errorf("decode(%q): go err=%v java err=%v", bad, gerr, jerr)
		}
	}
}

func TestOracleIPHash(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	e := env(t)
	h := auth.NewIPHasher(e["REMOTE_ADDRESS_SECRET_KEY_ALGORITHM"], e["REMOTE_ADDRESS_HASH_ITERATIONS"], e["REMOTE_ADDRESS_HASH_KEY_LENGTH"], e["REMOTE_ADDRESS_HASH_SALT"])
	cases := []struct{ xff, xra, remote string }{
		{"1.2.3.4", "", "127.0.0.1"}, {"", "5.6.7.8", "127.0.0.1"}, {"", "", "127.0.0.1"},
		{" , 9.9.9.9, 1.1.1.1", "x", "r"}, {" ", " ", "10.0.0.1"}, {", ,", "8.8.8.8", "r"}, {"2001:db8::1", "", ""}, {" 1.1.1.1 ", "", ""},
	}
	for _, c := range cases {
		var want string
		oracle.MustCall(t, "ipRetrieveHash", map[string]any{"xForwardedFor": c.xff, "xRemoteAddress": c.xra, "remoteAddr": c.remote}, &want)
		got, err := h.Hash(auth.ClientIP(c.xff, c.xra, c.remote))
		if err != nil || got != want {
			t.Errorf("ip hash %+v: go %s java %s (%v)", c, got, want, err)
		}
	}
}
