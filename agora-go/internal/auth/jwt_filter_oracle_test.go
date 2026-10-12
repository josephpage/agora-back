package auth_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"agora/internal/auth"
	"agora/internal/javacompat"
	"agora/parity/oracle"
)

// jwtCase builds a token: header and payload JSON texts, how the segments are
// encoded and mutated, and which algorithm signs them.
func jwtCorpus(key []byte, rnd *rand.Rand, n int) []string {
	now := time.Now().Unix()
	uid := "00000000-0000-4000-9000-000000000001"
	headers := []string{`{"alg":"HS256"}`, `{"alg":"Hs256"}`, `{"alg":"HS256","zip":"DEF"}`, `{"alg":"HS256","zip":"def"}`, `{"alg":"HS256","zip":"GZIP"}`, `{"alg":"HS256","zip":""}`, `{"alg":"HS256","zip":1}`, `1x`, `nullx`, "null\x0f", "true\x7f", "null]", "nullé", "null\xc3", "false€", "1\x0f", "1}", "true$", "null_", "false0", "null.", `{"alg":"HS256"}}`, `{"alg":"HS256\u0000"}`, "{\"alg\":\"HS256\"}\t", `{"alg":"HS384"}`, `{"alg":"HS512"}`, `{"typ":"JWT","alg":"HS256"}`, `{"alg":"none"}`, `{"alg":"RS256"}`,
		`{"alg":256}`, `{"alg":"hs256"}`, `{}`, `null`, `[]`, `"x"`, `1`, `{"alg":"HS256"} x`, `{"alg":"HS256","alg":"none"}`, `{"alg":"none","alg":"HS256"}`,
		`{"alg":"HS256"`, "\xef\xbb\xbf{\"alg\":\"HS256\"}", "{\"alg\":\"HS256\",\"x\":\"\xff\"}", `{"alg":"HS256","zip":"XYZ"}`, `{"alg":"HS256","crit":["x"]}`, ` {"alg":"HS256"}`, `{"alg":null}`, `{"alg":""}`}
	subs := []string{`"` + uid + `"`, `""`, `123`, `true`, `null`, `{"a":1}`, `[1]`, `"x"`, `1.5`, `"0-0-0-0-1"`}
	exps := []string{strconv.FormatInt(now+3600, 10), strconv.FormatInt(now-3600, 10), strconv.FormatInt(now+3600, 10) + ".7", `"` + strconv.FormatInt(now+3600, 10) + `"`,
		`"` + time.Unix(now+3600, 0).UTC().Format("2006-01-02T15:04:05Z") + `"`, `"` + time.Unix(now+3600, 0).UTC().Format("2006-01-02T15:04:05.000Z") + `"`,
		`"abc"`, `true`, `[1]`, `{"a":1}`, `null`, `1e30`, `99999999999999999999`, `-1`, `"+` + strconv.FormatInt(now+3600, 10) + `"`, `""`,
		`"2099-13-45T25:61:61Z"`, `"2099-01-01T00:00:00Zjunk"`, `"2099-01-01T00:00:00"`, `"2099-01-01T00:00:00.5Z"`, `"2099-01-01T00:00:00.5000Z"`, `"2099-1-1T0:0:0Z"`,
		`"20990101T000000Z"`, `"2099-01-01 00:00:00Z"`, `" 2099-01-01T00:00:00Z"`, `"١٢٣٤٥٦٧٨٩٠"`, `"-9223372036854775808"`, `"9223372036854775808"`, `-9223372036854775809`, `18446744073709551616000`,
		`4102444800.999`, `-0.5`, `"00004102444800"`, `4.1e9`, `1E10`}
	nbfs := []string{"", strconv.FormatInt(now-10, 10), strconv.FormatInt(now+3600, 10), `"abc"`, `true`, `null`}
	algs := map[string]func() hash.Hash{"HS256": sha256.New, "HS384": sha512.New384, "HS512": sha512.New}
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	junk := []string{"+", "/", "!", "=", "==", " ", "\r", "é", "\u00a0", "Ã©", "\u00ff", "\ufffd", "€", "😀", "\u0100", "-", "_", ".", "*"}
	var out []string
	for k := 0; k < n; k++ {
		h := headers[0]
		if rnd.Intn(3) == 0 {
			h = headers[rnd.Intn(len(headers))]
		}
		var claims []string
		if rnd.Intn(10) != 0 {
			claims = append(claims, `"sub":`+subs[rnd.Intn(len(subs))])
		}
		if rnd.Intn(10) != 0 {
			e := exps[0]
			if rnd.Intn(2) == 0 {
				e = exps[rnd.Intn(len(exps))]
			}
			claims = append(claims, `"exp":`+e)
		}
		if nb := nbfs[rnd.Intn(len(nbfs))]; nb != "" && rnd.Intn(3) == 0 {
			claims = append(claims, `"nbf":`+nb)
		}
		rnd.Shuffle(len(claims), func(i, j int) { claims[i], claims[j] = claims[j], claims[i] })
		p := "{" + strings.Join(claims, ",") + "}"
		switch rnd.Intn(12) {
		case 0:
			p = []string{"null", "[]", "x", "{}", " {}", "{} ", `{"sub":"` + uid + `"`, "{\"sub\":\"\xff\"}"}[rnd.Intn(8)]
		}
		pb := []byte(p)
		switch {
		case strings.Contains(h, `"DEF"`) || strings.Contains(h, `"def"`):
			var b bytes.Buffer
			w := zlib.NewWriter(&b)
			w.Write(pb)
			w.Close()
			pb = b.Bytes()
			if rnd.Intn(4) == 0 {
				pb = pb[:len(pb)/2]
			}
		case strings.Contains(h, `"GZIP"`):
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(pb)
			w.Close()
			pb = b.Bytes()
		}
		h64, p64 := enc([]byte(h)), enc(pb)
		switch rnd.Intn(16) {
		case 0:
			h64 = " " + h64 + "\t"
		case 1:
			p64 = p64 + " "
		case 2:
			p64 = ""
		case 3:
			h64 += "é"
		case 4:
			p64 = "\u00a0" + p64
		}
		if rnd.Intn(8) == 0 { // junk in the header or payload segment (still signed)
			j := junk[rnd.Intn(len(junk))]
			if rnd.Intn(2) == 0 {
				h64 += j
			} else {
				p64 = j + p64
			}
		}
		alg := "HS256"
		for a := range algs {
			if strings.Contains(h, `"`+a+`"`) {
				alg = a
			}
		}
		if rnd.Intn(10) == 0 {
			alg = []string{"HS256", "HS384", "HS512"}[rnd.Intn(3)]
		}
		mac := hmac.New(algs[alg], key)
		mac.Write([]byte(h64 + "." + p64))
		sig := enc(mac.Sum(nil))
		switch rnd.Intn(14) {
		case 0:
			sig += junk[rnd.Intn(len(junk))]
		case 1:
			sig = junk[rnd.Intn(len(junk))] + sig
		case 2: // an 'A' (or any char) replaced by '='
			i := rnd.Intn(len(sig))
			sig = sig[:i] + "=" + sig[i+1:]
		case 3:
			sig = sig[:len(sig)-1]
		case 4:
			sig = ""
		case 5:
			sig = strings.ReplaceAll(strings.ReplaceAll(sig, "-", "+"), "_", "/")
		case 6:
			sig += "=="
		case 7:
			if len(sig) > 77 {
				sig = sig[:76] + "\r\n" + sig[76:]
			}
		case 8:
			sig += "é"
		}
		tok := h64 + "." + p64 + "." + sig
		switch rnd.Intn(20) {
		case 0:
			tok += "."
		case 1:
			tok = "." + p64 + "." + sig
		case 2:
			tok = h64 + "." + p64
		case 3:
			tok = strings.Replace(tok, ".", "..", 1)
		}
		out = append(out, tok)
	}
	return out
}

