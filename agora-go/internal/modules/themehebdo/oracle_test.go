package themehebdo

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/strapi"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// oracleTheme is the JSON shape of a domain ThemeHebdo in the oracle protocol.
type oracleTheme struct {
	Titre           string   `json:"titre"`
	SousTitre       string   `json:"sousTitre"`
	Periode         string   `json:"periode"`
	Theme           string   `json:"theme"`
	AvatarURL       *string  `json:"avatarUrl"`
	Nom             *string  `json:"nom"`
	Fonction        *string  `json:"fonction"`
	ProchainsThemes []string `json:"prochainsThemes"`
	TitreCompteur   string   `json:"titreCompteur"`
	DateDebutTheme  *int64   `json:"dateDebutTheme"`
	DateFinTheme    *int64   `json:"dateFinTheme"`
	EstThemeLibre   bool     `json:"estThemeLibre"`
}

func ms(d *time.Time) *int64 {
	if d == nil {
		return nil
	}
	v := d.UnixMilli()
	return &v
}

func toOracle(t ThemeHebdo) oracleTheme {
	pt := t.ProchainsThemes
	if pt == nil {
		pt = []string{}
	}
	return oracleTheme{t.Titre, t.SousTitre, t.Periode, t.Theme, t.AvatarURL, t.Nom, t.Fonction, pt, t.TitreCompteur, ms(t.DateDebutTheme), ms(t.DateFinTheme), t.EstThemeLibre}
}

func fromOracle(o oracleTheme) ThemeHebdo {
	var debut, fin *time.Time
	if o.DateDebutTheme != nil {
		debut = dateFromMillis(*o.DateDebutTheme)
	}
	if o.DateFinTheme != nil {
		fin = dateFromMillis(*o.DateFinTheme)
	}
	return ThemeHebdo{o.Titre, o.SousTitre, o.Periode, o.Theme, o.AvatarURL, o.Nom, o.Fonction, o.ProchainsThemes, o.TitreCompteur, debut, fin, o.EstThemeLibre}
}

func sameTheme(a, b oracleTheme) bool { return reflect.DeepEqual(a, b) }

// ---- String.uppercase() over every code point ----------------------------------------

func TestOracleUppercaseAllCodePoints(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	var batch []string
	flush := func() {
		if len(batch) == 0 {
			return
		}
		var want []string
		oracle.MustCall(t, "javaUpperCase", map[string]any{"list": batch}, &want)
		for i, s := range batch {
			if got := kotlinUppercase(s); got != want[i] {
				t.Errorf("uppercase(U+%04X %q) = %q (%X), jvm %q (%X)", []rune(s)[0], s, got, []rune(got), want[i], []rune(want[i]))
			}
		}
		batch = batch[:0]
	}
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		batch = append(batch, string(r))
		if len(batch) == 4000 {
			flush()
		}
	}
	flush()
	// mixed strings (the mapping is per character, but check the whole-string path too)
	rnd := rand.New(rand.NewSource(5))
	pool := []rune("abcXYZ àéîõüçß ŉǆǅﬁﬃΐΣσς İıſµÿ 😀𐐨 -0123  ͅᾀ")
	for i := 0; i < 500; i++ {
		rs := make([]rune, rnd.Intn(12))
		for j := range rs {
			rs[j] = pool[rnd.Intn(len(pool))]
		}
		batch = append(batch, string(rs))
	}
	flush()
}

// ---- OffsetDateTime.parse + Date.from ------------------------------------------------

