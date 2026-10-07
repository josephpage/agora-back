package thematique

import (
	"math/rand"
	"strings"
	"testing"

	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// labels exercising the UTF-16 ordering (supplementary vs U+E000..U+FFFF), accents, case.
var oracleLabels = []string{
	"", "a", "A", "Z", "z", "é", "É", "Éducation", "Environnement", "environnement", "Santé", "Sante",
	"Transports", "Numérique", "Démocratie", "😀", "😀😀", "😁", "𐀀", "Ａ", "ＡＢ", "", "�", " ",
	"a b", "a  b", " a", "a ", "ǅ", "ß", "İ", "ı", " x", "日本", "한국", "\u0000x", "tab\there", "quote\"back\\slash",
}

func randomThematiques(r *rand.Rand) []Thematique {
	n := r.Intn(12)
	out := make([]Thematique, n)
	for i := range out {
		id := "th" + string(rune('a'+r.Intn(26)))
		if r.Intn(8) == 0 {
			id = idThematiqueAutre
		}
		label := oracleLabels[r.Intn(len(oracleLabels))]
		if r.Intn(3) == 0 {
			label += oracleLabels[r.Intn(len(oracleLabels))]
		}
		out[i] = Thematique{ID: id, Label: label, Picto: oracleLabels[r.Intn(len(oracleLabels))]}
	}
	return out
}

// ListThematiqueUseCase + ThematiqueJsonMapper against the JVM: same sort, same JSON bytes.
func TestOracleThematiqueList(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 400; i++ {
		list := randomThematiques(r)
		type th struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Picto string `json:"picto"`
		}
		in := make([]th, len(list))
		for j, x := range list {
			in[j] = th{x.ID, x.Label, x.Picto}
		}
		var want string
		oracle.MustCall(t, "thematiqueList", map[string]any{"thematiques": in}, &want)
		got := jsonjava.MarshalString(ToListJSON(sortThematiques(list)))
		if got != want {
			t.Fatalf("list %q\n go:  %s\n jvm: %s", list, got, want)
		}
	}
}

// The DTOs serialize to the bytes Jackson writes (JSON), also in XML.
func TestOracleThematiqueDTOs(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	list := ToListJSON([]Thematique{{ID: "a", Label: "é😀<&]>", Picto: "p"}, {ID: "b", Label: "", Picto: " "}})
	goJSON := jsonjava.MarshalString(list)
	for _, tc := range []struct {
		class string
		value any
	}{
		{"fr.gouv.agora.infrastructure.thematique.ThematiquesJson", list},
		{"fr.gouv.agora.infrastructure.thematique.ThematiquesJson", ToListJSON(nil)},
		{"fr.gouv.agora.infrastructure.thematique.ThematiqueJson", ToJSON(Thematique{ID: "i", Label: "l", Picto: "p"})},
		{"fr.gouv.agora.infrastructure.thematique.ThematiqueNoIdJson", ToNoIDJSON(Thematique{ID: "i", Label: "l\"", Picto: "p\n"})},
	} {
		in := jsonjava.MarshalString(tc.value)
		var back string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": tc.class, "json": in}, &back)
		if back != in {
			t.Errorf("%s json:\n go:  %s\n jvm: %s", tc.class, in, back)
		}
		var xml string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": tc.class, "json": in}, &xml)
		if got := string(xmljava.Marshal(tc.value)); got != xml {
			t.Errorf("%s xml:\n go:  %s\n jvm: %s", tc.class, got, xml)
		}
	}
	_ = goJSON
	if !strings.HasPrefix(goJSON, `{"thematiques":[`) {
		t.Fatal(goJSON)
	}
}

// ThematiqueStrapiDTO decoding: the same payloads give the same "usable or empty" outcome.
func TestOracleStrapiThematiqueDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	payloads := []string{
		`{"documentId":"a","label":"L","pictogramme":"P"}`,
		`{"documentId":"a","label":"L","pictogramme":"P","id":3,"createdAt":"x","extra":{"y":1}}`,
		`{"documentId":"a","label":"L"}`,
		`{"documentId":"a","label":null,"pictogramme":"P"}`,
		`{"documentId":1,"label":2.5,"pictogramme":true}`,
		`{"documentId":"a","label":["x"],"pictogramme":"P"}`,
		`{}`, `null`,
	}
	for _, p := range payloads {
		env := `{"data":[` + p + `],"meta":{"pagination":{"page":1,"pageSize":100,"pageCount":1,"total":1}}}`
		var back string
		jvmErr := oracle.Call("jsonRoundTrip", map[string]any{
			"className": "fr.gouv.agora.infrastructure.common.StrapiDTO<fr.gouv.agora.infrastructure.thematique.dto.ThematiqueStrapiDTO>",
			"json":      env,
		}, &back)
		var goEnv struct {
			Data []*strapiThematique `json:"data"`
		}
		goErr := jsonjava.Unmarshal([]byte(env), &goEnv)
		if (jvmErr == nil) != (goErr == nil) {
			t.Errorf("payload %s: jvm err=%v go err=%v", p, jvmErr, goErr)
			continue
		}
		if jvmErr == nil && goEnv.Data[0] == nil {
			// Jackson accepts a null element (the Kotlin mapper then throws): nothing more to compare
			if !strings.Contains(back, `"data":[null]`) {
				t.Errorf("payload %s: jvm %s", p, back)
			}
			continue
		}
		if jvmErr == nil && !strings.Contains(back, `"documentId":"`+goEnv.Data[0].DocumentID+`"`) {
			t.Errorf("payload %s: jvm %s go %+v", p, back, goEnv.Data)
		}
	}
}