// The outcome of AuthenticationTokenFilter (real jjwt + JwtTokenUtils) against JWT.Parse.
func TestOracleJWTFilter(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	e := env(t)
	key, err := base64.StdEncoding.DecodeString(e["JWT_SECRET"])
	if err != nil {
		t.Fatal(err)
	}
	j := auth.NewJWT(e["JWT_SECRET"], nil)
	seed := int64(31)
	if v, err := strconv.ParseInt(os.Getenv("JWT_FUZZ_SEED"), 10, 64); err == nil {
		seed = v
	}
	rnd := rand.New(rand.NewSource(seed))
	toks := jwtCorpus(key, rnd, 20000)
	toks = append(toks, "", " ", "\t", "a", "a.b", "a.b.c", "..", ".", "a..c", " ", "=", "=.=.=", "\ufffd", "a.b.\ufffd", "\ufffd.b.c", "x\ufffdy.b.c", "😀", "😀.😀.😀")
	kinds := map[string]int{}
	fails := 0
	for _, tok := range toks {
		var want struct {
			Outcome string  `json:"outcome"`
			UserID  *string `json:"userId"`
			Error   string  `json:"error"`
		}
		oracle.MustCall(t, "jwtFilter", map[string]any{"token": tok}, &want)
		kinds[want.Outcome]++
		got, gerr := j.Parse(tok)
		goOutcome := "auth"
		switch {
		case errors.Is(gerr, auth.ErrInvalidJWT):
			goOutcome = "anonymous"
		case gerr != nil:
			goOutcome = "500"
		}
		javaOutcome := want.Outcome
		if javaOutcome == "expired" {
			javaOutcome = "anonymous"
		}
		if javaOutcome == "auth" && want.UserID == nil {
			javaOutcome = "500" // loginWithUserId(null): NullPointerException
		}
		same := javaOutcome == goOutcome
		if same && goOutcome == "auth" {
			// the user lookup only finds UUIDs: any two non-UUID ids are the same anonymous request
			_, jUUID := javacompat.ToUUIDOrNull(*want.UserID)
			_, gUUID := javacompat.ToUUIDOrNull(got)
			same = (!jUUID && !gUUID) || *want.UserID == got
		}
		if !same && goOutcome == "anonymous" && javaOutcome == "auth" {
			_, jUUID := javacompat.ToUUIDOrNull(*want.UserID)
			same = !jUUID
		}
		if !same {
			fails++
			if fails <= 40 {
				t.Errorf("%q: Java %s %v %s, Go %s %q %v", tok, want.Outcome, deref(want.UserID), want.Error, goOutcome, got, gerr)
			}
		}
	}
	t.Logf("%d tokens, Java outcomes %v, %d differences", len(toks), kinds, fails)
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%q", *s)
}
