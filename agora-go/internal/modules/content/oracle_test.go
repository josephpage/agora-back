package content

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// These tests run the REAL Kotlin repositories / use cases / controllers of the
// reference jar (parity/oracle/java/S9Functions.java) on the same payloads and
// compare byte for byte. PARITY_ORACLE=1.

func useUTC(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
}

// ---------------------------------------------------------------------------
// Jackson java.time

func mutateString(r *rand.Rand, s string) string {
	const alphabet = "0123456789-:.TtZz+ \tx"
	b := []byte(s)
	for n := r.Intn(3); n > 0; n-- {
		switch r.Intn(3) {
		case 0:
			if len(b) > 0 {
				b[r.Intn(len(b))] = alphabet[r.Intn(len(alphabet))]
			}
		case 1:
			if len(b) > 0 {
				i := r.Intn(len(b))
				b = append(b[:i], b[i+1:]...)
			}
		default:
			i := r.Intn(len(b) + 1)
			b = append(b[:i], append([]byte{alphabet[r.Intn(len(alphabet))]}, b[i:]...)...)
		}
	}
	return string(b)
}

func pick[T any](r *rand.Rand, l ...T) T { return l[r.Intn(len(l))] }

func randDigits(r *rand.Rand, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(byte('0' + r.Intn(10)))
	}
	return b.String()
}

// randDateText builds a LocalDate / LocalDateTime candidate.
func randDateText(r *rand.Rand, withTime bool) string {
	var b strings.Builder
	b.WriteString(pick(r, "", "", "", "", "+", "-"))
	b.WriteString(randDigits(r, pick(r, 4, 4, 4, 4, 4, 1, 3, 5, 6, 9, 10, 11)))
	b.WriteString("-")
	b.WriteString(pick(r, fmt.Sprintf("%02d", 1+r.Intn(12)), fmt.Sprintf("%02d", 1+r.Intn(12)), fmt.Sprintf("%02d", r.Intn(15)), strconv.Itoa(1+r.Intn(12))))
	b.WriteString("-")
	b.WriteString(pick(r, fmt.Sprintf("%02d", 1+r.Intn(28)), fmt.Sprintf("%02d", 1+r.Intn(28)), fmt.Sprintf("%02d", 1+r.Intn(31)), fmt.Sprintf("%02d", r.Intn(34))))
	if withTime || r.Intn(4) == 0 {
		b.WriteString(pick(r, "T", "T", "T", "t", " "))
		b.WriteString(pick(r, fmt.Sprintf("%02d", r.Intn(24)), fmt.Sprintf("%02d", r.Intn(26))))
		b.WriteString(":")
		b.WriteString(pick(r, fmt.Sprintf("%02d", r.Intn(60)), fmt.Sprintf("%02d", r.Intn(62))))
		if r.Intn(3) > 0 {
			b.WriteString(":")
			b.WriteString(pick(r, fmt.Sprintf("%02d", r.Intn(60)), fmt.Sprintf("%02d", r.Intn(62))))
			if r.Intn(2) == 0 {
				b.WriteString(pick(r, ".", ".", ","))
				b.WriteString(randDigits(r, pick(r, 0, 1, 2, 3, 3, 6, 9, 10)))
			}
		}
	}
	b.WriteString(pick(r, "", "", "", "Z", "Z", "z", "+02:00", "ZZ", " ", "[UTC]"))
	return mutateStringMaybe(r, b.String())
}

func mutateStringMaybe(r *rand.Rand, s string) string {
	if r.Intn(4) == 0 {
		return mutateString(r, s)
	}
	return s
}

var fixedDateJSON = []string{
	`"2024-03-01"`, `"2024-03-01T10:00:00.000Z"`, `"2024-03-01T10:00:00"`, `"2024-03-01T10:00"`, `"2024-03-01T10:00:00+02:00"`, `"2024-03-01TZ"`,
	`"2024-03-01T"`, `"2024-03-01Z"`, `"2024-03-01Txxz"`, `"2024-03-01T10:00:00ZZ"`, `"+10000-01-01"`, `"-0001-01-01"`, `"0000-01-01"`, `"-0000-01-01"`,
	`""`, `"  "`, `"x"`, `null`, `true`, `false`, `{}`, `{"a":1}`, `[]`, `[null]`, `1`, `0`, `-1`, `19800`, `19800.5`, `1e3`, `-0`, `99999999999`,
	`365241780471`, `365241780472`, `-365243219162`, `-365243219163`, `9223372036854775807`, `9223372036854775808`,
	`[2024,3,1]`, `[2024,3,1,10,0]`, `[2024,3,1,10,0,5]`, `[2024,3,1,10,0,5,7]`, `[2024,3]`, `[2024,13,1]`, `[2024.0,3,1]`, `["2024",3,1]`, `[2024,3,1,10]`,
	`[2024,3,1,10,0,5,7,8]`, `[2024,3,1,24,0]`, `[2024,3,1,10,60]`, `[2024,3,1,10,0,60]`, `[2024,3,1,10,0,5,1000000000]`, `[1,1,1]`, `[-1,1,1]`,
	`[999999999,12,31]`, `[1000000000,1,1]`, `[2024,2,30]`, `[2147483648,1,1]`,
}

