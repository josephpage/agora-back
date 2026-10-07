package qag

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/strapi"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

var randomStrings = []string{
	"", " ", "a", "Titre simple", "Ligne 1\\nLigne 2", "avec\nretour", "tab\tchar", "guillemets \"doubles\" et 'simples'",
	"<b>html</b> & entités &amp; &lt;", "émojis 😀 👨‍👩‍👧‍👦", "accents éàüçœ ﬁ", "backslash \\ et \\\\n", "ünï\u2028code\u2029",
	"https://example.org/a?b=c&d=e", "100% ça marche", "x", "<script>alert(1)</script>",
}

func rs(r *rand.Rand) string { return randomStrings[r.Intn(len(randomStrings))] }

func rb(r *rand.Rand) bool { return r.Intn(2) == 0 }

func rbPtr(r *rand.Rand) *bool {
	if r.Intn(3) == 0 {
		return nil
	}
	b := rb(r)
	return &b
}

func tp(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

// randomDate is a date between 1970 and 2100, in milliseconds.
func randomDateMs(r *rand.Rand) int64 {
	return r.Int63n(130*365*86400) * 1000
}

type oracleDetails struct {
	args map[string]any
	d    QagDetails
}

func randomResponse(r *rand.Rand) (map[string]any, *ResponseQag) {
	ms := randomDateMs(r)
	date := time.UnixMilli(ms)
	base := map[string]any{
		"author": rs(r), "authorPortraitUrl": rs(r), "responseDate": ms, "feedbackQuestion": rs(r), "qagId": rs(r),
	}
	var fn *string
	if r.Intn(2) == 0 {
		s := rs(r)
		fn = &s
	}
	base["authorFunction"] = nil
	if fn != nil {
		base["authorFunction"] = *fn
	}
	if rb(r) {
		base["kind"] = "video"
		base["authorDescription"] = rs(r)
		base["videoUrl"] = rs(r)
		base["videoTitle"] = rs(r)
		base["videoWidth"] = r.Intn(4000)
		base["videoHeight"] = r.Intn(4000)
		base["transcription"] = rs(r)
		v := &ResponseQagVideo{
			Author: base["author"].(string), AuthorPortraitURL: base["authorPortraitUrl"].(string), ResponseDate: date,
			FeedbackQuestion: base["feedbackQuestion"].(string), QagID: base["qagId"].(string), AuthorFunction: fn,
			AuthorDescription: base["authorDescription"].(string), VideoURL: base["videoUrl"].(string), VideoTitle: base["videoTitle"].(string),
			VideoWidth: base["videoWidth"].(int), VideoHeight: base["videoHeight"].(int), Transcription: base["transcription"].(string),
		}
		if rb(r) {
			ai := ResponseQagAdditionalInfo{AdditionalInfoTitle: rs(r), AdditionalInfoDescription: rs(r)}
			v.AdditionalInfo = &ai
			base["additionalInfo"] = map[string]any{"title": ai.AdditionalInfoTitle, "description": ai.AdditionalInfoDescription}
		}
		return base, &ResponseQag{Video: v}
	}
	base["kind"] = "text"
	base["responseLabel"] = rs(r)
	base["responseText"] = rs(r)
	return base, &ResponseQag{Text: &ResponseQagText{
		Author: base["author"].(string), AuthorPortraitURL: base["authorPortraitUrl"].(string), ResponseDate: date,
		FeedbackQuestion: base["feedbackQuestion"].(string), QagID: base["qagId"].(string), AuthorFunction: fn,
		ResponseLabel: base["responseLabel"].(string), ResponseText: base["responseText"].(string),
	}}
}

func randomDetails(r *rand.Rand) oracleDetails {
	statuses := []QagStatus{StatusOpen, StatusArchived, StatusModeratedAccepted, StatusModeratedRejected, StatusSelectedForResponse}
	st := statuses[r.Intn(len(statuses))]
	ms := randomDateMs(r)
	th := Thematique{ID: rs(r), Label: rs(r), Picto: rs(r)}
	d := QagDetails{
		ID: rs(r), Thematique: th, Title: rs(r), Description: rs(r), Date: time.UnixMilli(ms), Status: st,
		Username: rs(r), UserID: rs(r), SupportCount: r.Intn(100000),
	}
	args := map[string]any{
		"id": d.ID, "thematique": map[string]any{"id": th.ID, "label": th.Label, "picto": th.Picto}, "title": d.Title,
		"description": d.Description, "date": ms, "status": st.String(), "username": d.Username, "userId": d.UserID,
		"supportCount": d.SupportCount, "response": nil, "feedbackResults": nil,
	}
	if r.Intn(3) > 0 {
		m, resp := randomResponse(r)
		args["response"], d.Response = m, resp
	}
	if r.Intn(3) > 0 {
		f := FeedbackResults{PositiveRatio: r.Intn(101), Count: r.Intn(1000)}
		f.NegativeRatio = 100 - f.PositiveRatio
		d.FeedbackResults = &f
		args["feedbackResults"] = map[string]any{"positiveRatio": f.PositiveRatio, "negativeRatio": f.NegativeRatio, "count": f.Count}
	}
	return oracleDetails{args: args, d: d}
}

func TestOracleQagJSONMapper(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(21))
	for i := 0; i < 600; i++ {
		od := randomDetails(r)
		u := QagWithUserData{
			QagDetails: od.d, CanShare: rb(r), CanSupport: rb(r), CanDelete: rb(r), IsAuthor: rb(r), IsSupportedByUser: rb(r), IsHelpful: rbPtr(r),
		}
		var want struct {
			JSON string `json:"json"`
			XML  string `json:"xml"`
		}
		oracle.MustCall(t, "qagJson", map[string]any{
			"details": od.args, "canShare": u.CanShare, "canSupport": u.CanSupport, "canDelete": u.CanDelete,
			"isAuthor": u.IsAuthor, "isSupportedByUser": u.IsSupportedByUser, "isHelpful": tp(u.IsHelpful),
		}, &want)
		j := ToQagJSON(u)
		if got := jsonjava.MarshalString(j); got != want.JSON {
			t.Fatalf("case %d json:\n go   %s\n java %s", i, got, want.JSON)
		}
		if got := string(xmljava.Marshal(j)); got != want.XML {
			t.Fatalf("case %d xml:\n go   %s\n java %s", i, got, want.XML)
		}
	}
}

