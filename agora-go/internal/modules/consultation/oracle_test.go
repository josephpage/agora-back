package consultation

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

var randomTexts = []string{
	"", " ", "a", "Titre simple", "Ligne 1\\nLigne 2", "avec\nretour", "guillemets \"doubles\" et 'simples'", "<b>html</b> & entités &amp; &lt;",
	"émojis 😀 👨‍👩‍👧‍👦", "accents éàüçœ ﬁ", "backslash \\ et \\\\n", "ünï\u2028code\u2029", "https://example.org/a?b=c&d=e", "100% ça marche",
	"{title} et {url}", "{url}{url}{title}", "</p>", "<p>x</p>",
}

func rt(r *rand.Rand) string { return randomTexts[r.Intn(len(randomTexts))] }

func maybe(r *rand.Rand, v any) any {
	if r.Intn(3) == 0 {
		return nil
	}
	return v
}

func randomRich(r *rand.Rand) []any {
	out := []any{}
	for i, n := 0, r.Intn(4); i < n; i++ {
		switch r.Intn(6) {
		case 0:
			out = append(out, obj{"type": "heading", "level": r.Intn(8), "children": []any{obj{"type": "text", "text": rt(r), "bold": r.Intn(2) == 0}}})
		case 1:
			out = append(out, obj{"type": "list", "format": []string{"ordered", "unordered"}[r.Intn(2)], "children": []any{obj{"type": "list-item", "children": []any{obj{"type": "text", "text": rt(r)}}}}})
		case 2:
			out = append(out, obj{"type": "quote", "children": []any{obj{"type": "text", "text": rt(r), "italic": true}}})
		case 3:
			out = append(out, obj{"type": "paragraph", "children": []any{obj{"type": "link", "url": rt(r), "children": []any{obj{"type": "text", "text": rt(r)}}}}})
		default:
			out = append(out, obj{"type": "paragraph", "children": []any{obj{"type": "text", "text": rt(r)}}})
		}
	}
	return out
}

func randomPicture(r *rand.Rand) any {
	switch r.Intn(4) {
	case 0:
		return nil
	case 1:
		return obj{"url": "https://img/" + rt(r) + ".jpg", "formats": nil}
	case 2:
		return obj{"url": "https://img/o.jpg", "formats": obj{"medium": nil}}
	}
	return obj{"url": "https://img/o.jpg", "formats": obj{"medium": obj{"url": "https://img/m-" + rt(r) + ".jpg"}}}
}

func randomSections(r *rand.Rand) []any {
	out := []any{}
	for i, n := 0, r.Intn(12); i < n; i++ {
		id := i + 1
		switch r.Intn(7) {
		case 0:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-titre", "titre": rt(r)})
		case 1:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-texte-riche", "description": randomRich(r)})
		case 2:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-citation", "description": randomRich(r)})
		case 3:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-image", "url": rt(r), "description_accessible_de_l_image": rt(r), "image": randomPicture(r)})
		case 4:
			var video any
			if r.Intn(2) == 0 {
				video = obj{"url": "https://v/" + rt(r)}
			}
			out = append(out, obj{"id": id, "__component": "consultation-section.section-video", "url": rt(r), "largeur": r.Intn(2000), "hauteur": r.Intn(2000),
				"nom_auteur": rt(r), "poste_auteur": rt(r), "date_tournage": fmt.Sprintf("20%02d-%02d-%02d", r.Intn(30), 1+r.Intn(12), 1+r.Intn(28)), "transcription": rt(r), "video": video})
		case 5:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-chiffre", "titre": rt(r), "description": randomRich(r)})
		default:
			out = append(out, obj{"id": id, "__component": "consultation-section.section-accordeon", "titre": rt(r), "description": randomRich(r)})
		}
	}
	return out
}