type javaTimeResult struct {
	Ok        bool   `json:"ok"`
	Value     string `json:"value"`
	Formatted string `json:"formatted"`
	Exception string `json:"exception"`
}

// javaDateString is LocalDate.toString().
func javaDateString(d LocalDate) string {
	abs := d.Year
	if abs < 0 {
		abs = -abs
	}
	var y string
	if abs < 1000 {
		y = fmt.Sprintf("%04d", abs)
		if d.Year < 0 {
			y = "-" + y
		}
	} else {
		y = strconv.FormatInt(d.Year, 10)
		if d.Year > 9999 {
			y = "+" + y
		}
	}
	return fmt.Sprintf("%s-%02d-%02d", y, d.Month, d.Day)
}

// javaDateTimeString is LocalDateTime.toString().
func javaDateTimeString(d LocalDateTime) string {
	s := fmt.Sprintf("%sT%02d:%02d", javaDateString(d.Date), d.Hour, d.Minute)
	if d.Second > 0 || d.Nano > 0 {
		s += fmt.Sprintf(":%02d", d.Second)
		switch {
		case d.Nano == 0:
		case d.Nano%1_000_000 == 0:
			s += fmt.Sprintf(".%03d", d.Nano/1_000_000)
		case d.Nano%1000 == 0:
			s += fmt.Sprintf(".%06d", d.Nano/1000)
		default:
			s += fmt.Sprintf(".%09d", d.Nano)
		}
	}
	return s
}

func TestOracleLocalDate(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(9))
	cases := append([]string(nil), fixedDateJSON...)
	for i := 0; i < 2500; i++ {
		cases = append(cases, strconv.Quote(randDateText(r, false)))
	}
	for _, raw := range cases {
		var want javaTimeResult
		oracle.MustCall(t, "s9LocalDate", map[string]any{"json": raw}, &want)
		var d LocalDate
		err := d.UnmarshalJavaTree(decodeTree(t, raw))
		if (err == nil) != want.Ok {
			t.Fatalf("%s: go ok=%v, jvm ok=%v (%s)", raw, err == nil, want.Ok, want.Exception)
		}
		if err == nil {
			if got := javaDateString(d); got != want.Value {
				t.Fatalf("%s: go %s, jvm %s", raw, got, want.Value)
			}
			if got := d.formatStartOfDay(); got != want.Formatted {
				t.Fatalf("%s: go formatted %s, jvm %s", raw, got, want.Formatted)
			}
		}
	}
}

