package profile

import (
	"math/rand"
	"strings"
	"testing"

	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// jsonValues is the pool of JSON scalars/containers a client may put in a field.
var jsonValues = []string{
	`null`, `"M"`, `""`, `" "`, `"75"`, `" 75"`, `"2A"`, `5`, `0`, `-0`, `1990`, `1990.5`, `1e3`, `1E+2`, `12345678901234567890`,
	`true`, `false`, `[]`, `{}`, `["x"]`, `{"a":1}`, `"é"`, `"😀"`, `"\u0000"`, `"Paris"`, `[null]`, `[1]`, `[[]]`,
	`"abc"`, `"+1990"`, `"1990 "`, `0.1`, `-12`, `"null"`,
}

func randomObject(r *rand.Rand, fields []string) string {
	// Duplicate property names are left out: the Go decoder keeps the last
	// value while Jackson also rejects an invalid earlier one (see the ledger).
	var parts []string
	seen := map[string]bool{}
	n := r.Intn(len(fields) + 3)
	for i := 0; i < n; i++ {
		name := fields[r.Intn(len(fields))]
		if r.Intn(8) == 0 {
			name = []string{"unknown", "Gender", "GENDER", "yearofbirth", "", "userId"}[r.Intn(6)]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, `"`+name+`":`+jsonValues[r.Intn(len(jsonValues))])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var rawBodies = []string{
	``, ` `, `null`, `[]`, `{}`, `"x"`, `12`, `true`, `{`, `{"gender":`, `{"gender":"M"} trailing`, `{"gender":"M"}{"gender":"F"}`,
	`{'gender':'M'}`, `{gender:"M"}`, `{"gender":"M",}`, `[{"gender":"M"}]`, `/* c */ {}`,
	`{"gender":NaN}`, `{"yearOfBirth":01}`, `{"yearOfBirth":.5}`, `{"yearOfBirth":1.}`, `{"yearOfBirth":+1}`, `{"departments":["a",null]}`,
	`{"departments":[]}`, `{"departments":null}`, `{"departments":"Paris"}`, `{"departments":[null,null,null]}`, `{"loginToken":"x"}`,
	`{"loginToken":null}`, `{"loginToken":1}`, `{"loginToken":true}`, `{"loginToken":["x"]}`, `{"loginToken":{}}`,
}

// compareDecode feeds the same body to the Go decoder and to Jackson (through
// the reference jar) and requires the same outcome and the same re-encoded
// value.
func compareDecode(t *testing.T, className string, body string, goDecode func(string) (string, bool)) {
	t.Helper()
	var want string
	err := oracle.Call("jsonDecode", map[string]any{"className": className, "json": body}, &want)
	javaOK := err == nil && want != "null"
	if err != nil {
		if _, ok := err.(*oracle.OracleError); !ok {
			t.Fatalf("oracle: %v", err)
		}
	}
	got, goOK := goDecode(body)
	if goOK != javaOK {
		t.Errorf("%s %q: go ok=%v (%s), jackson ok=%v (%s / %v)", className, body, goOK, got, javaOK, want, err)
		return
	}
	if goOK && got != want {
		t.Errorf("%s %q:\n go   %s\n java %s", className, body, got, want)
	}
}

func TestOracleProfileJSONDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const class = "fr.gouv.agora.infrastructure.profile.ProfileJson"
	dec := func(body string) (string, bool) {
		var p ProfileJSON
		if err := jsonjava.Unmarshal([]byte(body), &p); err != nil {
			return err.Error(), false
		}
		return jsonjava.MarshalString(p), true
	}
	fields := []string{"gender", "yearOfBirth", "department", "cityType", "jobCategory", "voteFrequency",
		"publicMeetingFrequency", "consultationFrequency", "primaryDepartment", "secondaryDepartment"}
	for _, b := range rawBodies {
		compareDecode(t, class, b, dec)
	}
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 400; i++ {
		compareDecode(t, class, randomObject(r, fields), dec)
	}
}

func TestOracleProfileDepartmentJSONDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const class = "fr.gouv.agora.infrastructure.profile.ProfileDepartmentJson"
	dec := func(body string) (string, bool) {
		var p ProfileDepartmentJSON
		if err := jsonjava.Unmarshal([]byte(body), &p); err != nil {
			return err.Error(), false
		}
		return jsonjava.MarshalString(p), true
	}
	for _, b := range rawBodies {
		compareDecode(t, class, b, dec)
	}
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 400; i++ {
		var elems []string
		for j := r.Intn(5); j > 0; j-- {
			elems = append(elems, jsonValues[r.Intn(len(jsonValues))])
		}
		value := "[" + strings.Join(elems, ",") + "]"
		if r.Intn(6) == 0 {
			value = jsonValues[r.Intn(len(jsonValues))]
		}
		compareDecode(t, class, `{"departments":`+value+`}`, dec)
	}
}

func TestOracleProfileJSONEncodingAndXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	s := func(v string) *string { return &v }
	for _, p := range []ProfileJSON{
		{},
		{Gender: s("M"), YearOfBirth: s("1990"), Department: s("75"), CityType: s("R"), JobCategory: s("AG"), VoteFrequency: s("S"),
			PublicMeetingFrequency: s("P"), ConsultationFrequency: s("J"), PrimaryDepartment: s("Bouches-du-Rhône"), SecondaryDepartment: s("Nord")},
		{Gender: s("é<&\"'"), YearOfBirth: s("")},
	} {
		js := jsonjava.MarshalString(p)
		var want string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": "fr.gouv.agora.infrastructure.profile.ProfileJson", "json": js}, &want)
		if js != want {
			t.Errorf("json:\n go   %s\n java %s", js, want)
		}
		var wantXML string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "fr.gouv.agora.infrastructure.profile.ProfileJson", "json": js}, &wantXML)
		if got := string(xmljava.Marshal(p)); got != wantXML {
			t.Errorf("xml:\n go   %s\n java %s", got, wantXML)
		}
	}
}