func TestOraclePublicQagJSONMapper(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(22))
	for i := 0; i < 600; i++ {
		od := randomDetails(r)
		var want struct {
			JSON string `json:"json"`
			XML  string `json:"xml"`
		}
		oracle.MustCall(t, "publicQagJson", map[string]any{"details": od.args}, &want)
		j := ToPublicQagJSON(od.d)
		if got := jsonjava.MarshalString(j); got != want.JSON {
			t.Fatalf("case %d json:\n go   %s\n java %s", i, got, want.JSON)
		}
		if got := string(xmljava.Marshal(j)); got != want.XML {
			t.Fatalf("case %d xml:\n go   %s\n java %s", i, got, want.XML)
		}
	}
}

func TestOracleSmallDTOShapes(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	text := "Vous avez déjà posé \"une\" question"
	for _, c := range []struct {
		class string
		value any
	}{
		{"fr.gouv.agora.infrastructure.qag.FeedbackResultsJson", FeedbackResultsJSON{PositiveRatio: 67, NegativeRatio: 33, Count: 3}},
		{"fr.gouv.agora.infrastructure.qag.FeedbackResultsJson", FeedbackResultsJSON{}},
		{"fr.gouv.agora.infrastructure.qag.QagInsertionResultJson", QagInsertionResultJSON{QagID: "61c9ae56-8d75-4973-941e-4494b3195c47"}},
		{"fr.gouv.agora.infrastructure.qagHome.QagAskStatusJson", QagAskStatusJSON{}},
		{"fr.gouv.agora.infrastructure.qagHome.QagAskStatusJson", QagAskStatusJSON{AskQagErrorText: &text}},
		{"fr.gouv.agora.infrastructure.qag.SupportQagJson", SupportQagJSON{SupportCount: 12, IsSupportedByUser: true}},
	} {
		js := jsonjava.MarshalString(c.value)
		var want string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": c.class, "json": js}, &want)
		if js != want {
			t.Errorf("%s json:\n go   %s\n java %s", c.class, js, want)
		}
		if _, ok := c.value.(interface{ JavaName() string }); ok {
			var wantXML string
			oracle.MustCall(t, "xmlSerialize", map[string]any{"className": c.class, "json": js}, &wantXML)
			if got := string(xmljava.Marshal(c.value)); got != wantXML {
				t.Errorf("%s xml:\n go   %s\n java %s", c.class, got, wantXML)
			}
		}
	}
}