func randomQuestions(r *rand.Rand) []any {
	out := []any{}
	n := r.Intn(8)
	for i := 1; i <= n; i++ {
		var next any
		switch r.Intn(4) {
		case 0:
			next = r.Intn(10)
		case 1:
			next = 999
		}
		q := obj{"id": 100 + i, "titre": rt(r), "numero": i, "question_suivante": next}
		if r.Intn(3) == 0 {
			q["popup_explication"] = randomRich(r)
		} else {
			q["popup_explication"] = nil
		}
		choix := func() []any {
			c := []any{}
			for k, m := 0, r.Intn(4); k < m; k++ {
				c = append(c, obj{"id": 1000*i + k, "label": rt(r), "ouvert": r.Intn(2) == 0})
			}
			return c
		}
		switch r.Intn(5) {
		case 0:
			q["__component"], q["choix"] = "question-de-consultation.question-a-choix-unique", choix()
		case 1:
			q["__component"], q["choix"], q["nombre_maximum_de_choix"] = "question-de-consultation.question-a-choix-multiples", choix(), r.Intn(4)
		case 2:
			q["__component"] = "question-de-consultation.question-ouverte"
		case 3:
			q["__component"], q["description"], q["url_image"], q["transcription_image"], q["image"] =
				"question-de-consultation.description", randomRich(r), maybe(r, rt(r)), maybe(r, rt(r)), randomPicture(r)
		default:
			c := []any{}
			for k, m := 0, r.Intn(3); k < m; k++ {
				c = append(c, obj{"id": 1000*i + k, "label": rt(r), "ouvert": r.Intn(2) == 0, "numero_de_la_question_suivante": 1 + r.Intn(n)})
			}
			q["__component"], q["choix"] = "question-de-consultation.question-conditionnelle", c
		}
		out = append(out, q)
	}
	return out
}

func randomWhen(r *rand.Rand) time.Time {
	switch r.Intn(6) {
	case 0:
		return baseTime
	case 1:
		return baseTime.Add(time.Duration(r.Intn(3)-1) * time.Second)
	}
	return baseTime.Add(time.Duration(r.Intn(60*24*60)-30*24*60) * time.Minute)
}

func randomDateTimeString(r *rand.Rand, t time.Time) any {
	switch r.Intn(6) {
	case 0:
		return t.Format("2006-01-02T15:04:05")
	case 1:
		return t.Format("2006-01-02T15:04")
	case 2:
		return t.Format("2006-01-02T15:04:05.000000") + "Z"
	case 3:
		return []any{t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second()}
	}
	return tdt(t)
}

func randomConsultationJSON(r *rand.Rand, n int) obj {
	spec := consultationSpec{id: fmt.Sprintf("c%d", n), slug: fmt.Sprintf("slug-%d", n), start: randomWhen(r), end: randomWhen(r)}
	for i, k := 0, r.Intn(4); i < k; i++ {
		extra := obj{"flamme_label": maybe(r, rt(r)), "recap_emoji": maybe(r, rt(r)), "recap_label": maybe(r, rt(r)), "sections": randomSections(r), "feedback_message": rt(r),
			"template_partage": rt(r), "historique_titre": rt(r), "historique_call_to_action": rt(r)}
		spec.autres = append(spec.autres, autreContenu(fmt.Sprintf("a%d-%d", n, i), randomWhen(r), extra))
	}
	if r.Intn(2) == 0 {
		spec.analyse = analyseContenu(fmt.Sprintf("an%d", n), randomWhen(r), obj{"flamme_label": maybe(r, rt(r)), "recap_emoji": maybe(r, rt(r)), "recap_label": maybe(r, rt(r)),
			"sections": randomSections(r), "pdf_analyse": maybe(r, obj{"url": rt(r)}), "feedback_message": rt(r)})
	}
	if r.Intn(2) == 0 {
		spec.commanditaire = commanditaireContenu(fmt.Sprintf("cm%d", n), randomWhen(r), obj{"flamme_label": maybe(r, rt(r)), "recap_emoji": maybe(r, rt(r)), "recap_label": maybe(r, rt(r)),
			"sections": randomSections(r), "feedback_message": rt(r)})
	}
	if r.Intn(2) == 0 {
		spec.aVenir = obj{"documentId": "v", "titre_historique": rt(r)}
	}
	spec.questions = randomQuestions(r)
	o := spec.json()
	avant := o["consultation_avant_reponse"].(obj)
	avant["sections"] = randomSections(r)
	avant["presentation"], avant["commanditaire"], avant["objectif"], avant["axe_gouvernemental"] = randomRich(r), randomRich(r), randomRich(r), randomRich(r)
	avant["template_partage"] = rt(r)
	apres := o["consultation_apres_reponse_ou_terminee"].(obj)
	apres["sections"] = randomSections(r)
	o["image_de_couverture"], o["image_page_de_contenu"] = randomPicture(r), randomPicture(r)
	o["territoire"], o["titre_consultation"], o["slug"] = rt(r), rt(r), rt(r)
	// dates in every accepted form (the end / start dates are re-written in a different form)
	o["datetime_de_debut"], o["datetime_de_fin"] = randomDateTimeString(r, spec.start), randomDateTimeString(r, spec.end)
	if r.Intn(10) == 0 {
		o["datetime_de_fin"] = "not a date"
	}
	return o
}