func mutateDate(r *rand.Rand) string {
	base := []string{
		"2026-05-19T00:00:00+02:00", "2026-10-06T10:00:00.000Z", "2026-10-06T10:00:00Z", "2026-10-06T10:00Z", "2024-02-29T12:30:45.123456789-05:30",
		"+12345-01-01T00:00:00Z", "-0044-03-15T12:00:00+01:00", "1969-12-31T23:59:59.9999Z", "2026-10-06T10:00:00+01:00:15", "2026-10-06T10:00:00.5+00:00",
	}
	s := []byte(base[r.Intn(len(base))])
	alphabet := []byte("0123456789:-+.TZtz 9")
	for n := r.Intn(4); n > 0; n-- {
		switch r.Intn(5) {
		case 0: // replace a char
			if len(s) > 0 {
				s[r.Intn(len(s))] = alphabet[r.Intn(len(alphabet))]
			}
		case 1: // delete a char
			if len(s) > 0 {
				i := r.Intn(len(s))
				s = append(s[:i], s[i+1:]...)
			}
		case 2: // insert a char
			i := r.Intn(len(s) + 1)
			s = append(s[:i], append([]byte{alphabet[r.Intn(len(alphabet))]}, s[i:]...)...)
		case 3: // truncate
			s = s[:r.Intn(len(s)+1)]
		case 4: // numeric tweak on a digit
			for k := 0; k < len(s); k++ {
				if i := r.Intn(len(s)); s[i] >= '0' && s[i] <= '9' {
					s[i] = byte('0' + r.Intn(10))
					break
				}
			}
		}
	}
	return string(s)
}

func strapiEnvelope(dateDebut, dateFin string) string {
	b, _ := json.Marshal(map[string]any{
		"data": []any{map[string]any{"theme": "t", "date_debut": dateDebut, "date_fin": dateFin}},
		"meta": map[string]any{"pagination": map[string]any{"page": 1, "pageSize": 100, "pageCount": 1, "total": 1}},
	})
	return string(b)
}

func TestOracleOffsetDateTimeParse(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(21))
	fixed := []string{
		"2026-10-06T10:00:00.000Z", "2026-05-19T00:00:00+02:00", "", "2026", "2026-05-19", "2026-05-19T00:00:00",
		"2026-05-19T00:00:00+0200", "2026-05-19T00:00:00+02", "2026-05-19T00:00:00+02:", "2026-05-19T00:00:00+02:0", "2026-05-19T00:00:00+02:00:",
		"2026-05-19T00:00:00+02:00:3", "2026-05-19T00:00:00+02:60", "2026-05-19T00:00:00+24:00", "2026-05-19T00:00:00+18:00", "2026-05-19T00:00:00-18:00",
		"2026-05-19T00:00:00+18:00:01", "2026-05-19T00:00:00+18:01", "2026-05-19T24:00:00Z", "2026-05-19T00:00:60Z", "2026-02-29T00:00:00Z", "2024-02-29T00:00:00Z",
		"2026-04-31T00:00:00Z", "0000-01-01T00:00:00Z", "-0000-01-01T00:00:00Z", "-0001-01-01T00:00:00Z", "+0001-01-01T00:00:00Z", "+10000-01-01T00:00:00Z",
		"10000-01-01T00:00:00Z", "+999999999-01-01T00:00:00Z", "+1000000000-01-01T00:00:00Z", "+292278994-08-17T07:12:55.807Z", "+292278994-08-17T07:12:55.808Z",
		"+292278994-08-17T07:12:56Z", "-292275055-05-16T16:47:04.192Z", "-292275055-05-16T16:47:04.191Z", "-292275055-05-16T16:47:04Z", "2026-05-19T00:00:00.1234567891Z",
		"2026-05-19T00:00:00.Z", "2026-05-19T00:00:00.1Z", "2026-05-19T00:00:00,1Z", "2026-05-19T00:00Z", "2026-05-19T00:00:Z", "2026-05-19T0:00:00Z",
		"2026-05-19T00:00:00z", "2026-05-19t00:00:00Z", "2026-05-19T00:00:00ZZ", "2026-05-19T00:00:00 Z", " 2026-05-19T00:00:00Z", "2026-05-19T00:00:00Z ",
		"2026-05-19T00:00:00+02:00[Europe/Paris]", "2026-W20-2T00:00:00Z", "٢٠٢٦-05-19T00:00:00Z", "2026-05-19T00:00:00+０2:00",
	}
	n := 0
	check := func(in string) {
		var res []oracleTheme
		jerr := oracle.Call("themeHebdoMap", map[string]any{"json": strapiEnvelope(in, "2026-05-19T00:00:00Z")}, &res)
		got, gerr := parseOffsetDateTimeMillis(in)
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("%q: jvm err=%v go err=%v (go %d)", in, jerr, gerr, got)
			return
		}
		if jerr == nil && (len(res) != 1 || res[0].DateDebutTheme == nil || *res[0].DateDebutTheme != got) {
			t.Errorf("%q: jvm %+v go %d", in, res, got)
		}
		n++
	}
	for _, in := range fixed {
		check(in)
	}
	for i := 0; i < 3000; i++ {
		check(mutateDate(r))
	}
	// random valid dates, every offset form
	for i := 0; i < 500; i++ {
		d := time.Date(1900+r.Intn(400), time.Month(1+r.Intn(12)), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), r.Intn(1e9), time.UTC)
		offs := []string{"Z", "+01:00", "-07:00", "+05:30", "+00:00", "+14:00", "-12:00", "+09:45:10"}
		s := d.Format("2006-01-02T15:04:05.999999999") + offs[r.Intn(len(offs))]
		check(s)
	}
	t.Logf("%d dates compared", n)
}