var jsonScalars = []string{
	`null`, `"x"`, `""`, `" "`, `5`, `-0`, `1.5`, `1e3`, `true`, `false`, `[]`, `{}`, `["x"]`, `{"a":1}`, `"é"`, `"😀"`,
	`"true"`, `"false"`, `0`, `1`, `2`, `"TRUE"`, `"yes"`, `0.0`, `"1"`, `[true]`, `[null]`,
}

func randomBody(r *rand.Rand, fields []string) string {
	var parts []string
	seen := map[string]bool{}
	for i := r.Intn(len(fields) + 3); i > 0; i-- {
		name := fields[r.Intn(len(fields))]
		if r.Intn(8) == 0 {
			name = []string{"unknown", "Title", "TITLE", "ishelpful", ""}[r.Intn(5)]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, `"`+name+`":`+jsonScalars[r.Intn(len(jsonScalars))])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var rawBodies = []string{
	``, ` `, `null`, `[]`, `{}`, `"x"`, `12`, `true`, `{`, `{"isHelpful":`, `{"isHelpful":true} trailing`, `{"isHelpful":true}{"isHelpful":false}`,
	`{'isHelpful':true}`, `{isHelpful:true}`, `{"isHelpful":true,}`, `[{"isHelpful":true}]`, `{"isHelpful":NaN}`, `{"isHelpful":01}`,
	`{"title":"a","thematiqueId":"b","description":"c","author":"d"}`, `{"title":1,"thematiqueId":true,"description":2.5,"author":[]}`,
	`{"title":"a","thematiqueId":"b","description":"c"}`, `{"title":"a","thematiqueId":"b","description":"c","author":null}`,
}

func compareDecode(t *testing.T, className, body string, goDecode func(string) (string, bool)) {
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

func TestOracleRequestBodyDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(23))
	decFeedback := func(body string) (string, bool) {
		var v FeedbackQagJSON
		if err := jsonjava.Unmarshal([]byte(body), &v); err != nil {
			return err.Error(), false
		}
		return jsonjava.MarshalString(v), true
	}
	decInserting := func(body string) (string, bool) {
		var v QagInsertingJSON
		if err := jsonjava.Unmarshal([]byte(body), &v); err != nil {
			return err.Error(), false
		}
		return jsonjava.MarshalString(v), true
	}
	for _, b := range rawBodies {
		compareDecode(t, "fr.gouv.agora.infrastructure.feedbackQag.FeedbackQagJson", b, decFeedback)
		compareDecode(t, "fr.gouv.agora.infrastructure.qag.QagInsertingJson", b, decInserting)
	}
	for i := 0; i < 500; i++ {
		compareDecode(t, "fr.gouv.agora.infrastructure.feedbackQag.FeedbackQagJson", randomBody(r, []string{"isHelpful"}), decFeedback)
		compareDecode(t, "fr.gouv.agora.infrastructure.qag.QagInsertingJson", randomBody(r, []string{"title", "thematiqueId", "description", "author"}), decInserting)
	}
}

func TestOracleAskQagStatus(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(24))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)
	for i := 0; i < 1500; i++ {
		// "now" anywhere in 2024..2025, the post date within +-9 days of it, often near a Monday 10:00
		now := start.Add(time.Duration(r.Int63n(int64(2*365*24*time.Hour)/int64(time.Millisecond))) * time.Millisecond)
		extra := int64(r.Intn(1000)) * 1000 // sub-millisecond nanoseconds, in microseconds
		now = now.Add(time.Duration(extra))
		var post time.Time
		switch r.Intn(3) {
		case 0:
			post = now.Add(time.Duration(r.Int63n(int64(18*24*time.Hour)/int64(time.Millisecond))-int64(9*24*time.Hour/time.Millisecond)) * time.Millisecond)
		case 1:
			monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
			monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 10, 0, 0, 0, time.Local)
			post = monday.AddDate(0, 0, 7*(r.Intn(3)-1)).Add(time.Duration(r.Intn(5)-2) * time.Second).Add(time.Duration(r.Intn(3)-1) * time.Millisecond)
		default:
			post = time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local).Add(time.Duration(r.Intn(3)-1) * time.Millisecond)
		}
		post = post.Truncate(time.Millisecond)
		var want string
		oracle.MustCall(t, "askQagStatus", map[string]any{"nowMs": now.UnixMilli(), "nowExtraNanos": now.Nanosecond() % 1_000_000, "postMs": post.UnixMilli()}, &want)
		got := "ENABLED"
		if isDateWithinTheWeek(post, now) {
			got = "WEEKLY_LIMIT_REACHED"
		}
		if got != want {
			t.Fatalf("now %s post %s: go %s java %s", now.Format(time.RFC3339Nano), post.Format(time.RFC3339Nano), got, want)
		}
	}
}