type oracleView struct {
	Kind    string `json:"kind"`
	Variant int    `json:"variant"`
	JSON    string `json:"json"`
	XML     string `json:"xml"`
}

type oracleConsultation struct {
	DecodeError   bool         `json:"decodeError"`
	Views         []oracleView `json:"views"`
	QuestionsJSON string       `json:"questionsJson"`
	QuestionsXML  string       `json:"questionsXml"`
	PreviewJSON   string       `json:"previewJson"`
	PreviewXML    string       `json:"previewXml"`
}

// goConsultation is everything the Go code derives from a consultation payload, like the Kotlin oracle function.
func goConsultation(c *strapiConsultation, now time.Time) (res oracleConsultation, err any) {
	defer func() { err = recover() }()
	nowL := FromTime(now)
	m := infoMapper{log: nil}
	info := m.toConsultationInfo(c)
	history := historyOf(c, func() time.Time { return now })
	um := updateMapper{}
	var kinds []string
	var updates []*UpdateInfo
	add := func(kind string, u *UpdateInfo) { kinds, updates = append(kinds, kind), append(updates, u) }
	add("avant", um.toDomainUnanswered(c))
	add("apres", um.toDomainAnsweredOrEnded(c, nowL))
	add("analyse", um.toDomainAnalyseDesReponses(c))
	add("commanditaire", um.toDomainReponseDuCommanditaire(c))
	for i, a := range c.ContenuAutres {
		add(fmt.Sprintf("autre-%d", i), um.toDomainContenuAutre(c, a))
	}
	for k, u := range updates {
		if u == nil {
			continue
		}
		for variant := 0; variant < 3; variant++ {
			var stats *FeedbackStats
			var fb *bool
			if variant > 0 {
				stats = &FeedbackStats{PositiveRatio: 60, NegativeRatio: 40, ResponseCount: 5}
				b := variant == 1
				fb = &b
			}
			body := ToDetailsJSON(&DetailsWithInfo{Consultation: info, Update: u, FeedbackStats: stats, History: history, ParticipantCount: 17,
				IsUserFeedbackPositive: fb, IsAnsweredByUser: variant == 2}, "https://www.agora.gouv.fr")
			v := oracleView{Kind: kinds[k], Variant: variant, JSON: string(jsonjava.Marshal(body))}
			if u.Goals != nil {
				// xmljava writes <goals/> for a null List? (Jackson omits it): see S4-known-xml-null-list
				v.XML = string(xmljava.Marshal(body))
			}
			res.Views = append(res.Views, v)
		}
	}
	q := ToQuestionsJSON(Questions{QuestionCount: c.NombreDeQuestion, Questions: questionsOf(c)})
	res.QuestionsJSON, res.QuestionsXML = string(jsonjava.Marshal(q)), string(xmljava.Marshal(q))
	page := ConsultationPreviewPage{Ongoing: m.toConsultationPreview([]*strapiConsultation{c})}
	page.Finished = m.toDomainFinished([]*strapiConsultation{c}, nowL)
	page.Answered = page.Finished
	p := ToPreviewJSON(page, nowL)
	res.PreviewJSON, res.PreviewXML = string(jsonjava.Marshal(p)), string(xmljava.Marshal(p))
	return res, nil
}

