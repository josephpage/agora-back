package responseqag

import (
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"agora/internal/common"
	"agora/internal/jsonjava"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

func qagTh(m map[string]any) thematique.Thematique {
	return thematique.Thematique{ID: m["id"].(string), Label: m["label"].(string), Picto: m["picto"].(string)}
}

func checkJSONAndXML(t *testing.T, name string, v any, wantJSON, wantXML string) {
	t.Helper()
	if got := jsonjava.MarshalString(v); got != wantJSON {
		t.Fatalf("%s JSON:\n go   %s\n java %s", name, got, wantJSON)
	}
	if got := string(xmljava.Marshal(v)); got != wantXML {
		t.Fatalf("%s XML:\n go   %s\n java %s", name, got, wantXML)
	}
}

// ---------------------------------------------------------------------------
// SimpleDateFormat("yyyy-MM-dd") non lenient
// ---------------------------------------------------------------------------

type jvmDate struct {
	OK    bool   `json:"ok"`
	Ms    int64  `json:"ms"`
	Error string `json:"error"`
}

func checkMinDate(t *testing.T, s string) {
	t.Helper()
	var want jvmDate
	oracle.MustCall(t, "simpleDateParse", map[string]any{"s": s}, &want)
	got, ok := parseMinDate(s)
	if ok != want.OK || ok && got != want.Ms {
		t.Fatalf("parseMinDate(%q): go (%d, %v) java (%d, %v %s)", s, got, ok, want.Ms, want.OK, want.Error)
	}
}

func TestOracleMinDate(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	for _, s := range []string{
		"", " ", "2024-01-01", "2024-1-1", "2024-01-01T10:00:00", "20240101", "2024/01/01", " 2024- 1- 1", "\t2024-\t1-\t1", "24-01-01",
		"-2024-01-01", "+2024-01-01", "2E3-01-01", "2024.5-01-01", "NaN-01-01", "٢٠٢٤-٠١-٠١", "２０２４-01-01", "0000-01-01", "0001-01-01",
		"1582-10-04", "1582-10-05", "1582-10-14", "1582-10-15", "1500-02-29", "1700-02-29", "1600-02-29", "1900-02-29", "2000-02-29", "2023-02-29",
		"292278993-12-31", "292278994-01-01", "292278994-08-17", "292278994-08-18", "292278994-12-31", "292278995-01-01", "99999999999999999999-01-01",
		"2024-13-01", "2024-00-01", "2024-01-00", "2024-01-32", "2024-04-31", "2024-02-30", "2024-1", "2024-", "2024", "abc", "2024-01-01abc",
		"2024-01-01,2024-01-02", "2020-01-01,x", "1E1-1-1", "2024-1E0-1", "2024-1-1E0", "2024-1-1E-1", "2024-1-1E", "2024-1E-1-1", "1E+1-1-1",
		"0-1-1", "00001-01-01", "2024-001-001", "2024--1-1", "2024-1--1", "2024-0x1-01", "∞-1-1", "-∞-1-1", "2024-∞-1", "4294967297-1-1", "4294967296-1-1",
		"9223372036854775807-1-1", "9223372036854775808-1-1", "-9223372036854775808-1-1", "1e400-1-1", "0.1-1-1", "2024-01-01\u0000", " 2024-01-01",
		"2024-01-01 ", "2024 -01-01", "2024- 01 -01", "2024-01- 01", "2024-１-１", "２０２４-０１-０１",
	} {
		checkMinDate(t, s)
	}
	r := rand.New(rand.NewSource(11))
	alphabet := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "-", "-", "-", " ", "\t", "E", ".", "+", "x", "٣", "N", "a", "9999999999", "00"}
	for i := 0; i < 3000; i++ {
		var b strings.Builder
		if r.Intn(2) == 0 {
			fmt.Fprintf(&b, "%d-%d-%d", r.Intn(3000)-5, r.Intn(15)-1, r.Intn(34)-1)
		}
		for k := 0; k < r.Intn(10); k++ {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
		}
		checkMinDate(t, b.String())
	}
	// every date of the years around the Gregorian cutover and the leap-year rules
	for _, y := range []int{1, 4, 100, 1500, 1581, 1582, 1583, 1600, 1700, 1900, 2000, 2024, 2100} {
		for m := 1; m <= 12; m++ {
			for d := 1; d <= 31; d++ {
				checkMinDate(t, fmt.Sprintf("%04d-%02d-%02d", y, m, d))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Kotlin use cases and mappers
// ---------------------------------------------------------------------------

func jvmQag(q qag.QagInfoWithSupportCount) map[string]any {
	return map[string]any{
		"id": q.ID, "thematiqueId": q.ThematiqueID, "title": q.Title, "description": q.Description, "date": q.Date.UnixMilli(),
		"status": q.Status.String(), "username": q.Username, "userId": q.UserID, "supportCount": q.SupportCount, "moderatedDate": nil,
	}
}

func jvmResponse(r qag.ResponseQag) map[string]any {
	m := map[string]any{"authorFunction": nil}
	set := func(author, portrait string, date time.Time, question, qagID string, fn *string) {
		m["author"], m["authorPortraitUrl"], m["responseDate"], m["feedbackQuestion"], m["qagId"] = author, portrait, date.UnixMilli(), question, qagID
		if fn != nil {
			m["authorFunction"] = *fn
		}
	}
	if v := r.Video; v != nil {
		set(v.Author, v.AuthorPortraitURL, v.ResponseDate, v.FeedbackQuestion, v.QagID, v.AuthorFunction)
		m["kind"], m["authorDescription"], m["videoUrl"], m["videoTitle"] = "video", v.AuthorDescription, v.VideoURL, v.VideoTitle
		m["videoWidth"], m["videoHeight"], m["transcription"], m["additionalInfo"] = v.VideoWidth, v.VideoHeight, v.Transcription, nil
	} else {
		t := r.Text
		set(t.Author, t.AuthorPortraitURL, t.ResponseDate, t.FeedbackQuestion, t.QagID, t.AuthorFunction)
		m["kind"], m["responseLabel"], m["responseText"] = "text", t.ResponseLabel, t.ResponseText
	}
	return m
}

func TestOracleBuildOrderResult(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(5))
	for iter := 0; iter < 300; iter++ {
		var incoming []qag.QagInfoWithSupportCount
		var jin []map[string]any
		var responses []qagWithResponse
		var jresp []map[string]any
		var low []string
		for i := 0; i < r.Intn(6); i++ {
			q := qag.QagInfoWithSupportCount{ID: "i" + strconv.Itoa(i), ThematiqueID: "t", Title: "t", Description: "d", Date: time.UnixMilli(int64(r.Intn(6)) * 86_400_000), Status: qag.StatusSelectedForResponse, Username: "u", UserID: "x"}
			incoming = append(incoming, q)
			jin = append(jin, jvmQag(q))
			if r.Intn(3) == 0 {
				low = append(low, q.ID)
			}
		}
		for i := 0; i < r.Intn(6); i++ {
			q := qag.QagInfoWithSupportCount{ID: "r" + strconv.Itoa(i), ThematiqueID: "t", Title: "t", Description: "d", Date: time.UnixMilli(0), Status: qag.StatusSelectedForResponse, Username: "u", UserID: "x"}
			resp := videoResponse(q.ID, time.UnixMilli(int64(r.Intn(6))*86_400_000), "")
			responses = append(responses, qagWithResponse{qag: q, response: resp})
			jresp = append(jresp, map[string]any{"qag": jvmQag(q), "response": jvmResponse(resp)})
			if r.Intn(3) == 0 {
				low = append(low, q.ID)
			}
		}
		var want struct {
			Incoming []struct {
				ID    string
				Order int
			} `json:"incoming"`
			Responses []struct {
				ID    string
				Order int
			} `json:"responses"`
		}
		oracle.MustCall(t, "buildOrderResult", map[string]any{"lowPriority": low, "incoming": jin, "responses": jresp}, &want)
		got := buildOrderResult(low, incoming, responses)
		var gi, wi, gr, wr []string
		for _, o := range got.incoming {
			gi = append(gi, o.qag.ID+":"+strconv.Itoa(o.order))
		}
		for _, o := range want.Incoming {
			wi = append(wi, o.ID+":"+strconv.Itoa(o.Order))
		}
		for _, o := range got.responses {
			gr = append(gr, o.qag.ID+":"+strconv.Itoa(o.order))
		}
		for _, o := range want.Responses {
			wr = append(wr, o.ID+":"+strconv.Itoa(o.Order))
		}
		if !reflect.DeepEqual(gi, wi) || !reflect.DeepEqual(gr, wr) {
			t.Fatalf("iteration %d (low %v):\n go   %v %v\n java %v %v", iter, low, gi, gr, wi, wr)
		}
	}
}

func TestOracleMappers(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(9))
	// toIncomingResponsePreview: the Mondays around every instant, including the zone's midnight edges
	base := time.Date(2024, 2, 25, 0, 0, 0, 0, time.Local)
	for i := 0; i < 400; i++ {
		d := base.Add(time.Duration(r.Int63n(int64(800 * 24 * time.Hour))))
		if i%3 == 0 {
			d = time.Date(d.Year(), d.Month(), d.Day(), 23+r.Intn(1), 59, 59, 999_000_000, time.Local)
		}
		d = d.Truncate(time.Millisecond)
		q := qag.QagInfoWithSupportCount{ID: "id", ThematiqueID: "t", Title: "t", Description: "d", Date: d, Status: qag.StatusSelectedForResponse, Username: "u", UserID: "x"}
		var want struct {
			Previous, Next string
			Order          int
		}
		oracle.MustCall(t, "incomingResponsePreview", map[string]any{"qag": jvmQag(q), "order": i, "thematique": map[string]any{"id": "t", "label": "l", "picto": "p"}}, &want)
		got := toIncomingResponsePreview(q, i, th)
		if got.DateLundiPrecedent.Format("2006-01-02") != want.Previous || got.DateLundiSuivant.Format("2006-01-02") != want.Next || got.Order != i {
			t.Fatalf("%v: go %s/%s java %s/%s", d, got.DateLundiPrecedent.Format("2006-01-02"), got.DateLundiSuivant.Format("2006-01-02"), want.Previous, want.Next)
		}
	}
	// the sanitized text of a response
	texts := []string{
		"", " ", "<p>Bonjour</p>", "<p>un</p><p>deux</p>", "a\tb\nc\r\nd\fe\u000bf", " a ", "a  b", "<a\nhref='x'>l</a>", "<<>>", "x > y < z", "<", ">", "<>", "a<b", "100% <b>gras</b> & co",
		strings.Repeat("a", 399), strings.Repeat("a", 400), strings.Repeat("a", 401), strings.Repeat("é", 450), strings.Repeat("😀", 200), strings.Repeat("😀", 201),
		strings.Repeat("😀", 199) + "é" + "😀", "<p>" + strings.Repeat("b ", 300) + "</p>", strings.Repeat("a", 398) + "😀😀", strings.Repeat("a", 399) + "😀",
		"  " + strings.Repeat("a", 400) + "   b", "​zero​ width", "tab\there", "<p>\n</p>", "ligne 1\\nligne 2",
	}
	for i, text := range texts {
		for _, video := range []bool{false, true} {
			var resp qag.ResponseQag
			if video {
				resp = videoResponse("id", time.UnixMilli(1000), text)
			} else {
				resp = textResponse("id", time.UnixMilli(1000), text)
			}
			var want struct {
				Text  string
				Utf16 []uint16
			}
			oracle.MustCall(t, "responseTextPreview", map[string]any{"response": jvmResponse(resp), "thematique": map[string]any{"id": "t", "label": "l", "picto": "p"}, "utf16": true}, &want)
			got := toResponseQagPreviewWithoutOrder(qagInfo, resp, th)
			if len(want.Utf16) > 0 && hasLoneSurrogate(want.Utf16) {
				// Go cannot hold a lone UTF-16 surrogate: a cut inside a surrogate pair gives "?" (class C, see divergences)
				continue
			}
			if *got.ResponseText != want.Text {
				t.Fatalf("text %d (video %v): go %q java %q", i, video, *got.ResponseText, want.Text)
			}
		}
	}
}

func hasLoneSurrogate(u []uint16) bool {
	for i := 0; i < len(u); i++ {
		switch {
		case u[i] >= 0xd800 && u[i] < 0xdc00:
			if i+1 >= len(u) || u[i+1] < 0xdc00 || u[i+1] > 0xdfff {
				return true
			}
			i++
		case u[i] >= 0xdc00 && u[i] <= 0xdfff:
			return true
		}
	}
	return false
}

// responsesPageCase builds a Strapi payload and the Go equivalent.
func TestOracleResponsesPage(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(13))
	now := time.Now()
	for iter := 0; iter < 200; iter++ {
		n := r.Intn(9)
		var data []string
		var list []qag.ResponseQag
		for i := 0; i < n; i++ {
			day := time.Date(2026, 9, 1+r.Intn(20), 0, 0, 0, 0, time.Local)
			id := "q" + strconv.Itoa(i)
			data = append(data, fmt.Sprintf(`{"auteur":"a","auteurPortraitUrl":"p","auteurFonction":null,"reponseDate":"%s","feedbackQuestion":"f","questionId":"%s","auteurPortrait":null,"reponseType":[{"__component":"reponse.reponsetextuelle","label":"l","text":[]}]}`, day.Format("2006-01-02"), id))
			// the mapper gives the day the current time of day: use the same instant as the Go repository
			list = append(list, textResponse(id, common.LocalDate{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}.ToDate(now), ""))
		}
		payload := `{"data":[` + strings.Join(data, ",") + `],"meta":{"pagination":{"page":1,"pageSize":100,"pageCount":1,"total":` + strconv.Itoa(n) + `}}}`
		from := []int{0, 0, 1, 2, 5, 9, 20, -1, -6, 2147483646}[r.Intn(10)]
		pageSize := []int{5, 5, 20}[r.Intn(3)]
		var minDate *int64
		args := map[string]any{"json": payload, "from": from, "pageSize": pageSize, "minDate": nil}
		if r.Intn(2) == 0 {
			md := time.Date(2026, 9, 1+r.Intn(20), 0, 0, 0, 0, time.Local).UnixMilli()
			minDate = &md
			args["minDate"] = md
		}
		var want struct {
			IDs   []string `json:"ids"`
			Count int      `json:"count"`
			Error string   `json:"error"`
		}
		oracle.MustCall(t, "responsesPage", args, &want)

		// ties between responses of the same day are decided by the current time of day of each mapping in Kotlin
		// (and by the Strapi order here): compare day by day
		repo := testRepo(0, &fakeStrapi{all: list})
		var gotIDs []string
		var gotErr string
		func() {
			defer func() {
				if v := recover(); v != nil {
					gotErr = "IndexOutOfBoundsException"
				}
			}()
			for _, resp := range repo.GetResponsesQagPage(ctx, from, pageSize, minDate) {
				gotIDs = append(gotIDs, resp.QagID())
			}
		}()
		if gotErr != want.Error {
			t.Fatalf("iteration %d (from %d, size %d, n %d): go error %q java %q", iter, from, pageSize, n, gotErr, want.Error)
		}
		if gotErr == "" {
			if !sameUnorderedByDay(t, gotIDs, want.IDs, list) {
				t.Fatalf("iteration %d (from %d, size %d, minDate %v):\n go   %v\n java %v", iter, from, pageSize, minDate, gotIDs, want.IDs)
			}
			count := 0
			for _, resp := range list {
				if minDate == nil || resp.ResponseDateMillis() >= *minDate {
					count++
				}
			}
			if minDate != nil && count != want.Count {
				t.Fatalf("iteration %d: count go %d java %d", iter, count, want.Count)
			}
		}
	}
}

// sameUnorderedByDay compares two lists of ids that may differ in the order of the responses of the same day.
func sameUnorderedByDay(t *testing.T, got, want []string, list []qag.ResponseQag) bool {
	if len(got) != len(want) {
		return false
	}
	day := map[string]int64{}
	for _, r := range list {
		day[r.QagID()] = r.ResponseDateMillis() / 86_400_000
	}
	for i := range got {
		if day[got[i]] != day[want[i]] {
			return false
		}
	}
	return true
}

func TestOraclePaginatedOffsets(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	for _, page := range []int{-2147483648, -5, -1, 0, 1, 2, 3, 4, 100, 858993459, 858993460, 858993461, 858993462, 1717986919, 1717986920, 2147483646, 2147483647} {
		for _, count := range []int{0, 1, 4, 5, 6, 10, 11, 100} {
			var want struct {
				IsNull        bool     `json:"isNull"`
				MaxPageNumber *int     `json:"maxPageNumber"`
				Calls         []string `json:"calls"`
			}
			oracle.MustCall(t, "responsePaginatedOffsets", map[string]any{"pageNumber": page, "count": count}, &want)
			resp := &fakeResponses{count: count}
			got, err := newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, page, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != want.IsNull {
				t.Fatalf("page %d count %d: go nil=%v java nil=%v", page, count, got == nil, want.IsNull)
			}
			if got != nil {
				if got.MaxPageNumber != *want.MaxPageNumber {
					t.Fatalf("page %d count %d: max %d vs %d", page, count, got.MaxPageNumber, *want.MaxPageNumber)
				}
				if len(resp.pageCalls) != 1 || want.Calls[len(want.Calls)-1] != fmt.Sprintf("page:%d:%d", resp.pageCalls[0][0], resp.pageCalls[0][1]) {
					t.Fatalf("page %d count %d: page calls go %v java %v", page, count, resp.pageCalls, want.Calls)
				}
			}
		}
	}
}

func TestOracleJSON(t *testing.T) {
	if !oracle.Available() {
		t.Skip(oracle.UnavailableReason())
	}
	r := rand.New(rand.NewSource(21))
	strs := []string{"", "Titre simple", "accents éàüç œ", "émoji 😀 ZWJ 👨‍👩‍👧", "<b>html</b> & \"quotes\" 'single'", "ligne\navec retour\ttab", "  ", "backslash \\ et \\\\n", "100% ça marche"}
	rs := func() string { return strs[r.Intn(len(strs))] }
	jth := func() map[string]any { return map[string]any{"id": rs(), "label": rs(), "picto": rs()} }
	for iter := 0; iter < 150; iter++ {
		var incoming []IncomingResponsePreview
		var jin []map[string]any
		for i := 0; i < r.Intn(4); i++ {
			prev := time.Date(2020+r.Intn(10), time.Month(1+r.Intn(12)), 1+r.Intn(28), 0, 0, 0, 0, time.Local)
			next := prev.AddDate(0, 0, 7)
			m := jth()
			p := IncomingResponsePreview{ID: rs(), Thematique: qagTh(m), Title: rs(), SupportCount: r.Intn(100), DateLundiPrecedent: prev, DateLundiSuivant: next, Order: r.Intn(10)}
			incoming = append(incoming, p)
			jin = append(jin, map[string]any{"id": p.ID, "thematique": m, "title": p.Title, "supportCount": p.SupportCount, "previous": prev.Format("2006-01-02"), "next": next.Format("2006-01-02"), "order": p.Order})
		}
		var responses []ResponseQagPreview
		var jr []map[string]any
		for i := 0; i < r.Intn(4); i++ {
			m := jth()
			p := ResponseQagPreview{QagID: rs(), Thematique: qagTh(m), Title: rs(), Author: rs(), AuthorPortraitURL: rs(), ResponseDate: time.UnixMilli(r.Int63n(100*365*86400) * 1000), Order: r.Intn(10)}
			responses = append(responses, p)
			jr = append(jr, map[string]any{"qagId": p.QagID, "thematique": m, "title": p.Title, "author": p.Author, "authorPortraitUrl": p.AuthorPortraitURL, "responseDate": p.ResponseDate.UnixMilli(), "order": p.Order})
		}
		var want struct{ JSON, XML string }
		oracle.MustCall(t, "qagResponsesJson", map[string]any{"incoming": jin, "responses": jr}, &want)
		checkJSONAndXML(t, "QagResponsesJson", ToResponsesJSON(ResponseQagPreviewList{IncomingResponses: incoming, Responses: responses}), want.JSON, want.XML)

		var list []ResponseQagPreviewWithoutOrder
		var jl []map[string]any
		for i := 0; i < r.Intn(4); i++ {
			m := jth()
			d := ResponseQagPreviewWithoutOrder{QagID: rs(), Thematique: qagTh(m), Title: rs(), Author: rs(), AuthorPortraitURL: rs(), ResponseDate: time.UnixMilli(r.Int63n(100*365*86400) * 1000), Username: rs()}
			j := map[string]any{"qagId": d.QagID, "thematique": m, "title": d.Title, "author": d.Author, "authorPortraitUrl": d.AuthorPortraitURL, "responseDate": d.ResponseDate.UnixMilli(), "username": d.Username, "authorFunction": nil, "responseText": nil}
			if r.Intn(2) == 0 {
				s := rs()
				d.AuthorFunction, j["authorFunction"] = &s, s
			}
			if r.Intn(2) == 0 {
				s := rs()
				d.ResponseText, j["responseText"] = &s, s
			}
			list = append(list, d)
			jl = append(jl, j)
		}
		max := r.Intn(30)
		oracle.MustCall(t, "responsePaginatedJson", map[string]any{"responses": jl, "maxPageNumber": max}, &want)
		checkJSONAndXML(t, "ResponseQagPaginatedJson", ToPaginatedJSON(ResponseQagPaginatedList{ResponsesQag: list, MaxPageNumber: max}), want.JSON, want.XML)
	}
}