func TestOracleFeedbackResults(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for total := 0; total <= 60; total++ {
		for helpful := 0; helpful <= total; helpful++ {
			check(t, total, helpful)
		}
	}
	r := rand.New(rand.NewSource(25))
	for i := 0; i < 300; i++ {
		total := r.Intn(5000)
		check(t, total, r.Intn(total+1))
	}
}

func check(t *testing.T, total, helpful int) {
	t.Helper()
	var want FeedbackResultsJSON
	oracle.MustCall(t, "feedbackResults", map[string]any{"total": total, "helpful": helpful}, &want)
	got := resultsFromCounts(total, helpful)
	if got.PositiveRatio != want.PositiveRatio || got.NegativeRatio != want.NegativeRatio || got.Count != want.Count {
		t.Fatalf("total %d helpful %d: go %+v java %+v", total, helpful, got, want)
	}
}

// ---------------------------------------------------------------------------
// Strapi response QaG: decoding + ResponseQagMapper
// ---------------------------------------------------------------------------

type strapiVariant struct {
	name string
	gen  func(r *rand.Rand) any
}

func pick[T any](r *rand.Rand, l []T) T { return l[r.Intn(len(l))] }

func richText(r *rand.Rand) any {
	nodes := []any{
		map[string]any{"type": "paragraph", "children": []any{map[string]any{"type": "text", "text": rs(r), "bold": rb(r), "italic": rb(r)}}},
		map[string]any{"type": "heading", "level": r.Intn(8), "children": []any{map[string]any{"type": "text", "text": rs(r)}}},
		map[string]any{"type": "list", "format": pick(r, []string{"ordered", "unordered"}), "children": []any{map[string]any{"type": "list-item", "children": []any{map[string]any{"type": "text", "text": rs(r)}}}}},
		map[string]any{"type": "quote", "children": []any{map[string]any{"type": "link", "url": rs(r), "children": []any{map[string]any{"type": "text", "text": rs(r), "code": true}}}}},
	}
	out := []any{}
	for i := r.Intn(4); i > 0; i-- {
		out = append(out, pick(r, nodes))
	}
	return out
}

