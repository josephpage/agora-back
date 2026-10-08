package qaglist

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

func qagTh(id, label, picto string) thematique.Thematique {
	return thematique.Thematique{ID: id, Label: label, Picto: picto}
}

// checkJSONAndXML compares the bytes written by Go (jsonjava / xmljava) with Jackson's.
func checkJSONAndXML(t *testing.T, name string, v any, wantJSON, wantXML string) {
	t.Helper()
	if got := jsonjava.MarshalString(v); got != wantJSON {
		t.Fatalf("%s JSON:\n go   %s\n java %s", name, got, wantJSON)
	}
	if got := string(xmljava.Marshal(v)); got != wantXML {
		t.Fatalf("%s XML:\n go   %s\n java %s", name, got, wantXML)
	}
}

// powBits calls Math.pow on the JVM for every base.
func jvmPow(t *testing.T, xs []float64, y float64) []string {
	var out []string
	oracle.MustCall(t, "mathPow", map[string]any{"xs": xs, "y": y}, &out)
	return out
}

func TestOracleJavaPow(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	for _, y := range []float64{1.5, 1.0, 2.0, 0.5, 2.5, 3.0, 1.2, 0.0, -1.0, 1.7} {
		var xs []float64
		limit := 20000
		if y == 1.5 {
			limit = 300000 // every whole number of hours up to ~34 years
		}
		for x := 2; x < limit; x++ {
			xs = append(xs, float64(x))
		}
		want := jvmPow(t, xs, y)
		bad := 0
		for i, x := range xs {
			got := strconv.FormatUint(math.Float64bits(javaPow(x, y)), 16)
			if got != want[i] {
				if bad < 5 {
					t.Errorf("pow(%v, %v): go %s java %s", x, y, got, want[i])
				}
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("exponent %v: %d of %d bases differ", y, bad, len(xs))
		}
	}
}

func jvmPreview(p qag.QagPreview) map[string]any {
	return map[string]any{
		"id": p.ID, "thematique": p.Thematique.ID, "title": p.Title, "supportCount": p.SupportCount, "isSupportedByUser": p.IsSupportedByUser,
		"isAuthor": p.IsAuthor, "canShare": p.CanShare, "date": p.Date.UnixMilli(),
	}
}

type jvmResult struct {
	Result *struct {
		MaxPageCount int `json:"maxPageCount"`
		Qags         []struct {
			ID                string `json:"id"`
			SupportCount      int    `json:"supportCount"`
			IsSupportedByUser bool   `json:"isSupportedByUser"`
			IsAuthor          bool   `json:"isAuthor"`
			CanShare          bool   `json:"canShare"`
		} `json:"qags"`
		Header *struct {
			HeaderID string `json:"headerId"`
		} `json:"header"`
	} `json:"result"`
	Calls []string `json:"calls"`
}

func qagJSON(q qag.QagInfoWithSupportCount) map[string]any {
	m := map[string]any{
		"id": q.ID, "thematiqueId": q.ThematiqueID, "title": q.Title, "description": q.Description, "date": q.Date.UnixMilli(),
		"status": q.Status.String(), "username": q.Username, "userId": q.UserID, "supportCount": q.SupportCount, "moderatedDate": nil,
	}
	if q.ModeratedDate != nil {
		m["moderatedDate"] = q.ModeratedDate.UnixMilli()
	}
	return m
}

var fuzzTitles = []string{"Santé mentale", "santé et prévention", "Logement et loyer", "LOYER trop élevé", "Transport ferroviaire", "Écologie", "Question neutre",
	"Hôpital", "Énergie et climat", "Emploi", "Chômage", "100% remboursé", "İstanbul santé", "ǅ titre", "straße santé", "SANTÉ"}

// TestOracleTrending runs the real QagPaginatedV2UseCase.getTrendingQag on random candidates, clusters and clocks.
func TestOracleTrending(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(42))
	now := time.Date(2026, 10, 7, 14, 30, 5, 123_000_000, time.Local)
	for iter := 0; iter < 400; iter++ {
		n := r.Intn(25)
		var list []qag.QagInfoWithSupportCount
		for i := 0; i < n; i++ {
			// hours since moderation: around the boundaries of the guards (72 h) and of the hour
			var d time.Duration
			switch r.Intn(4) {
			case 0:
				d = time.Duration(r.Intn(400)) * time.Hour
			case 1:
				d = time.Duration(70+r.Intn(5))*time.Hour + time.Duration(r.Intn(3600))*time.Second
			case 2:
				d = time.Duration(r.Intn(600)) * time.Minute
			default:
				d = time.Duration(r.Int63n(int64(7 * 24 * time.Hour)))
			}
			moderated := now.Add(-d).Truncate(time.Millisecond)
			q := qag.QagInfoWithSupportCount{
				ID: "q" + strconv.Itoa(r.Intn(15)), ThematiqueID: []string{"th1", "th2", "unknown"}[r.Intn(3)], Title: fuzzTitles[r.Intn(len(fuzzTitles))],
				Description: "d", Date: moderated.Add(-time.Duration(r.Intn(100)) * time.Hour), Status: qag.StatusModeratedAccepted, Username: "u",
				UserID: []string{"me", "other", "x"}[r.Intn(3)], SupportCount: r.Intn(60),
			}
			if r.Intn(5) > 0 {
				q.ModeratedDate = &moderated
			}
			list = append(list, q)
		}
		libre := r.Intn(2) == 0
		var clusters []TrendingCluster
		var jclusters []map[string]any
		for c := 0; c < r.Intn(4); c++ {
			cl := TrendingCluster{ID: "c" + strconv.Itoa(r.Intn(3))}
			for w := 0; w < 1+r.Intn(3); w++ {
				cl.Mots = append(cl.Mots, []string{"santé", "loyer", "énergie", "emploi", "TRANSPORT", "i", "ß", "ǆ"}[r.Intn(8)])
			}
			clusters = append(clusters, cl)
			jclusters = append(jclusters, map[string]any{"id": cl.ID, "mots": cl.Mots})
		}
		var supported []string
		for i := 0; i < r.Intn(6); i++ {
			supported = append(supported, "q"+strconv.Itoa(r.Intn(15)))
		}
		exponent := []float64{1.5, 1.5, 1.5, 2, 1, 0.5, 2.5, 3}[r.Intn(8)]

		jq := make([]map[string]any, len(list))
		for i, q := range list {
			jq[i] = qagJSON(q)
		}
		args := map[string]any{
			"trending": jq, "page": []any{}, "count": 0, "thematiques": []string{"th1", "th2"}, "header": nil, "supportedIds": supported,
			"estThemeLibre": libre, "clusters": jclusters, "nowMs": now.UnixMilli(), "userId": "me",
		}
		var want jvmResult
		if err := oracle.CallEnv(map[string]any{"TRENDING_SCORE_EXPONENT": strconv.FormatFloat(exponent, 'f', -1, 64)}, "trendingQag", args, &want); err != nil {
			t.Fatal(err)
		}

		h := newHarness()
		h.uc.exponent = exponent
		h.uc.now = func() time.Time { return now }
		h.uc.themes = fakeThemes{"th1": {ID: "th1", Label: "label-th1", Picto: "picto"}, "th2": {ID: "th2", Label: "label-th2", Picto: "picto"}}
		h.trending.list = list
		h.theme.libre = libre
		h.clusters.clusters = clusters
		h.supports.ids = supported
		got, err := h.uc.GetTrendingQag(ctx, "me")
		if err != nil {
			t.Fatal(err)
		}
		var gotIDs, wantIDs []string
		for i, q := range got.Qags {
			gotIDs = append(gotIDs, fmt.Sprintf("%s/%d/%v/%v/%v", q.ID, q.SupportCount, q.IsSupportedByUser, q.IsAuthor, q.CanShare))
			_ = i
		}
		for _, q := range want.Result.Qags {
			wantIDs = append(wantIDs, fmt.Sprintf("%s/%d/%v/%v/%v", q.ID, q.SupportCount, q.IsSupportedByUser, q.IsAuthor, q.CanShare))
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) || got.MaxPageCount != want.Result.MaxPageCount {
			t.Fatalf("iteration %d (exponent %v, libre %v, clusters %v):\n go   %v\n java %v", iter, exponent, libre, clusters, gotIDs, wantIDs)
		}
		// the weekly theme and the clusters are read in the same cases
		var wantCalls []string
		for _, c := range want.Calls {
			if c == "theme" || c == "clusters" {
				wantCalls = append(wantCalls, c)
			}
		}
		var gotCalls []string
		if h.theme.calls > 0 {
			gotCalls = append(gotCalls, "theme")
		}
		if h.clusters.calls > 0 {
			gotCalls = append(gotCalls, "clusters")
		}
		if !reflect.DeepEqual(gotCalls, wantCalls) {
			t.Fatalf("iteration %d: calls go %v java %v", iter, gotCalls, wantCalls)
		}
	}
}