func TestOracleConsultationMappers(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE not set")
	}
	r := rand.New(rand.NewSource(4))
	counts := map[string]int{}
	for n := 0; n < 400; n++ {
		o := randomConsultationJSON(r, n)
		b, _ := json.Marshal(o)
		var want oracleConsultation
		wantErr := oracle.Call("s4Consultation", map[string]any{"json": string(b), "nowMs": baseTime.UnixMilli()}, &want)

		var c strapiConsultation
		decErr := jsonjava.Unmarshal(b, &c)
		if want.DecodeError || (wantErr == nil) == false && strings.Contains(fmt.Sprint(wantErr), "decode") {
			if decErr == nil {
				t.Fatalf("#%d: Kotlin cannot read the payload, Go can: %s", n, b)
			}
			counts["decode error"]++
			continue
		}
		if decErr != nil {
			t.Fatalf("#%d: Go cannot read the payload: %v\n%s", n, decErr, b)
		}
		got, panicked := goConsultation(&c, baseTime)
		if wantErr != nil {
			if panicked == nil {
				t.Fatalf("#%d: Kotlin fails (%v), Go does not:\n%s", n, wantErr, b)
			}
			counts["kotlin exception"]++
			continue
		}
		if panicked != nil {
			t.Fatalf("#%d: Go panics (%v), Kotlin does not:\n%s", n, panicked, b)
		}
		counts["ok"]++
		if len(got.Views) != len(want.Views) {
			t.Fatalf("#%d: %d views, want %d", n, len(got.Views), len(want.Views))
		}
		for i := range want.Views {
			g, w := got.Views[i], want.Views[i]
			if g.Kind != w.Kind || g.Variant != w.Variant || g.JSON != w.JSON {
				t.Fatalf("#%d view %s/%d:\n go: %s\nref: %s\npayload: %s", n, w.Kind, w.Variant, g.JSON, w.JSON, b)
			}
			if g.XML != "" && g.XML != w.XML {
				t.Fatalf("#%d view %s/%d XML:\n go: %s\nref: %s", n, w.Kind, w.Variant, g.XML, w.XML)
			}
		}
		if got.QuestionsJSON != want.QuestionsJSON || got.QuestionsXML != want.QuestionsXML {
			t.Fatalf("#%d questions:\n go: %s\nref: %s\npayload: %s", n, got.QuestionsJSON, want.QuestionsJSON, b)
		}
		if got.PreviewJSON != want.PreviewJSON || got.PreviewXML != want.PreviewXML {
			t.Fatalf("#%d preview:\n go: %s\nref: %s", n, got.PreviewJSON, want.PreviewJSON)
		}
	}
	t.Logf("%v", counts)
	if counts["ok"] < 100 {
		t.Fatalf("too few comparable payloads: %v", counts)
	}
}