func maybe(r *rand.Rand, v any) any {
	if r.Intn(4) == 0 {
		return nil
	}
	return v
}

func randomStrapiResponse(r *rand.Rand) map[string]any {
	dates := []any{"2026-10-04", "2024-02-29", "2026-02-29", "2026-10-04T10:00:00", "2026-10-04T23:59:59Z", "20261004", "", nil, 5, []any{2026, 10, 4},
		"+12026-01-02", " 2026-10-04 ", "2026-10-04T24:00:00", "1999-12-31"}
	video := func() map[string]any {
		m := map[string]any{
			"__component": "reponse.reponse-video", "auteurDescription": rs(r), "urlVideo": rs(r), "videoWidth": pick(r, []any{1920, "1280", 7.5, nil, true}),
			"videoHeight": pick(r, []any{1080, "x", 0, nil}), "transcription": rs(r), "page_title": rs(r),
			"informationAdditionnelleTitre":       pick(r, []any{nil, "", "  ", "Plus", rs(r)}),
			"informationAdditionnelleDescription": pick(r, []any{nil, []any{}, richText(r)}),
			"video":                               pick(r, []any{nil, map[string]any{"url": rs(r)}, map[string]any{}}),
		}
		if r.Intn(8) == 0 {
			delete(m, pick(r, []string{"urlVideo", "page_title", "transcription", "auteurDescription"}))
		}
		return m
	}
	text := func() map[string]any {
		m := map[string]any{"__component": "reponse.reponsetextuelle", "label": rs(r), "text": richText(r)}
		if r.Intn(10) == 0 {
			delete(m, "label")
		}
		return m
	}
	types := []any{}
	for i := r.Intn(3); i > 0; i-- {
		switch r.Intn(10) {
		case 0:
			types = append(types, map[string]any{"__component": "reponse.inconnu"})
		case 1:
			types = append(types, nil)
		default:
			if rb(r) {
				types = append(types, video())
			} else {
				types = append(types, text())
			}
		}
	}
	m := map[string]any{
		"auteur": rs(r), "auteurPortraitUrl": rs(r), "auteurFonction": maybe(r, rs(r)), "reponseDate": pick(r, dates),
		"feedbackQuestion": rs(r), "questionId": rs(r), "reponseType": types,
		"auteurPortrait": pick(r, []any{nil, map[string]any{"url": rs(r)}, map[string]any{"url": rs(r), "formats": nil},
			map[string]any{"url": rs(r), "formats": map[string]any{"medium": map[string]any{"url": rs(r)}}},
			map[string]any{"url": rs(r), "formats": map[string]any{"medium": nil}}, map[string]any{"formats": nil}}),
	}
	if r.Intn(12) == 0 {
		delete(m, pick(r, []string{"auteur", "auteurPortraitUrl", "feedbackQuestion", "questionId", "reponseDate", "reponseType"}))
	}
	return m
}

type goResponse struct {
	ResponseDay string  `json:"responseDay"`
	Author      string  `json:"author"`
	PortraitURL string  `json:"authorPortraitUrl"`
	Question    string  `json:"feedbackQuestion"`
	QagID       string  `json:"qagId"`
	Function    *string `json:"authorFunction"`
	Kind        string  `json:"kind"`
	AuthorDesc  string  `json:"authorDescription,omitempty"`
	VideoURL    string  `json:"videoUrl,omitempty"`
	VideoTitle  string  `json:"videoTitle,omitempty"`
	VideoWidth  int     `json:"videoWidth,omitempty"`
	VideoHeight int     `json:"videoHeight,omitempty"`
	Transcr     string  `json:"transcription,omitempty"`
	Additional  any     `json:"additionalInfo,omitempty"`
	Label       string  `json:"responseLabel,omitempty"`
	Text        string  `json:"responseText,omitempty"`
}

