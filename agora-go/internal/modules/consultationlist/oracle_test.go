package consultationlist

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

const isoLocal = "2006-01-02T15:04:05.999999999"

func isoOf(d consultation.LocalDateTime) string { return d.T.Format(isoLocal) }

type oracleTheme struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Picto string `json:"picto"`
}

var oracleThemes = []oracleTheme{{"th1", "Santé", "S"}, {"th2", "É<d>&\"u", "\U0001F333"}}

func goThemes() *fakeThemes {
	f := &fakeThemes{}
	for _, t := range oracleThemes {
		f.list = append(f.list, consultation.Thematique{ID: t.ID, Label: t.Label, Picto: t.Picto})
	}
	return f
}

func randomDate(r *rand.Rand, around time.Time) consultation.LocalDateTime {
	d := time.Duration(r.Intn(400)-200) * 24 * time.Hour
	d += time.Duration(r.Intn(86400)) * time.Second
	if r.Intn(4) == 0 { // the edges of the 90 days window and of "now"
		d = time.Duration([]int{-90, -91, 0, 1, -89}[r.Intn(5)]) * 24 * time.Hour
		d += time.Duration(r.Intn(3)-1) * time.Second
	}
	t := around.Add(d)
	if r.Intn(5) == 0 {
		t = t.Add(time.Duration(r.Intn(1000000)) * time.Microsecond)
	}
	return consultation.LocalDateTime{T: time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)}
}

type oraclePaginated struct {
	FinishedJSON, FinishedXML string
	AnsweredJSON, AnsweredXML string
	ListJSON, ListXML         string
}

// ConsultationPaginatedJsonMapper (with ConsultationPreviewJsonMapper: step, update label, dates) against the JVM.
func TestOraclePaginatedJSON(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(55))
	labels := []any{nil, "", "Merci !", "🔥 <b>&amp;</b> \"q\" é", "x"}
	for n := 0; n < 300; n++ {
		a := newApp(0, false)
		var list []consultation.ConsultationPreviewFinished
		var in []map[string]any
		for k, cnt := 0, r.Intn(6); k < cnt; k++ {
			th := oracleThemes[r.Intn(len(oracleThemes))]
			var label *string
			if l := labels[r.Intn(len(labels))]; l != nil {
				s := l.(string)
				label = &s
			}
			c := consultation.ConsultationPreviewFinished{
				ID: fmt.Sprintf("id%d", k), Slug: "slug-é-" + fmt.Sprint(k), Title: "Titre <&> \"é\" " + fmt.Sprint(r.Intn(9)), CoverURL: "https://c/" + fmt.Sprint(k),
				Thematique: consultation.Thematique{ID: th.ID, Label: th.Label, Picto: th.Picto}, UpdateLabel: label,
				LastUpdateDate: randomDate(r, baseTime), EndDate: randomDate(r, baseTime), Territory: []string{"France", "Nord", "Français de l'étranger"}[r.Intn(3)],
			}
			list = append(list, c)
			in = append(in, map[string]any{"id": c.ID, "slug": c.Slug, "title": c.Title, "coverUrl": c.CoverURL, "thematique": th, "updateLabel": c.UpdateLabel,
				"lastUpdateDate": isoOf(c.LastUpdateDate), "endDate": isoOf(c.EndDate), "territory": c.Territory})
		}
		max := []int{0, 1, 2, 7, 21474837}[r.Intn(5)]
		var want oraclePaginated
		oracle.MustCall(t, "s5Paginated", map[string]any{"nowMs": baseTime.UnixMilli(), "list": in, "maxPageNumber": max}, &want)
		for _, tc := range []struct {
			name           string
			max            int
			wantJSON, wXML string
		}{{"finished", max, want.FinishedJSON, want.FinishedXML}, {"answered", max, want.AnsweredJSON, want.AnsweredXML}, {"list", 1, want.ListJSON, want.ListXML}} {
			body := toJSON(a, tc.max, list)
			if got := jsonjava.MarshalString(body); got != tc.wantJSON {
				t.Fatalf("#%d %s JSON:\n go:  %s\n jvm: %s", n, tc.name, got, tc.wantJSON)
			}
			if got := string(xmljava.Marshal(body)); got != tc.wXML {
				t.Fatalf("#%d %s XML:\n go:  %s\n jvm: %s", n, tc.name, got, tc.wXML)
			}
		}
	}
}

type oracleUseCase struct {
	Result *struct {
		MaxPageNumber int      `json:"maxPageNumber"`
		IDs           []string `json:"ids"`
	} `json:"result"`
	Error string   `json:"error"`
	Calls []string `json:"calls"`
}