// ---- ThemeHebdoJsonMapper dates --------------------------------------------------------

func TestOracleThemeHebdoJSONDates(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(33))
	instants := []int64{0, -1, 1, 999, 1000, 1759745220123, 1783040400000, -2209075200000, -2209075199999, -3000000000000, -62167219200000, 253402300800000, 4102444800000, 1e14, -1e14, 1e15, -1e15, 9007199254740991}
	// around the DST transitions of Paris
	for _, y := range []int{1900, 1916, 1940, 1944, 1945, 1976, 1977, 2000, 2026, 2037, 2038, 2050, 2100} {
		for _, m := range []time.Month{3, 10} {
			base := time.Date(y, m, 25, 0, 0, 0, 0, time.UTC).UnixMilli()
			for k := int64(0); k < 8*24; k++ {
				instants = append(instants, base+k*3600*1000, base+k*3600*1000-1)
			}
		}
	}
	for i := 0; i < 1500; i++ {
		instants = append(instants, r.Int63n(6e12)-3e12)
	}
	for i := 0; i < len(instants); i += 2 {
		debut := instants[i]
		fin := instants[(i+1)%len(instants)]
		th := ThemeHebdo{Titre: "T", SousTitre: "S", Periode: "P", Theme: "th", ProchainsThemes: []string{"a"}, TitreCompteur: "C", DateDebutTheme: dateFromMillis(debut), DateFinTheme: dateFromMillis(fin)}
		var want string
		oracle.MustCall(t, "themeHebdoJson", map[string]any{"theme": toOracle(th)}, &want)
		if got := jsonjava.MarshalString(ToJSON(th)); got != want {
			t.Fatalf("debut=%d fin=%d\n go:  %s\n jvm: %s", debut, fin, got, want)
		}
	}
	// null dates: NullPointerException on both sides
	th := ThemeHebdo{Titre: "T", SousTitre: "S", Theme: "th", ProchainsThemes: []string{}, TitreCompteur: "C", DateFinTheme: dateFromMillis(0)}
	oe := oracle.MustFail(t, "themeHebdoJson", map[string]any{"theme": toOracle(th)})
	if oe.Class() != "NullPointerException" {
		t.Fatalf("jvm error %v", oe)
	}
}

// DTO bytes (JSON and XML) with null and non-null optional properties.
func TestOracleThemeHebdoDTO(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	th := func(avatar, nom *string, prochains []string, libre bool) ThemeHebdoJSON {
		return ToJSON(ThemeHebdo{Titre: "T\"<&]>", SousTitre: "é😀", Periode: "P", Theme: "th", AvatarURL: avatar, Nom: nom, Fonction: nil,
			ProchainsThemes: prochains, TitreCompteur: "C", DateDebutTheme: dateFromMillis(1783040400123), DateFinTheme: dateFromMillis(1783040400000), EstThemeLibre: libre})
	}
	for _, v := range []ThemeHebdoJSON{
		th(nil, nil, nil, false),
		th(strPtr("https://x/y.png"), strPtr("Nom"), []string{"a", "b é"}, true),
		th(strPtr(""), nil, []string{}, true),
	} {
		in := jsonjava.MarshalString(v)
		var back string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": "fr.gouv.agora.infrastructure.themeHebdo.ThemeHebdoJson", "json": in}, &back)
		if back != in {
			t.Errorf("json:\n go:  %s\n jvm: %s", in, back)
		}
		var xml string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "fr.gouv.agora.infrastructure.themeHebdo.ThemeHebdoJson", "json": in}, &xml)
		if got := string(xmljava.Marshal(v)); got != xml {
			t.Errorf("xml:\n go:  %s\n jvm: %s", got, xml)
		}
	}
}