// goResponseQagMap decodes a Strapi payload and maps it like the repository: a
// payload that cannot be decoded gives an empty list, mapper exceptions panic.
func goResponseQagMap(payload string) (out []goResponse, failure string) {
	defer func() {
		if rec := recover(); rec != nil {
			failure = fmt.Sprint(rec)
		}
	}()
	var env strapi.Envelope[*strapiResponseQag]
	if err := jsonjava.Unmarshal([]byte(payload), &env); err != nil {
		env = strapi.Envelope[*strapiResponseQag]{}
	}
	now := time.Now()
	for _, r := range responseQagMapper(env.Data, now) {
		g := goResponse{}
		switch {
		case r.Video != nil:
			v := r.Video
			g = goResponse{ResponseDay: formatDate(v.ResponseDate)[:strings.IndexByte(formatDate(v.ResponseDate), ' ')], Author: v.Author, PortraitURL: v.AuthorPortraitURL,
				Question: v.FeedbackQuestion, QagID: v.QagID, Function: v.AuthorFunction, Kind: "video", AuthorDesc: v.AuthorDescription,
				VideoURL: v.VideoURL, VideoTitle: v.VideoTitle, VideoWidth: v.VideoWidth, VideoHeight: v.VideoHeight, Transcr: v.Transcription}
			if v.AdditionalInfo != nil {
				g.Additional = map[string]any{"title": v.AdditionalInfo.AdditionalInfoTitle, "description": v.AdditionalInfo.AdditionalInfoDescription}
			}
		case r.Text != nil:
			x := r.Text
			g = goResponse{ResponseDay: formatDate(x.ResponseDate)[:strings.IndexByte(formatDate(x.ResponseDate), ' ')], Author: x.Author, PortraitURL: x.AuthorPortraitURL,
				Question: x.FeedbackQuestion, QagID: x.QagID, Function: x.AuthorFunction, Kind: "text", Label: x.ResponseLabel, Text: x.ResponseText}
		}
		out = append(out, g)
	}
	return out, ""
}

func TestOracleResponseQagMapper(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(26))
	for i := 0; i < 800; i++ {
		var data []any
		for n := r.Intn(3); n > 0; n-- {
			data = append(data, randomStrapiResponse(r))
		}
		if data == nil {
			data = []any{}
		}
		payload := map[string]any{"data": data, "meta": map[string]any{"pagination": map[string]any{"page": 1, "pageSize": 25, "pageCount": 1, "total": len(data)}}}
		if r.Intn(15) == 0 {
			delete(payload, "meta")
		}
		raw, _ := json.Marshal(payload)
		var wantJSON []goResponse
		err := oracle.Call("responseQagMap", map[string]any{"json": string(raw)}, &wantJSON)
		got, failure := goResponseQagMap(string(raw))
		if err != nil {
			if _, ok := err.(*oracle.OracleError); !ok {
				t.Fatalf("oracle: %v", err)
			}
			if failure == "" {
				t.Fatalf("case %d: java threw %v, go did not\n%s", i, err, raw)
			}
			continue
		}
		if failure != "" {
			t.Fatalf("case %d: go panicked (%s), java did not\n%s", i, failure, raw)
		}
		if len(got) != len(wantJSON) {
			t.Fatalf("case %d: %d responses in go, %d in java\n%s", i, len(got), len(wantJSON), raw)
		}
		for k := range got {
			g, _ := json.Marshal(got[k])
			w, _ := json.Marshal(wantJSON[k])
			if string(g) != string(w) {
				t.Fatalf("case %d response %d:\n go   %s\n java %s\n%s", i, k, g, w, raw)
			}
		}
	}
}