// The use cases against the JVM: existence of the page, maxPageNumber, the calls (Int arithmetic included).
func TestOracleUseCases(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(77))
	pages := []int{1, 2, 3, 4, 5, 0, -1, 21474836, 21474837, 21474838, 107374182, 107374183, 107374184, 2147483647, -2147483648, 1073741824, 42949673, 42949674}
	counts := []int{0, 1, 19, 20, 21, 39, 40, 99, 100, 101, 121, 200, 380, 2147483647, -1, -2147483648, 1000000}
	terrs := []string{"France", "france", "Nord", "ile-de-france", "Français de l'étranger", "Inconnu", "", " Nord", "NORD", "ÉCOLOGIE", "Hauts-de-France"}
	outcomes := map[string]int{}
	for n := 0; n < 600; n++ {
		count, page := counts[r.Intn(len(counts))], pages[r.Intn(len(pages))]
		if r.Intn(3) == 0 {
			page = 1 + r.Intn(40)
		}
		kind := []string{"finished", "answered"}[r.Intn(2)]
		territory := terrs[r.Intn(len(terrs))]
		var infos []consultation.ConsultationWithUpdateInfo
		var inInfos []map[string]any
		for k, cnt := 0, r.Intn(5); k < cnt; k++ {
			th := []string{"th1", "th2", "unknown"}[r.Intn(3)]
			i := info(fmt.Sprintf("c%d", k), th)
			i.UpdateDate, i.EndDate = randomDate(r, baseTime), randomDate(r, baseTime)
			infos = append(infos, i)
			inInfos = append(inInfos, map[string]any{"id": i.ID, "slug": i.Slug, "title": i.Title, "coverUrl": i.CoverURL, "thematiqueId": th,
				"endDate": isoOf(i.EndDate), "updateDate": isoOf(i.UpdateDate), "updateLabel": i.UpdateLabel, "territory": i.Territory})
		}
		if kind == "answered" && count == 0 {
			// a user without any answer has no consultation id to ask Strapi for (the fake repository must say the same)
			infos, inInfos = nil, nil
		}
		var want oracleUseCase
		oracle.MustCall(t, "s5UseCase", map[string]any{"kind": kind, "count": count, "page": page, "territory": territory, "thematiques": oracleThemes, "infos": inInfos}, &want)

		var got *struct {
			maxPage int
			ids     []string
		}
		var gotErr string
		var goCalls []string
		a := newApp(0, false)
		themes := goThemes()
		if kind == "finished" {
			repo := &fakeFinished{count: count, list: infos}
			l, err := finishedUseCase(a, repo, themes).GetConsultationFinishedPaginatedList(context.Background(), page, territory)
			if err != nil {
				gotErr = err.Error()
			}
			if l != nil {
				got = &struct {
					maxPage int
					ids     []string
				}{l.MaxPageNumber, idsOf(l.Consultations)}
			}
			if repo.counts > 0 {
				goCalls = append(goCalls, "count")
			}
			// Kotlin order: count, thematiques, list
			if themes.calls.Load() > 0 {
				goCalls = append(goCalls, "getThematiqueList")
			}
			for _, p := range repo.pages {
				goCalls = append(goCalls, "list:"+strings.ReplaceAll(p, "/", "/"))
			}
		} else {
			repo := &fakeAnswered{count: count, list: infos}
			l, err := answeredUseCase(a, repo, themes).GetConsultationAnsweredPaginatedList(context.Background(), "userId", page)
			if err != nil {
				gotErr = err.Error()
			}
			if l != nil {
				got = &struct {
					maxPage int
					ids     []string
				}{l.MaxPageNumber, idsOf(l.Consultations)}
			}
			if repo.counts > 0 {
				goCalls = append(goCalls, "count")
			}
			for _, c := range repo.lists {
				goCalls = append(goCalls, "list:"+c)
			}
			if themes.calls.Load() > 0 {
				goCalls = append(goCalls, "getThematiqueList")
			}
		}

		// the Kotlin cache calls are not part of the comparison; Kotlin reads the (empty) answered list of a user without any
		// answer from the repository, Go does not call it
		var wantCalls []string
		for _, c := range want.Calls {
			if strings.HasPrefix(c, "cache.") || kind == "answered" && count == 0 && strings.HasPrefix(c, "list:") {
				continue
			}
			wantCalls = append(wantCalls, c)
		}
		if want.Error != "" {
			if gotErr == "" || !strings.HasSuffix(want.Error, gotErr) {
				t.Fatalf("#%d %s count %d page %d %q: Kotlin %q, Go %q", n, kind, count, page, territory, want.Error, gotErr)
			}
			outcomes["exception"]++
			continue
		}
		if gotErr != "" {
			t.Fatalf("#%d %s count %d page %d %q: Go fails %q, Kotlin does not", n, kind, count, page, territory, gotErr)
		}
		if (want.Result == nil) != (got == nil) {
			t.Fatalf("#%d %s count %d page %d %q: Kotlin page exists: %v, Go: %v", n, kind, count, page, territory, want.Result != nil, got != nil)
		}
		if got != nil {
			if got.maxPage != want.Result.MaxPageNumber || strings.Join(got.ids, ",") != strings.Join(want.Result.IDs, ",") {
				t.Fatalf("#%d %s count %d page %d: Kotlin %+v, Go %+v", n, kind, count, page, want.Result, got)
			}
			outcomes["page"]++
		} else {
			outcomes["no page"]++
		}
		// Go reads the answered count once for all the pages of a user: compare the sequence of one first request
		if strings.Join(goCalls, " ") != strings.Join(wantCalls, " ") {
			t.Fatalf("#%d %s count %d page %d %q: calls\n Kotlin %v\n Go     %v", n, kind, count, page, territory, wantCalls, goCalls)
		}
	}
	t.Logf("outcomes: %v", outcomes)
	if outcomes["page"] < 100 || outcomes["no page"] < 100 || outcomes["exception"] < 5 {
		t.Errorf("the generator does not cover the cases: %v", outcomes)
	}
}

func idsOf(l []consultation.ConsultationPreviewFinished) []string {
	out := []string{}
	for _, c := range l {
		out = append(out, c.ID)
	}
	return out
}