func TestOracleLocalDateTime(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE not set")
	}
	r := rand.New(rand.NewSource(5))
	digits := func(n int) string {
		s := ""
		for i := 0; i < n; i++ {
			s += string(rune('0' + r.Intn(10)))
		}
		return s
	}
	cases := []string{`""`, `"  "`, `null`, `1`, `1.5`, `true`, `"2026-10-01"`, `"2026-10-01T10:00:00+02:00"`, `"2026-10-01T24:00:00"`, `"2026-02-30T10:00:00"`, `[2026,10,1,10,0]`,
		`[2026,10,1,10,0,5]`, `[2026,10,1,10,0,5,7]`, `[2026,10,1]`, `[2026,10,1,10]`, `[2026,10,1,10,0,5,7,8]`, `[2026.7,10,1,10,0]`, `["2026",10,1,10,0]`, `[]`, `{}`,
		`"2026-10-01t10:00:00"`, `"2026-10-01T10:00:00."`, `"2026-10-01T10:00:00Z"`, `"2026-10-01T10:00:00ZZ"`, `" 2026-10-01T10:00:00 "`, `"+12026-10-01T10:00:00"`,
		`"-0001-10-01T10:00:00"`, `"-0000-10-01T10:00:00"`, `"+2026-10-01T10:00:00"`, `"12026-10-01T10:00:00"`, `"2026-10-01T10:00:00.1234567890"`}
	for i := 0; i < 600; i++ {
		var s string
		switch r.Intn(6) {
		case 0:
			s = fmt.Sprintf("%s-%s-%sT%s:%s:%s", digits(4), digits(2), digits(2), digits(2), digits(2), digits(2))
		case 1:
			s = fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", 1900+r.Intn(300), 1+r.Intn(13), 1+r.Intn(31), r.Intn(25), r.Intn(61))
		case 2:
			s = fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%s", 1900+r.Intn(300), 1+r.Intn(12), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), digits(r.Intn(11)))
		case 3:
			s = fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ", 1900+r.Intn(300), 1+r.Intn(12), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60))
		case 4:
			s = fmt.Sprintf("%c%s-%02d-%02dT10:00:00", "+-"[r.Intn(2)], digits(4+r.Intn(8)), 1+r.Intn(12), 1+r.Intn(28))
		default:
			s = fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", 1900+r.Intn(300), 1+r.Intn(12), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60))
		}
		b, _ := json.Marshal(s)
		cases = append(cases, string(b))
	}
	for i := 0; i < 150; i++ {
		cases = append(cases, fmt.Sprintf("[%d,%d,%d,%d,%d,%d,%d]", 1900+r.Intn(300), r.Intn(14), r.Intn(33), r.Intn(26), r.Intn(62), r.Intn(62), r.Intn(1100000000)-5))
		cases = append(cases, fmt.Sprintf("[%d,%d,%d,%d,%d]", 1900+r.Intn(300), r.Intn(14), r.Intn(33), r.Intn(26), r.Intn(62)))
	}
	type holder struct {
		D LocalDateTime `json:"d"`
	}
	ok := 0
	for _, in := range cases {
		var want string
		wantErr := oracle.Call("s4LocalDateTime", map[string]any{"json": in}, &want)
		var h holder
		err := jsonjava.Unmarshal([]byte(`{"d":`+in+`}`), &h)
		switch {
		case wantErr != nil && err == nil:
			t.Errorf("%s: Kotlin fails (%v), Go reads %s", in, wantErr, h.D.Format())
		case wantErr == nil && err != nil:
			t.Errorf("%s: Kotlin reads %s, Go fails (%v)", in, want, err)
		case wantErr == nil && h.D.Format() != want:
			t.Errorf("%s: Go %s, Kotlin %s", in, h.D.Format(), want)
		case wantErr == nil:
			ok++
		}
	}
	if ok < 100 {
		t.Fatalf("only %d accepted values", ok)
	}
}

func TestOracleFeedbackStats(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE not set")
	}
	type stats struct{ PositiveRatio, NegativeRatio, ResponseCount int }
	for p := 0; p <= 40; p++ {
		for n := 0; n <= 40; n++ {
			var want stats
			oracle.MustCall(t, "s4FeedbackStats", map[string]any{"positive": p, "negative": n}, &want)
			got := toStats([]statGroup{{1, p}, {0, n}})
			if got.PositiveRatio != want.PositiveRatio || got.NegativeRatio != want.NegativeRatio || got.ResponseCount != want.ResponseCount {
				t.Fatalf("%d/%d: %+v, want %+v", p, n, *got, want)
			}
		}
	}
}