// TestOraclePaginated runs the real use case for the top / latest / supporting tabs: offsets (Int overflow), nulls, header, overlay.
func TestOraclePaginated(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(7))
	now := time.Date(2026, 10, 7, 14, 30, 5, 0, time.Local)
	pages := []int{math.MinInt32, -5, -1, 0, 1, 2, 3, 4, 5, 6, 50, 107374182, 107374183, 107374184, 1073741825, 1073741824, 2147483646, 2147483647}
	for iter := 0; iter < 300; iter++ {
		count := []int{0, 1, 19, 20, 21, 39, 40, 41, 100, 145}[r.Intn(10)]
		page := pages[r.Intn(len(pages))]
		if r.Intn(3) == 0 {
			page = 1 + r.Intn(9)
		}
		filter := []string{"top", "latest", "supporting"}[r.Intn(3)]
		var thematique *string
		if r.Intn(2) == 0 {
			thematique = ptr("th1")
		}
		var list []qag.QagInfoWithSupportCount
		for i := 0; i < r.Intn(8); i++ {
			list = append(list, qag.QagInfoWithSupportCount{
				ID: "q" + strconv.Itoa(i), ThematiqueID: []string{"th1", "unknown"}[r.Intn(2)], Title: "t", Description: "d", Date: now.Add(-time.Hour),
				Status: []qag.QagStatus{qag.StatusModeratedAccepted, qag.StatusOpen, qag.StatusSelectedForResponse}[r.Intn(3)], Username: "u",
				UserID: []string{"me", "x"}[r.Intn(2)], SupportCount: r.Intn(10),
			})
		}
		var header *HeaderQag
		var jheader any
		if r.Intn(2) == 0 {
			header = &HeaderQag{HeaderID: "h", Title: "t", Message: "m"}
			jheader = map[string]any{"headerId": "h", "title": "t", "message": "m"}
		}
		supported := []string{"q1", "q3", "q4"}
		jq := make([]map[string]any, len(list))
		for i, q := range list {
			jq[i] = qagJSON(q)
		}
		args := map[string]any{
			"trending": []any{}, "page": jq, "count": count, "supportedCount": count, "thematiques": []string{"th1"}, "header": jheader, "supportedIds": supported,
			"estThemeLibre": false, "clusters": []any{}, "nowMs": now.UnixMilli(), "userId": "me", "pageNumber": page, "filter": filter,
		}
		if thematique != nil {
			args["thematiqueId"] = *thematique
		} else {
			args["thematiqueId"] = nil
		}
		var want jvmResult
		oracle.MustCall(t, "qagPaginated", args, &want)

		h := newHarness()
		h.uc.now = func() time.Time { return now }
		h.shared.count = count
		h.supports.count = count
		h.supports.ids = supported
		h.headers.headers[filter] = header
		pageKey := func() string {
			offset := int(int32(page-1) * 20)
			key := filter + ":" + strconv.Itoa(offset)
			if filter == "supporting" {
				key = "me:" + strconv.Itoa(offset)
			}
			if thematique != nil {
				key += ":th1"
			}
			return key
		}
		if page >= 1 {
			h.shared.pages[pageKey()] = list
			h.supp.list = list
		}
		var got *QagsAndMaxPageCountV2
		var err error
		switch filter {
		case "top":
			got, err = h.uc.GetPopularQagPaginated(ctx, "me", page, thematique)
		case "latest":
			got, err = h.uc.GetLatestQagPaginated(ctx, "me", page, thematique)
		default:
			got, err = h.uc.GetSupportedQagPaginated(ctx, "me", page, thematique)
		}
		if err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want.Result == nil) {
			t.Fatalf("iteration %d (%s page %d count %d): go nil=%v java nil=%v", iter, filter, page, count, got == nil, want.Result == nil)
		}
		if got == nil {
			continue
		}
		var gotIDs, wantIDs []string
		for _, q := range got.Qags {
			gotIDs = append(gotIDs, fmt.Sprintf("%s/%d/%v/%v/%v", q.ID, q.SupportCount, q.IsSupportedByUser, q.IsAuthor, q.CanShare))
		}
		for _, q := range want.Result.Qags {
			wantIDs = append(wantIDs, fmt.Sprintf("%s/%d/%v/%v/%v", q.ID, q.SupportCount, q.IsSupportedByUser, q.IsAuthor, q.CanShare))
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) || got.MaxPageCount != want.Result.MaxPageCount || (got.Header == nil) != (want.Result.Header == nil) {
			t.Fatalf("iteration %d (%s page %d count %d):\n go   %v %d %v\n java %v %d %v", iter, filter, page, count, gotIDs, got.MaxPageCount, got.Header, wantIDs, want.Result.MaxPageCount, want.Result.Header)
		}
	}
}

