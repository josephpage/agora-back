package login

import (
	"math/rand"
	"strings"
	"testing"

	"agora/internal/auth"
	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

var jsonValues = []string{
	`null`, `"x"`, `""`, `" "`, `5`, `-0`, `1.5`, `1e3`, `true`, `false`, `[]`, `{}`, `["x"]`, `{"a":1}`, `"é"`, `"😀"`, `12345678901234567890`,
	`"euxogrrEhl4PRVh40bRahniogi/YOWRgTDtjYUFhOhIK8UeYtso2EbJ3uG7Walwlucljy6EHMr/wCr2RPFF3uQ=="`,
}

var rawBodies = []string{
	``, ` `, `null`, `[]`, `{}`, `"x"`, `12`, `true`, `{`, `{"loginToken":`, `{"loginToken":"x"} trailing`, `{"loginToken":"x"}{"loginToken":"y"}`,
	`{'loginToken':'x'}`, `{loginToken:"x"}`, `{"loginToken":"x",}`, `[{"loginToken":"x"}]`, `{"logintoken":"x"}`, `{"LoginToken":"x"}`,
}

func TestOracleLoginRequestDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const class = "fr.gouv.agora.infrastructure.login.LoginRequestJson"
	check := func(body string) {
		t.Helper()
		var want string
		err := oracle.Call("jsonDecode", map[string]any{"className": class, "json": body}, &want)
		javaOK := err == nil && want != "null"
		if _, isOracle := err.(*oracle.OracleError); err != nil && !isOracle {
			t.Fatalf("oracle: %v", err)
		}
		var v LoginRequestJSON
		goErr := jsonjava.Unmarshal([]byte(body), &v)
		if (goErr == nil) != javaOK {
			t.Errorf("%q: go err=%v, jackson ok=%v (%s / %v)", body, goErr, javaOK, want, err)
			return
		}
		if goErr == nil {
			if got := jsonjava.MarshalString(v); got != want {
				t.Errorf("%q:\n go   %s\n java %s", body, got, want)
			}
		}
	}
	for _, b := range rawBodies {
		check(b)
	}
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 300; i++ {
		var parts []string
		for j := r.Intn(3); j > 0; j-- {
			name := []string{"loginToken", "other"}[r.Intn(2)]
			if j == 2 {
				name = "zz" // no duplicate property names (see the profile oracle test)
			}
			parts = append(parts, `"`+name+`":`+jsonValues[r.Intn(len(jsonValues))])
		}
		check("{" + strings.Join(parts, ",") + "}")
	}
}

func TestOracleSignupAndLoginJSONShapes(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, c := range []struct {
		class string
		value any
	}{
		{"fr.gouv.agora.infrastructure.login.SignupInfoJson", SignupInfoJSON{UserID: "61c9ae56-8d75-4973-941e-4494b3195c47", JWTToken: "a.b.c", JWTExpirationEpochMilli: 1791412410628, LoginToken: "dG9rZW4=", IsModerator: false}},
		{"fr.gouv.agora.infrastructure.login.LoginInfoJson", LoginInfoJSON{JWTToken: "a.b.c", JWTExpirationEpochMilli: 1791412410628, IsModerator: false}},
		{"fr.gouv.agora.infrastructure.login.LoginInfoJson", LoginInfoJSON{JWTToken: "é\"", JWTExpirationEpochMilli: 0, IsModerator: true}},
	} {
		js := jsonjava.MarshalString(c.value)
		var want string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": c.class, "json": js}, &want)
		if js != want {
			t.Errorf("json:\n go   %s\n java %s", js, want)
		}
		var wantXML string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": c.class, "json": js}, &wantXML)
		if got := string(xmljava.Marshal(c.value)); got != wantXML {
			t.Errorf("xml:\n go   %s\n java %s", got, wantXML)
		}
	}
}

// The login token written by signup is what /login decodes (AES, shared env).
func TestOracleLoginTokenRoundTrip(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	lt := auth.NewLoginTokens(auth.LoginTokenConfig{
		EncodeSecret: "E2M9xqpEuqZRkYWNmgIjbw==", EncodeTransformation: "AES/ECB/PKCS5Padding", EncodeAlgorithm: "AES",
		DecodeSecret: "E2M9xqpEuqZRkYWNmgIjbw==", DecodeTransformation: "AES/ECB/PKCS5Padding", DecodeAlgorithm: "AES",
	})
	got, err := lt.Build("61c9ae56-8d75-4973-941e-4494b3195c47")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Token string `json:"token"`
	}
	oracle.MustCall(t, "loginTokenEncode", map[string]any{"userId": "61c9ae56-8d75-4973-941e-4494b3195c47"}, &out)
	if out.Token != got {
		t.Errorf("go %s java %s", got, out.Token)
	}
}