// ---- ThemeHebdoStrapiDTO decoding + ThemeHebdoMapper -----------------------------------

func TestOracleStrapiThemeHebdoMapping(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	d1, d2 := "2026-10-05T00:00:00.000Z", "2026-10-12T00:00:00.000Z"
	items := []string{
		fmt.Sprintf(`{"theme":"A","date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","periode":null,"photo":null,"nom_ministre":null,"fonction":null,"est_theme_libre":null,"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","periode":"3-9 oct","photo":{"url":"u","formats":{"medium":{"url":"m"}}},"nom_ministre":"N","fonction":"F","est_theme_libre":true,"date_debut":%q,"date_fin":%q,"id":3,"extra":[1]}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","photo":{"url":"u","formats":{}},"est_theme_libre":"true","date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","photo":{"url":"u","formats":null,"width":1},"est_theme_libre":1,"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","photo":{"formats":null},"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","photo":{"url":null},"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","periode":12,"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","periode":true,"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","periode":["x"],"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":null,"date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"periode":"x","date_debut":%q,"date_fin":%q}`, d1, d2),
		fmt.Sprintf(`{"theme":"A","date_debut":%q}`, d1),
		fmt.Sprintf(`{"theme":"A","date_fin":%q}`, d2),
		fmt.Sprintf(`{"theme":"A","date_debut":null,"date_fin":%q}`, d2),
		fmt.Sprintf(`{"theme":"A","date_debut":5,"date_fin":%q}`, d2),
		fmt.Sprintf(`{"theme":"A","date_debut":"bad","date_fin":%q}`, d2),
		fmt.Sprintf(`{"theme":"A","date_debut":%q,"date_fin":%q}`, d2, d1),
		`null`, `[]`, `"x"`, `{}`,
	}
	meta := `"meta":{"pagination":{"page":1,"pageSize":100,"pageCount":1,"total":1}}`
	envs := []string{
		`{"data":[` + items[0] + `,` + items[2] + `,` + items[1] + `],` + meta + `}`,
		`{"data":[],` + meta + `}`,
		`{"data":null,` + meta + `}`,
		`{"data":[` + items[0] + `]}`,
		`{"data":[` + items[0] + `],"meta":{}}`,
		`{"data":[` + items[0] + `],"meta":null}`,
		`{"meta":{}}`, `[]`, `null`, `not json`, ``,
	}
	for _, it := range items {
		envs = append(envs, `{"data":[`+it+`],`+meta+`}`)
	}
	for _, env := range envs {
		var want []oracleTheme
		jerr := oracle.Call("themeHebdoMap", map[string]any{"json": env}, &want)
		// Go side: what getThemeHebdoList computes from the Strapi answer
		var decoded strapi.Envelope[*strapiThemeHebdo]
		var got []ThemeHebdo
		var gerr error
		if derr := jsonjava.Unmarshal([]byte(env), &decoded); derr == nil {
			got, gerr = toDomain(decoded.Data)
		}
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("%s\n jvm err=%v go err=%v", env, jerr, gerr)
			continue
		}
		if jerr != nil {
			continue
		}
		if len(want) != len(got) {
			t.Errorf("%s\n jvm %d items, go %d", env, len(want), len(got))
			continue
		}
		for i := range want {
			if g := toOracle(got[i]); !sameTheme(want[i], g) {
				t.Errorf("%s item %d\n jvm %+v\n go  %+v", env, i, want[i], g)
			}
		}
	}
}