func TestOracleLocalDateTime(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(10))
	cases := append([]string(nil), fixedDateJSON...)
	for i := 0; i < 3500; i++ {
		cases = append(cases, strconv.Quote(randDateText(r, true)))
	}
	for _, raw := range cases {
		var want javaTimeResult
		oracle.MustCall(t, "s9LocalDateTime", map[string]any{"json": raw}, &want)
		var d LocalDateTime
		err := d.UnmarshalJavaTree(decodeTree(t, raw))
		if (err == nil) != want.Ok {
			t.Fatalf("%s: go ok=%v, jvm ok=%v (%s)", raw, err == nil, want.Ok, want.Exception)
		}
		if err == nil {
			if got := javaDateTimeString(d); got != want.Value {
				t.Fatalf("%s: go %s, jvm %s", raw, got, want.Value)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Strapi payload generators

var richTexts = []string{"", "a", "é😀", "a & <b> \"q\"", "<br/>", "x<br/>", "\u2028", "\\", "]]>"}

func randText(r *rand.Rand) map[string]any {
	n := map[string]any{"type": "text", "text": pick(r, richTexts...)}
	for _, m := range []string{"bold", "italic", "underline", "strikethrough", "code"} {
		switch r.Intn(6) {
		case 0:
			n[m] = true
		case 1:
			n[m] = false
		case 2:
			n[m] = nil
		}
	}
	return n
}

func randRichNode(r *rand.Rand, depth int) any {
	kids := func() any {
		if depth <= 0 || r.Intn(6) == 0 {
			return []any{randText(r)}
		}
		var out []any
		for i := r.Intn(3); i >= 0; i-- {
			out = append(out, randRichNode(r, depth-1))
		}
		return out
	}
	switch r.Intn(12) {
	case 0:
		return randText(r)
	case 1:
		return map[string]any{"type": "paragraph", "children": kids()}
	case 2:
		return map[string]any{"type": "heading", "level": pick[any](r, 1, 2, 3, 4, 5, 6, 7, 0, nil, "2"), "children": kids()}
	case 3:
		return map[string]any{"type": "list", "format": pick(r, "ordered", "unordered", "x"), "children": kids()}
	case 4:
		return map[string]any{"type": "list-item", "children": kids()}
	case 5:
		return map[string]any{"type": "quote", "children": kids()}
	case 6:
		return map[string]any{"type": "link", "url": pick(r, "https://x/?a=1&b=2", "u\"", ""), "children": []any{randText(r), randText(r)}}
	case 7:
		return map[string]any{"type": pick(r, "image", "code", "zzz", ""), "children": kids()}
	case 8:
		return map[string]any{"children": kids()}
	case 9:
		return map[string]any{"type": "paragraph"}
	case 10:
		return nil
	}
	return map[string]any{"type": "paragraph", "children": []any{}}
}

func randRich(r *rand.Rand) any {
	switch r.Intn(10) {
	case 0:
		return "texte"
	case 1:
		return 5
	case 2:
		return map[string]any{}
	case 3:
		return nil
	case 4:
		return []any{}
	}
	var out []any
	for i := r.Intn(4); i >= 0; i-- {
		out = append(out, randRichNode(r, 2))
	}
	return out
}

var stringPool = []any{nil, 5, 1.5, true, false, "", "é😀 & <>", []any{}, []any{"a"}, map[string]any{}, "x", " lead ", "a\u2028b", "tab\t", "lorem ipsum"}

func randWrong(r *rand.Rand) any { return stringPool[r.Intn(len(stringPool))] }

var dateJSONPool = []any{"2024-03-01", "2024-03-01T10:00:00.000Z", "2024-03-01T10:00:00", "2024-03-01T10:00:00+02:00", "2024-02-30", "+10000-01-01", "-0001-01-01",
	"", "x", nil, 19800, 1.5, true, []any{2024, 3, 1}, []any{2024, 3}, "2024-03-01TZ", "2024-3-1", " 2024-03-01 "}

var dateTimePool = []any{"2024-03-01T10:00:00.000Z", "2024-03-01T10:00:00Z", "2024-03-01T10:00:00", "2024-03-01T10:00", "2024-03-01T10:00:00+02:00", "2024-03-01",
	"", "x", nil, 1577872800000, true, []any{2024, 3, 1, 10, 0}, "2025-01-01T00:00:00.123456", "2023-12-31T23:59:59.999999999Z"}

func randPresent(r *rand.Rand, good any, pool []any) any {
	switch x := r.Intn(10); {
	case x < 7:
		return good
	case x < 8:
		return pool[r.Intn(len(pool))]
	default:
		return randWrong(r)
	}
}

// mutateFields drops / corrupts a few fields of the document.
func mutateFields(r *rand.Rand, doc map[string]any, kinds map[string]byte) {
	for name, k := range kinds {
		if r.Intn(8) != 0 {
			continue
		}
		if r.Intn(3) == 0 {
			delete(doc, name)
			continue
		}
		switch k {
		case 'r':
			doc[name] = randRich(r)
		case 'd':
			doc[name] = dateJSONPool[r.Intn(len(dateJSONPool))]
		case 't':
			doc[name] = dateTimePool[r.Intn(len(dateTimePool))]
		default:
			doc[name] = randWrong(r)
		}
	}
}

var ficheKinds = map[string]byte{
	"documentId": 's', "etape_1_lancement": 'r', "etape_2_analyse": 'r', "etape_3_suivi": 'r', "titre": 's', "debut": 'd', "fin": 'd', "porteur": 's',
	"lien_site": 's', "condition_participation": 's', "modalite_participation": 's', "etape": 's', "annee_de_lancement": 's', "type": 's',
}

func randFiche(r *rand.Rand, id string) map[string]any {
	d := ficheDoc(id, map[string]any{
		"etape_1_lancement": randPresent(r, []any{para("L", "<x>")}, nil2(randRichPool(r))),
		"debut":             randPresent(r, pick(r, "2024-03-01", "2024-03-01T10:00:00.000Z", "2000-02-29", "2023-12-31"), dateJSONPool),
		"fin":               randPresent(r, "2024-06-30", dateJSONPool),
		"titre":             pick[any](r, "Titre", "Débat é😀", "Grande consultation", "x"),
		"etape":             pick[any](r, "Lancement", "Analyse", "Suivi"),
	})
	mutateFields(r, d, ficheKinds)
	switch r.Intn(7) {
	case 0:
		d["thematique"] = randWrong(r)
	case 1:
		th := d["thematique"].(map[string]any)
		for _, k := range []string{"documentId", "label", "pictogramme"} {
			if r.Intn(3) == 0 {
				if r.Intn(2) == 0 {
					delete(th, k)
				} else {
					th[k] = randWrong(r)
				}
			}
		}
	case 2:
		delete(d, "thematique")
	}
	switch r.Intn(9) {
	case 0:
		d["illustration"] = randWrong(r)
	case 1:
		delete(d, "illustration")
	case 2:
		d["illustration"] = map[string]any{"url": "https://u", "formats": map[string]any{"medium": map[string]any{"url": "https://m"}}}
	case 3:
		d["illustration"] = map[string]any{"url": pick(r, stringPool...), "formats": pick[any](r, nil, map[string]any{}, map[string]any{"medium": nil}, map[string]any{"medium": map[string]any{}}, "x")}
	case 4:
		d["illustration"] = map[string]any{"url": "https://u"}
	}
	if r.Intn(5) == 0 {
		d["unknown"] = map[string]any{"a": []any{1}}
	}
	return d
}

func randRichPool(r *rand.Rand) any { return randRich(r) }

// nil2 keeps the value as the (single element) pool of randPresent.
func nil2(v any) []any { return []any{v} }

func randEnvelope(r *rand.Rand, docs []map[string]any) string {
	switch r.Intn(25) {
	case 0:
		return "not json"
	case 1:
		return `{"data":null,"meta":{}}`
	case 2:
		return `{"data":[]}`
	case 3:
		return `{"error":{"status":500}}`
	case 4:
		return `{"data":[null],"meta":{"pagination":{"page":1,"pageSize":1,"pageCount":1,"total":1}}}`
	case 5:
		return ""
	}
	return envelope(docs...)
}

func goFiches(ta *testApp, f ficheFilters) (string, bool) {
	list, err := getAllFiches(context.Background(), ta.App, f)
	if err != nil {
		return "", false
	}
	out := make(FicheInventaireListJSON, len(list))
	for i, fi := range list {
		out[i] = toFicheInventaireJSON(fi)
	}
	return jsonjava.MarshalString(out), true
}

type pipelineResult struct {
	JSON         *string  `json:"json"`
	URI          *string  `json:"uri"`
	Key          *string  `json:"key"`
	Exception    string   `json:"exception"`
	Message      string   `json:"message"`
	Titre        *string  `json:"titre"`
	Thematique   *string  `json:"thematique"`
	Etape        []string `json:"etape"`
	Condition    []string `json:"condition"`
	Modalite     []string `json:"modalite"`
	Annee        *string  `json:"annee"`
	CacheControl *string  `json:"cacheControl"`
}

func randFilterString(r *rand.Rand) *string {
	if r.Intn(3) == 0 {
		return nil
	}
	return sp(pick(r, "", " ", "  x  ", "consultation", "Débat", "é😀", "null", "a b", "a&b=c", "100%", "\t", "\u00a0", "th0000000000000000000001", "2024", "a,b"))
}

func randFilterList(r *rand.Rand) []string {
	switch r.Intn(5) {
	case 0:
		return nil
	case 1:
		return []string{}
	}
	var l []string
	for i := r.Intn(4); i >= 0; i-- {
		l = append(l, pick(r, "", " ", "Lancement", " Analyse ", "Suivi", "a,b", "é", "😀", "\uff5e", "Z", "null", "x y", "\u00a0"))
	}
	return l
}

func TestOracleFiches(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	useUTC(t)
	r := rand.New(rand.NewSource(21))
	for i := 0; i < 600; i++ {
		var docs []map[string]any
		for n := r.Intn(4); n > 0; n-- {
			docs = append(docs, randFiche(r, fmt.Sprintf("fi%d", n)))
		}
		payload := randEnvelope(r, docs)
		titre, thematique, annee := randFilterString(r), randFilterString(r), randFilterString(r)
		etape, cond, modal := randFilterList(r), randFilterList(r), randFilterList(r)
		f := newFicheFilters(titre, thematique, etape, cond, modal, annee)

		var want pipelineResult
		oracle.MustCall(t, "s9Fiches", map[string]any{"payload": payload, "titre": titre, "thematique": thematique, "etape": etape,
			"conditionParticipation": cond, "modaliteParticipation": modal, "annee": annee}, &want)

		// filters: derived values and cache key
		if !eqPtr(want.Titre, f.titre) || !eqPtr(want.Thematique, f.thematique) || !eqPtr(want.Annee, f.anneeDeLancement) ||
			!eqList(want.Etape, f.etape) || !eqList(want.Condition, f.conditionParticipation) || !eqList(want.Modalite, f.modaliteParticipation) {
			t.Fatalf("filters differ:\n jvm %+v\n go  %+v", want, f)
		}
		if want.Key == nil || *want.Key != f.cacheKey() {
			t.Fatalf("cache key: jvm %v, go %q", want.Key, f.cacheKey())
		}
		ta := newTestApp(t, constBody(payload))
		got, ok := goFiches(ta, f)
		if want.URI == nil || *want.URI != ta.strapi.lastURI() {
			t.Fatalf("uri: jvm %v, go %q", want.URI, ta.strapi.lastURI())
		}
		if want.JSON == nil {
			if ok {
				t.Fatalf("payload %s\n jvm exception %s, go %s", payload, want.Exception, got)
			}
			continue
		}
		if !ok || got != *want.JSON {
			t.Fatalf("payload %s\n jvm %s\n go  %s (ok=%v)", payload, *want.JSON, got, ok)
		}
	}
}

func derefStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func eqList(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return strings.Join(a, "\x00") == strings.Join(b, "\x00") && len(a) == len(b)
}

func TestOracleFiche(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	useUTC(t)
	r := rand.New(rand.NewSource(22))
	for i := 0; i < 400; i++ {
		var docs []map[string]any
		for n := r.Intn(3); n > 0; n-- {
			docs = append(docs, randFiche(r, fmt.Sprintf("fi%d", n)))
		}
		payload := randEnvelope(r, docs)
		id := pick(r, "fi1", "fi2", "x y", "é", "a,b", "a&b", "😀")
		var want pipelineResult
		oracle.MustCall(t, "s9Fiche", map[string]any{"payload": payload, "id": id}, &want)
		ta := newTestApp(t, constBody(payload))
		fiche, err := getFiche(context.Background(), ta.App, id)
		if want.URI == nil || *want.URI != ta.strapi.lastURI() {
			t.Fatalf("uri: jvm %v, go %q", want.URI, ta.strapi.lastURI())
		}
		switch {
		case want.Exception != "":
			if err == nil {
				t.Fatalf("payload %s: jvm exception %s, go %+v", payload, want.Exception, fiche)
			}
		case want.JSON == nil:
			if err != nil || fiche != nil {
				t.Fatalf("payload %s: jvm null, go %+v %v", payload, fiche, err)
			}
		default:
			if err != nil || fiche == nil || jsonjava.MarshalString(toFicheInventaireJSON(*fiche)) != *want.JSON {
				t.Fatalf("payload %s\n jvm %s\n go  %+v %v", payload, *want.JSON, fiche, err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// content pages

type pageSpec struct {
	route, javaPage, model string
	kinds                  map[string]byte
	good                   map[string]any
}

func pageSpecs() []pageSpec {
	rich := func() []any {
		return []any{para("Un ", "<b>"), map[string]any{"type": "heading", "level": 2, "children": []any{map[string]any{"type": "text", "text": "T", "bold": true}}}}
	}
	return []pageSpec{
		{"page-questions-au-gouvernement", "questions", "page-questions-au-gouvernement",
			map[string]byte{"information_bottomsheet": 's', "nombre_de_questions": 's', "programme_du_mois": 'r', "comment_ca_marche": 's'},
			map[string]any{"information_bottomsheet": "i", "nombre_de_questions": "{} questions {}", "programme_du_mois": rich(), "comment_ca_marche": "c"}},
		{"page-reponses-aux-qags", "reponses", "page-reponse-aux-questions-au-gouvernement",
			map[string]byte{"information_reponse_a_venir_bottomsheet": 's'}, map[string]any{"information_reponse_a_venir_bottomsheet": "info é"}},
		{"page-poser-ma-question", "poser", "page-poser-ma-question", map[string]byte{"texte_regles": 'r'}, map[string]any{"texte_regles": rich()}},
		{"page-site-vitrine-accueil", "accueil", "site-vitrine-accueil",
			map[string]byte{"titre_header": 's', "sous_titre_header": 's', "titre_body": 's', "description_body": 's', "texte_image_1": 'r', "texte_image_2": 'r', "texte_image_3": 'r'},
			map[string]any{"titre_header": "h", "sous_titre_header": "sh", "titre_body": "b", "description_body": "d", "texte_image_1": rich(), "texte_image_2": rich(), "texte_image_3": rich()}},
		{"page-site-vitrine-conditions-generales", "cgu", "site-vitrine-conditions-generales-d-utilisation",
			map[string]byte{"conditions_generales_d_utilisation": 'r'}, map[string]any{"conditions_generales_d_utilisation": rich()}},
		{"page-site-vitrine-consultation", "consultation", "site-vitrine-consultation", map[string]byte{"donnez_votre_avis": 'r'}, map[string]any{"donnez_votre_avis": rich()}},
		{"page-site-vitrine-declaration-accessibilite", "declaration", "site-vitrine-declaration-d-accessibilite",
			map[string]byte{"declaration": 'r'}, map[string]any{"declaration": rich()}},
		{"page-site-vitrine-mentions-legales", "mentions", "site-vitrine-mentions-legale", map[string]byte{"mentions_legales": 'r'}, map[string]any{"mentions_legales": rich()}},
		{"page-site-vitrine-politique-confidentialite", "politique", "site-vitrine-politique-de-confidentialite",
			map[string]byte{"politique_de_confidentialite": 'r'}, map[string]any{"politique_de_confidentialite": rich()}},
		{"page-site-vitrine-question-au-gouvernement", "qag", "site-vitrine-question-au-gouvernement",
			map[string]byte{"titre": 's', "sous_titre": 's', "texte_soutien": 'r'}, map[string]any{"titre": "t", "sous_titre": "st", "texte_soutien": rich()}},
	}
}

func TestOraclePages(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	useUTC(t)
	r := rand.New(rand.NewSource(23))
	old := qagCount
	defer func() { qagCount = old }()
	for _, p := range pageSpecs() {
		for i := 0; i < 120; i++ {
			doc := map[string]any{}
			for k, v := range p.good {
				doc[k] = v
			}
			mutateFields(r, doc, p.kinds)
			payload := mustJSON(map[string]any{"data": doc, "meta": map[string]any{}})
			switch r.Intn(15) {
			case 0:
				payload = "garbage"
			case 1:
				payload = `{"data":null}`
			case 2:
				payload = `{"data":[]}`
			case 3:
				payload = `{"data":{}}`
			}
			count := r.Intn(1000)
			qagCount = func(context.Context, *app.App) int { return count }
			var want pipelineResult
			oracle.MustCall(t, "s9Page", map[string]any{"page": p.javaPage, "payload": payload, "qagCount": count}, &want)
			ta := newTestApp(t, constBody(payload)).routes()
			got := ta.get("/content/" + p.route)
			if want.JSON == nil {
				if got.status != 500 {
					t.Fatalf("%s %s: jvm exception %s, go %d %s", p.route, payload, want.Exception, got.status, got.body)
				}
				continue
			}
			if got.status != 200 || got.body != *want.JSON {
				t.Fatalf("%s %s\n jvm %s\n go  %d %s", p.route, payload, *want.JSON, got.status, got.body)
			}
			if want.CacheControl == nil || *want.CacheControl != got.header.Get("Cache-Control") {
				t.Fatalf("Cache-Control: jvm %v, go %q", want.CacheControl, got.header.Get("Cache-Control"))
			}
			if want.URI == nil || *want.URI != ta.strapi.lastURI() {
				t.Fatalf("uri: jvm %v, go %q", want.URI, ta.strapi.lastURI())
			}
		}
	}
}

// ---------------------------------------------------------------------------
// news and charter

func randNews(r *rand.Rand, now time.Time) map[string]any {
	d := newsDoc(randDateTimeJSON(r, now), map[string]any{
		"message":                    randPresent(r, []any{para("m", "é")}, nil2(randRich(r))),
		"short_message":              pick[any](r, "court", "é😀"),
		"page_route_mobile_enum":     pick[any](r, "qag", "consultation", "themeHebdo"),
		"page_route_argument_mobile": pick[any](r, nil, "arg", "é"),
	})
	mutateFields(r, d, map[string]byte{"message": 'r', "short_message": 's', "call_to_action": 's', "date_de_debut": 't', "page_route_mobile_enum": 's', "page_route_argument_mobile": 's'})
	return d
}

func randDateTimeJSON(r *rand.Rand, now time.Time) any {
	switch r.Intn(4) {
	case 0:
		return now.Add(time.Duration(r.Intn(20)-10) * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	case 1:
		return now.Add(time.Duration(r.Intn(2000)-1000) * time.Millisecond).Format("2006-01-02T15:04:05.000")
	case 2:
		return pick(r, "2020-01-01T00:00:00.000Z", "2020-01-01T00:00:00.000Z", "2021-05-05T05:05:05Z", now.Format("2006-01-02T15:04:05.000Z"))
	}
	return dateTimePool[r.Intn(len(dateTimePool))]
}

func TestOracleNews(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	useUTC(t)
	r := rand.New(rand.NewSource(24))
	now := time.Date(2026, 10, 6, 12, 30, 45, 123_000_000, time.UTC)
	for i := 0; i < 500; i++ {
		var docs []map[string]any
		for n := r.Intn(5); n > 0; n-- {
			docs = append(docs, randNews(r, now))
		}
		payload := randEnvelope(r, docs)
		var want pipelineResult
		oracle.MustCall(t, "s9News", map[string]any{"payload": payload, "nowNs": now.UnixNano()}, &want)
		ta := newTestApp(t, constBody(payload)).routes()
		ta.now = now
		got := ta.get("/welcome_page/last_news")
		if want.URI == nil || *want.URI != ta.strapi.lastURI() {
			t.Fatalf("uri: jvm %v, go %q", want.URI, ta.strapi.lastURI())
		}
		switch {
		case want.Exception != "":
			if got.status != 500 {
				t.Fatalf("%s: jvm exception %s, go %d %s", payload, want.Exception, got.status, got.body)
			}
		case want.JSON == nil:
			if got.status != 404 || got.body != "" {
				t.Fatalf("%s: jvm null, go %d %s", payload, got.status, got.body)
			}
		default:
			if got.status != 200 || got.body != *want.JSON {
				t.Fatalf("%s\n jvm %s\n go  %d %s", payload, *want.JSON, got.status, got.body)
			}
		}
	}
}

func TestOracleCharter(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	useUTC(t)
	r := rand.New(rand.NewSource(25))
	now := time.Date(2026, 10, 6, 12, 30, 45, 123_456_000, time.UTC)
	for i := 0; i < 400; i++ {
		var docs []map[string]any
		for n := r.Intn(4); n > 0; n-- {
			d := map[string]any{
				"documentId": "c", "charte": randPresent(r, []any{para("Charte", "é")}, nil2(randRich(r))),
				"charte_preview": randPresent(r, []any{para("Aperçu")}, nil2(randRich(r))), "datetime_debut": randDateTimeJSON(r, now),
			}
			mutateFields(r, d, map[string]byte{"charte": 'r', "charte_preview": 'r', "datetime_debut": 't'})
			docs = append(docs, d)
		}
		payload := randEnvelope(r, docs)
		var want pipelineResult
		oracle.MustCall(t, "s9Charter", map[string]any{"payload": payload, "nowNs": now.UnixNano()}, &want)
		ta := newTestApp(t, constBody(payload)).routes()
		ta.now = now
		got := ta.get("/participation_charter")
		if want.URI == nil || *want.URI != ta.strapi.lastURI() {
			t.Fatalf("uri: jvm %v, go %q", derefStr(want.URI), ta.strapi.lastURI())
		}
		if want.JSON == nil {
			if got.status != 500 {
				t.Fatalf("%s: jvm exception %s, go %d %s", payload, want.Exception, got.status, got.body)
			}
			continue
		}
		if got.status != 200 || got.body != *want.JSON {
			t.Fatalf("%s\n jvm %s\n go  %d %s", payload, *want.JSON, got.status, got.body)
		}
	}
}

// ---------------------------------------------------------------------------
// app feedback

func TestOracleFeedbackType(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	cases := []string{"bug", "BUG", "Bug", "feature", "FEATURE", "comment", "COMMENT", "", " ", " bug", "bug ", "feature_request", "bugs", "\u0130", "BUG\u0000", "ＢＵＧ",
		"B\u0130G", "\u212a", "FEATURE\u0130", "ǅ", "ß", "bu\u0307g", "comment\n", "Feature", "cOmMeNt", "\u0049\u0307"}
	for _, in := range cases {
		var want *string
		oracle.MustCall(t, "s9FeedbackType", map[string]any{"type": in}, &want)
		got, ok := feedbackToDomain(AppFeedbackJSON{Type: in}, "u")
		switch {
		case want == nil && ok:
			t.Errorf("%q: jvm null, go %q", in, got.typ)
		case want != nil && (!ok || string(got.typ) != *want):
			t.Errorf("%q: jvm %q, go %q (%v)", in, *want, got.typ, ok)
		}
	}
}

var errNullBody = errors.New("null body")

// The request body decoding (jsonjava vs Jackson + Kotlin module).
func TestOracleFeedbackDecode(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const cls = "fr.gouv.agora.infrastructure.appFeedback.AppFeedbackJson"
	dev := `{"model":"m","osVersion":"o","appVersion":"a"}`
	cases := []string{
		`{"type":"bug","description":"d"}`, `{"type":"bug","description":"d","deviceInfo":null}`, `{"type":"bug","description":"d","deviceInfo":` + dev + `}`,
		`{"type":1,"description":2.5}`, `{"type":true,"description":false}`, `{"type":null,"description":"d"}`, `{"type":"bug","description":null}`,
		`{"description":"d"}`, `{"type":"bug"}`, `{}`, `[]`, `null`, `""`, `1`, `{"type":"bug","description":"d","deviceInfo":{}}`,
		`{"type":"bug","description":"d","deviceInfo":"x"}`, `{"type":"bug","description":"d","deviceInfo":[]}`, `{"type":"bug","description":"d","deviceInfo":{"model":1,"osVersion":2,"appVersion":3}}`,
		`{"type":"bug","description":"d","deviceInfo":{"model":null,"osVersion":"o","appVersion":"a"}}`, `{"type":"bug","description":"d","extra":{"a":[1,2]}}`,
		`{"type":"a","type":"b","description":"d"}`, `{"Type":"bug","Description":"d"}`, `{"type":[],"description":"d"}`, `{"type":{},"description":"d"}`,
		`{"type":"bug","description":"d"} xyz`, `{"type":"bug","description":"d"}{"x":1}`, `{"type":"bug","description":"\u00e9\ud83d\ude00\n"}`,
		`{"type":"bug","description":12345678901234567890}`, `{"type":"bug","description":1e400}`, `{"type":"bug","description":0.1}`, `{"type":"bug","description":-0}`,
		`{"type":"bug","description":"d"`, `{'type':'bug'}`, `{"type":"bug","description":"d",}`, "", "   ", `{"type":"bug","description":"d","deviceInfo":` + dev + `,"deviceInfo":null}`,
	}
	for _, in := range cases {
		var want string
		err := oracle.Call("jsonRoundTrip", map[string]any{"className": cls, "json": in}, &want)
		if err == nil && want == "null" {
			err = errNullBody // readValue returned null: "Required request body is missing"
		}
		var body AppFeedbackJSON
		gerr := jsonjava.Unmarshal([]byte(in), &body)
		switch {
		case err != nil && gerr == nil:
			t.Errorf("%s: jvm fails (%v), go decodes %+v", in, err, body)
		case err == nil && gerr != nil:
			t.Errorf("%s: jvm decodes %s, go fails (%v)", in, want, gerr)
		case err == nil:
			if got := jsonjava.MarshalString(body); got != want {
				t.Errorf("%s:\n jvm %s\n go  %s", in, want, got)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// response DTOs

func TestOracleDTOs(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	tricky := "é😀 \u2028 \"q\" \\ </p> & ]]> 'a'\t\n"
	arg := tricky
	for _, tc := range []struct {
		class string
		value any
	}{
		{"fr.gouv.agora.infrastructure.content.json.QuestionsAuGouvernementContentJson", QuestionsAuGouvernementContentJSON{"i", tricky, &arg, nil}},
		{"fr.gouv.agora.infrastructure.content.json.QuestionsAuGouvernementContentJson", QuestionsAuGouvernementContentJSON{"i", "", nil, &arg}},
		{"fr.gouv.agora.infrastructure.content.json.ReponseAuxQagsJson", ReponseAuxQagsJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.PoserMaQuestionJson", PoserMaQuestionJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineAccueilJson", SiteVitrineAccueilJSON{"a", tricky, "c", "d", "e", "f", "g"}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineConditionGeneralesJson", SiteVitrineConditionGeneralesJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineConsultationJson", SiteVitrineConsultationJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineDeclarationAccessibiliteJson", SiteVitrineDeclarationAccessibiliteJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineMentionsLegalesJson", SiteVitrineMentionsLegalesJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrinePolitiqueConfidentialiteJson", SiteVitrinePolitiqueConfidentialiteJSON{tricky}},
		{"fr.gouv.agora.infrastructure.content.json.SiteVitrineQuestionAuGouvernementJson", SiteVitrineQuestionAuGouvernementJSON{"a", tricky, "c"}},
		{"fr.gouv.agora.infrastructure.welcomePage.NewsJson", NewsJSON{tricky, "s", "c", "r", &arg}},
		{"fr.gouv.agora.infrastructure.welcomePage.NewsJson", NewsJSON{"d", tricky, "c", "r", nil}},
		{"fr.gouv.agora.infrastructure.participationCharter.ParticipationCharterJson", ParticipationCharterJSON{tricky, "<body></body>"}},
	} {
		in := jsonjava.MarshalString(tc.value)
		var back string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": tc.class, "json": in}, &back)
		if back != in {
			t.Errorf("%s JSON:\n jvm %s\n go  %s", tc.class, back, in)
		}
		var xml string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": tc.class, "json": in}, &xml)
		if got := string(xmljava.Marshal(tc.value)); got != xml {
			t.Errorf("%s XML:\n jvm %s\n go  %s", tc.class, xml, got)
		}
	}
	// the fiche DTO and its list (root element "List")
	f := FicheInventaireJSON{ID: "i", EtapeLancementHTML: tricky, Titre: tricky, Debut: "2024-01-01 00:00:00", Fin: "f", Porteur: "p", LienSite: "l"}
	f.Thematique.Label, f.Thematique.Picto = tricky, "😀"
	one := jsonjava.MarshalString(f)
	var back string
	oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": "fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireJson", "json": one}, &back)
	if back != one {
		t.Errorf("fiche JSON:\n jvm %s\n go  %s", back, one)
	}
	oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireJson", "json": one}, &back)
	if got := string(xmljava.Marshal(f)); got != back {
		t.Errorf("fiche XML:\n jvm %s\n go  %s", back, got)
	}
	list := FicheInventaireListJSON{f, f}
	lj := jsonjava.MarshalString(list)
	// the oracle writes the ArrayList value; Spring's converter writes the declared type List<...> (checked by the
	// scenarios against the live reference: <List>)
	listRoot := func(s string) string {
		return strings.Replace(strings.Replace(s, "<ArrayList", "<List", 1), "</ArrayList>", "</List>", 1)
	}
	oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "java.util.List<fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireJson>", "json": lj}, &back)
	back = listRoot(back)
	if got := string(xmljava.Marshal(list)); got != back {
		t.Errorf("fiche list XML:\n jvm %s\n go  %s", back, got)
	}
	oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "java.util.List<fr.gouv.agora.infrastructure.ficheInventaire.FicheInventaireJson>", "json": "[]"}, &back)
	back = listRoot(back)
	if got := string(xmljava.Marshal(FicheInventaireListJSON{})); got != back {
		t.Errorf("empty fiche list XML:\n jvm %s\n go  %s", back, got)
	}
}