// TestOracleJSON compares the JSON and XML of the DTOs with the Kotlin mappers + Jackson (JSON and XML like Spring MVC).
func TestOracleJSON(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(3))
	strs := []string{"", "Titre simple", "accents éàüç œ", "émoji 😀 ZWJ 👨‍👩‍👧", "<b>html</b> & \"quotes\" 'single'", "ligne\navec retour\ttab", "  ", "backslash \\ et \\\\n", "100% ça marche", "x"}
	rs := func() string { return strs[r.Intn(len(strs))] }
	for iter := 0; iter < 200; iter++ {
		var previews []qag.QagPreview
		var jp []map[string]any
		for i := 0; i < r.Intn(5); i++ {
			p := qag.QagPreview{
				ID: rs(), Thematique: qagTh(rs(), rs(), rs()), Title: rs(), Description: rs(), Username: rs(),
				Date: time.UnixMilli(r.Int63n(130*365*86400) * 1000), SupportCount: r.Intn(1000), IsSupportedByUser: r.Intn(2) == 0,
				IsAuthor: r.Intn(2) == 0, CanShare: r.Intn(2) == 0,
			}
			previews = append(previews, p)
			m := jvmPreview(p)
			m["description"] = p.Description
			m["username"] = p.Username
			m["thematique"] = map[string]any{"id": p.Thematique.ID, "label": p.Thematique.Label, "picto": p.Thematique.Picto}
			m["date"] = p.Date.UnixMilli()
			jp = append(jp, m)
		}
		var header *HeaderQag
		var jheader any
		if r.Intn(2) == 0 {
			header = &HeaderQag{HeaderID: rs(), Title: rs(), Message: rs()}
			jheader = map[string]any{"headerId": header.HeaderID, "title": header.Title, "message": header.Message}
		}
		maxPage := r.Intn(100)
		var want struct{ JSON, XML string }
		oracle.MustCall(t, "qagPaginatedJson", map[string]any{"qags": jp, "header": jheader, "maxPageCount": maxPage}, &want)
		j := ToPaginatedJSON(QagsAndMaxPageCountV2{Qags: previews, Header: header, MaxPageCount: maxPage})
		checkJSONAndXML(t, "QagPaginatedJsonV2", j, want.JSON, want.XML)

		var wantList struct{ JSON, XML string }
		oracle.MustCall(t, "qagPreviewListJson", map[string]any{"qags": jp}, &wantList)
		checkJSONAndXML(t, "QagPreviewListJson", ToPreviewListJSON(previews), wantList.JSON, wantList.XML)
	}
}