// ---- GetThemeHebdoUseCase / IsThemeHebdoTransitionUseCase -----------------------------

func randomThemes(r *rand.Rand, now int64) []ThemeHebdo {
	n := r.Intn(7)
	day := int64(24 * 3600 * 1000)
	periodes := []string{"", "", "Semaine libre", "19-25 mai", "février", "Straße ﬁn", "ǆ", "août 😀"}
	themes := []string{"A", "B", "é", "Z", ""}
	out := make([]ThemeHebdo, n)
	for i := range out {
		t := ThemeHebdo{
			Titre: "T" + fmt.Sprint(i), SousTitre: "S", Periode: periodes[r.Intn(len(periodes))], Theme: themes[r.Intn(len(themes))] + fmt.Sprint(i),
			ProchainsThemes: []string{}, TitreCompteur: "C", EstThemeLibre: r.Intn(3) == 0,
		}
		if r.Intn(2) == 0 {
			t.AvatarURL, t.Nom = strPtr("http://a"), strPtr("N")
		}
		pick := func() int64 {
			switch r.Intn(4) {
			case 0:
				return now + (r.Int63n(30)-15)*day
			case 1:
				return now + (r.Int63n(12*3600*1000*2) - 12*3600*1000)
			case 2:
				return now - 6*3600*1000 + (r.Int63n(3)-1)*(r.Int63n(2)) // window edges
			}
			return now + (r.Int63n(400)-200)*day/4
		}
		if r.Intn(8) != 0 {
			t.DateDebutTheme = dateFromMillis(pick())
		}
		if r.Intn(8) != 0 {
			t.DateFinTheme = dateFromMillis(pick())
		}
		if r.Intn(3) != 0 && t.DateDebutTheme != nil { // mostly sane intervals
			t.DateFinTheme = dateFromMillis(t.DateDebutTheme.UnixMilli() + r.Int63n(10)*day + r.Int63n(day))
		}
		out[i] = t
	}
	return out
}

func TestOracleGetThemeHebdo(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(77))
	for i := 0; i < 1500; i++ {
		var now int64
		switch r.Intn(3) {
		case 0:
			now = time.Date(2026, time.Month(1+r.Intn(12)), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), 0, time.UTC).UnixMilli() + int64(r.Intn(1000))
		case 1:
			now = 1790000000000 + r.Int63n(40*24*3600*1000)
		default:
			now = r.Int63n(4e12) // 1970-2096
		}
		themes := randomThemes(r, now)
		ot := make([]oracleTheme, len(themes))
		for k := range themes {
			ot[k] = toOracle(themes[k])
		}
		var res struct {
			Theme     oracleTheme `json:"theme"`
			JSON      string      `json:"json"`
			JSONError string      `json:"jsonError"`
		}
		oracle.MustCall(t, "getThemeHebdo", map[string]any{"nowMs": now, "themes": ot}, &res)
		got := buildTheme(themes, time.UnixMilli(now).In(time.UTC))
		if g := toOracle(got); !sameTheme(res.Theme, g) {
			t.Fatalf("now=%d themes=%+v\n jvm %+v\n go  %+v", now, ot, res.Theme, g)
		}
		if res.JSONError != "" {
			t.Fatalf("jvm json error %s", res.JSONError)
		}
		if gj := jsonjava.MarshalString(ToJSON(got)); gj != res.JSON {
			t.Fatalf("now=%d\n go:  %s\n jvm: %s", now, gj, res.JSON)
		}

		var wantTr bool
		oracle.MustCall(t, "isThemeHebdoTransition", map[string]any{"nowMs": now, "themes": ot}, &wantTr)
		nowT := time.UnixMilli(now)
		trans := false
		for _, th := range themes {
			if withinWindow(now, th.DateDebutTheme) || withinWindow(now, th.DateFinTheme) {
				trans = true
			}
		}
		_ = nowT
		if trans != wantTr {
			t.Fatalf("transition: now=%d themes=%+v go=%v jvm=%v", now, ot, trans, wantTr)
		}
	}
}
