package consultationresponse

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"agora/internal/domain"
	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
	"agora/internal/sanitize"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// JVM oracle tests (PARITY_ORACLE=1): the real Kotlin validators, use cases and mappers of the reference jar
// against the Go port, on random inputs.

var (
	oTitles = []string{"Titre", "Question <b>1</b> & \"deux\" 'trois'", "é à ç 😀 ü", "", "a\tb", "<>&", "100 % sûr"}
	oIDs    = []string{"1", "2", "3", "q4", "uuid-like-1111", "é", "", " 1", "11111111-1111-1111-1111-111111111111", "00000000-0000-0000-0000-000000000000"}
)

// goString turns the UTF-16 units of a Java String into the string the database would store: a lone surrogate is '?'.
func goString(units []uint16) string {
	var sb strings.Builder
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u < 0xDC00 && i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] < 0xE000:
			sb.WriteRune(utf16.DecodeRune(rune(u), rune(units[i+1])))
			i++
		case u >= 0xD800 && u < 0xE000:
			sb.WriteByte('?')
		default:
			sb.WriteRune(rune(u))
		}
	}
	return sb.String()
}

type oText struct {
	Text  string   `json:"text"`
	Utf16 []uint16 `json:"utf16"`
}

func (t oText) String() string { return goString(t.Utf16) }

// ---------------------------------------------------------------------------
// random questions
// ---------------------------------------------------------------------------

func randQuestions(r *rand.Rand) []consultation.Question {
	var out []consultation.Question
	for i, n := 0, r.Intn(6); i < n; i++ {
		id := fmt.Sprintf("q%d", r.Intn(8))
		if r.Intn(8) == 0 {
			id = oIDs[r.Intn(len(oIDs))]
		}
		var nChoices int
		if r.Intn(6) > 0 {
			nChoices = 1 + r.Intn(4)
		}
		q := consultation.Question{ID: id, Title: oTitles[r.Intn(len(oTitles))], Order: r.Intn(10), ConsultationID: "c"}
		switch r.Intn(5) {
		case 0:
			q.Kind = consultation.KindUniqueChoice
		case 1:
			q.Kind = consultation.KindMultipleChoices
			q.MaxChoices = r.Intn(5) - 1
		case 2:
			q.Kind = consultation.KindConditional
		case 3:
			q.Kind = consultation.KindOpen
			nChoices = 0
		default:
			q.Kind = consultation.KindChapter
			nChoices = 0
		}
		for k := 0; k < nChoices; k++ {
			c := consultation.ChoixPossible{ID: fmt.Sprintf("%s-c%d", id, k), Label: oTitles[r.Intn(len(oTitles))], Ordre: k, QuestionID: id, HasOpenTextField: r.Intn(3) == 0}
			if q.Kind == consultation.KindConditional {
				c.NextQuestionID = "next"
			}
			q.ChoixPossibleList = append(q.ChoixPossibleList, c)
		}
		out = append(out, q)
	}
	return out
}

func questionJSON(q consultation.Question) map[string]any {
	kinds := map[consultation.QuestionKind]string{consultation.KindUniqueChoice: "unique", consultation.KindMultipleChoices: "multiple", consultation.KindConditional: "conditional",
		consultation.KindOpen: "open", consultation.KindChapter: "chapter"}
	var cs []map[string]any
	for _, c := range q.ChoixPossibleList {
		cs = append(cs, map[string]any{"id": c.ID, "label": c.Label, "ordre": c.Ordre, "questionId": c.QuestionID, "hasOpenTextField": c.HasOpenTextField, "nextQuestionId": c.NextQuestionID})
	}
	return map[string]any{"kind": kinds[q.Kind], "id": q.ID, "title": q.Title, "popup": nil, "order": q.Order, "nextQuestionId": nil, "consultationId": q.ConsultationID,
		"choices": cs, "maxChoices": q.MaxChoices, "urlImage": nil, "description": "d", "transcriptionImage": nil}
}

func questionsJSON(qs []consultation.Question) []map[string]any {
	out := make([]map[string]any, len(qs))
	for i, q := range qs {
		out[i] = questionJSON(q)
	}
	return out
}

func responseJSON(rs []ReponseConsultationInserting) []map[string]any {
	out := make([]map[string]any, len(rs))
	for i, r := range rs {
		m := map[string]any{"questionId": r.QuestionID, "responseText": r.ResponseText}
		if r.ChoiceIDsNil {
			m["choiceIds"] = nil
		} else {
			list := make([]any, len(r.ChoiceIDs))
			for k, c := range r.ChoiceIDs {
				if c != nil {
					list[k] = *c
				}
			}
			m["choiceIds"] = list
		}
		out[i] = m
	}
	return out
}

var oTexts = []string{"", "texte", "<b>gras</b> & co", "é😀", "a < b", "&amp;&lt;", "x\ny", "😀"}

func randText(r *rand.Rand) string {
	switch r.Intn(6) {
	case 0:
		return ""
	case 1:
		return oTexts[r.Intn(len(oTexts))]
	case 2: // around the limits, in UTF-16 units
		unit := []string{"a", "é", "😀", "<b>"}[r.Intn(4)]
		n := []int{0, 1, 99, 100, 101, 133, 134, 199, 200, 201, 202, 399, 400, 401, 402}[r.Intn(15)]
		return strings.Repeat(unit, n)
	default:
		return oTexts[r.Intn(len(oTexts))] + oTexts[r.Intn(len(oTexts))]
	}
}

func randResponses(r *rand.Rand, qs []consultation.Question) []ReponseConsultationInserting {
	var out []ReponseConsultationInserting
	for i, n := 0, r.Intn(5); i < n; i++ {
		qid := oIDs[r.Intn(len(oIDs))]
		if len(qs) > 0 && r.Intn(4) > 0 {
			qid = qs[r.Intn(len(qs))].ID
		}
		resp := ReponseConsultationInserting{QuestionID: qid, ResponseText: randText(r)}
		if r.Intn(25) == 0 {
			resp.ChoiceIDsNil = true
		} else {
			for k, m := 0, r.Intn(4); k < m; k++ {
				if r.Intn(10) == 0 {
					resp.ChoiceIDs = append(resp.ChoiceIDs, nil)
				} else {
					s := fmt.Sprintf("%s-c%d", qid, r.Intn(3))
					resp.ChoiceIDs = append(resp.ChoiceIDs, &s)
				}
			}
		}
		out = append(out, resp)
	}
	return out
}

// ---------------------------------------------------------------------------
// ControlResponseConsultationUseCase
// ---------------------------------------------------------------------------

func TestOracleControl(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(61))
	valid := 0
	for n := 0; n < 1500; n++ {
		qs := randQuestions(r)
		rs := randResponses(r, qs)
		openMax := []int{400, 400, 400, 2000, 10, 0, -1}[r.Intn(7)]
		var want bool
		oracle.MustCall(t, "s6Control", map[string]any{"consultationId": "c", "questions": questionsJSON(qs), "responses": responseJSON(rs), "openMax": openMax}, &want)
		uc := newControlResponseConsultationUseCase(&fakeQuestions{list: qs}, openMax)
		if got := uc.IsResponseConsultationValid(context.Background(), "c", rs); got != want {
			t.Fatalf("#%d go=%v jvm=%v\nquestions %+v\nresponses %+v", n, got, want, qs, rs)
		}
		if want {
			valid++
		}
	}
	if valid < 100 {
		t.Fatalf("only %d valid cases: the generator is too strict", valid)
	}
}

// ---------------------------------------------------------------------------
// InsertReponseConsultationUseCase
// ---------------------------------------------------------------------------

type oInserted struct {
	QuestionID string   `json:"questionId"`
	ChoiceIDs  []any    `json:"choiceIds"`
	Text       oText    `json:"text"`
	Raw        struct{} `json:"-"`
}

type oInsert struct {
	Result         string      `json:"result"`
	Error          string      `json:"error"`
	RepositoryCall bool        `json:"repositoryCalled"`
	Inserted       []oInserted `json:"inserted"`
	Calls          []string    `json:"calls"`
}

func TestOracleInsert(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(62))
	outcomes := map[string]int{}
	nowTime := today
	for n := 0; n < 800; n++ {
		qs := randQuestions(r)
		rs := randResponses(r, qs)
		openMax := []int{400, 400, 2000, 10, 150}[r.Intn(5)]
		var infoJSON any
		var info *consultation.ConsultationInfo
		switch r.Intn(4) {
		case 0: // unknown
		case 1: // finished
			infoJSON, info = map[string]any{"id": "c", "title": "t", "slug": "s", "coverUrl": "u", "endDate": "2023-06-20T14:00:00"}, endingAt(nowTime.AddDate(0, -4, -1))
		default:
			infoJSON, info = map[string]any{"id": "c", "title": "t", "slug": "s", "coverUrl": "u", "endDate": "2024-10-19T19:00:00"}, endingAt(nowTime.AddDate(1, 0, 0))
		}
		has := r.Intn(5) == 0
		insertResult := []string{"SUCCESS", "SUCCESS", "SUCCESS", "FAILURE"}[r.Intn(4)]
		var want oInsert
		oracle.MustCall(t, "s6Insert", map[string]any{"consultationId": "c", "userId": "userId", "now": "2023-10-19T19:00:00", "info": infoJSON, "hasAnswered": has, "insertResult": insertResult,
			"questions": questionsJSON(qs), "responses": responseJSON(rs), "openMax": openMax}, &want)

		f := newInsert(nowTime, openMax, &fakeInfo{info: info})
		f.uc.sanitize = sanitize.Sanitize
		f.qs.list = qs
		f.answered.has = has
		f.inserter.result = InsertSuccess
		if insertResult == "FAILURE" {
			f.inserter.result = InsertFailure
		}
		var gotErr string
		got, err := func() (res InsertResult, err error) {
			defer func() {
				if p := recover(); p != nil {
					gotErr = "panic"
				}
			}()
			return f.uc.InsertReponseConsultation(context.Background(), "c", "userId", rs)
		}()
		if err != nil {
			t.Fatal(err)
		}
		if (want.Error != "") != (gotErr != "") {
			t.Fatalf("#%d error: go=%q jvm=%q", n, gotErr, want.Error)
		}
		if want.Error != "" {
			outcomes["error"]++
			continue
		}
		wantResult := map[string]InsertResult{"INSERT_SUCCESS": InsertSuccess, "INSERT_FAILURE": InsertFailure}[want.Result]
		mustEqual(t, fmt.Sprintf("#%d result", n), got, wantResult)
		mustEqual(t, fmt.Sprintf("#%d repository called", n), f.inserter.calls > 0, want.RepositoryCall)
		jvmAnsweredInsert := false
		for _, c := range want.Calls {
			jvmAnsweredInsert = jvmAnsweredInsert || c == "answered.insertUserAnsweredConsultation"
		}
		mustEqual(t, fmt.Sprintf("#%d user marked", n), len(f.answered.inserted) > 0, jvmAnsweredInsert)
		var jvmHas bool
		for _, c := range want.Calls {
			jvmHas = jvmHas || c == "answered.hasAnsweredConsultation"
		}
		mustEqual(t, fmt.Sprintf("#%d has answered asked", n), f.answered.hasCalls > 0, jvmHas)
		if want.RepositoryCall {
			if len(f.inserter.got) != len(want.Inserted) {
				t.Fatalf("#%d inserted %d answers, jvm %d", n, len(f.inserter.got), len(want.Inserted))
			}
			for i, w := range want.Inserted {
				g := f.inserter.got[i]
				mustEqual(t, fmt.Sprintf("#%d answer %d question", n, i), g.QuestionID, w.QuestionID)
				mustEqual(t, fmt.Sprintf("#%d answer %d text", n, i), g.ResponseText, w.Text.String())
				if w.ChoiceIDs == nil {
					mustEqual(t, fmt.Sprintf("#%d answer %d null choices", n, i), g.ChoiceIDsNil, true)
				} else {
					var gotIDs []any
					for _, c := range g.ChoiceIDs {
						if c == nil {
							gotIDs = append(gotIDs, nil)
						} else {
							gotIDs = append(gotIDs, *c)
						}
					}
					if len(gotIDs) != len(w.ChoiceIDs) || (len(gotIDs) > 0 && !reflect.DeepEqual(gotIDs, w.ChoiceIDs)) {
						t.Fatalf("#%d answer %d choices go=%v jvm=%v", n, i, gotIDs, w.ChoiceIDs)
					}
				}
			}
		}
		outcomes[want.Result+fmt.Sprint(want.RepositoryCall)]++
	}
	t.Logf("outcomes %v", outcomes)
	if outcomes["INSERT_SUCCESStrue"] < 100 || outcomes["INSERT_FAILUREfalse"] < 20 {
		t.Fatalf("poor coverage: %v", outcomes)
	}
}

// ---------------------------------------------------------------------------
// ConsultationResultsUseCase + ConsultationResultJsonMapper
// ---------------------------------------------------------------------------

type oResults struct {
	Results *struct {
		JSON string `json:"json"`
		XML  string `json:"xml"`
	} `json:"results"`
	Calls []string `json:"calls"`
}

func choiceQuestions(qs []consultation.Question) []consultation.Question {
	var out []consultation.Question
	for _, q := range qs {
		if len(q.ChoixPossibleList) > 0 {
			out = append(out, q)
		}
	}
	return out
}

func TestOracleResults(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(63))
	na, skip := "11111111-1111-1111-1111-111111111111", "00000000-0000-0000-0000-000000000000"
	known := 0
	for n := 0; n < 800; n++ {
		qs := randQuestions(r)
		participants := []int{0, 1, 2, 3, 7, 10, 100, 12345, 2147483647}[r.Intn(9)]
		var counts []ResponseConsultationCount
		var aggregated []ConsultationResultAggregated
		for _, q := range qs {
			ids := []string{na, skip, "unknown"}
			for _, c := range q.ChoixPossibleList {
				ids = append(ids, c.ID)
			}
			for _, id := range ids {
				if r.Intn(3) == 0 {
					continue
				}
				cnt := []int{0, 1, 2, 3, 5, 7, 100, 2147483647}[r.Intn(8)]
				if r.Intn(2) == 0 {
					cnt = r.Intn(participants + 2)
				}
				if r.Intn(3) == 0 {
					aggregated = append(aggregated, ConsultationResultAggregated{ConsultationID: "c", QuestionID: q.ID, ChoiceID: id, ResponseCount: cnt})
				} else {
					counts = append(counts, ResponseConsultationCount{QuestionID: q.ID, ChoiceID: id, ResponseCount: cnt})
				}
			}
		}
		if r.Intn(2) == 0 {
			aggregated = nil // the common case: nothing aggregated yet
		}
		var info *consultation.ConsultationInfo
		var infoJSON any
		if r.Intn(10) > 0 {
			info = &consultation.ConsultationInfo{ID: "c", Title: oTitles[r.Intn(len(oTitles))], CoverURL: "https://c/é?a=b&c=d"}
			infoJSON = map[string]any{"id": "c", "title": info.Title, "slug": "s", "coverUrl": info.CoverURL, "endDate": "2030-01-01T00:00:00"}
		}
		var jc, ja []map[string]any
		for _, c := range counts {
			jc = append(jc, map[string]any{"questionId": c.QuestionID, "choiceId": c.ChoiceID, "responseCount": c.ResponseCount})
		}
		for _, c := range aggregated {
			ja = append(ja, map[string]any{"questionId": c.QuestionID, "choiceId": c.ChoiceID, "responseCount": c.ResponseCount})
		}
		var want oResults
		oracle.MustCall(t, "s6Results", map[string]any{"consultationId": "c", "info": infoJSON, "questions": questionsJSON(qs), "participantCount": participants, "counts": jc, "aggregated": ja}, &want)

		src := &fakeResultsSources{participants: participants, counts: counts, aggregated: aggregated}
		uc, _, _ := newResults(newApp(false), info, qs, src)
		got, err := uc.BuildConsultationResults(context.Background(), "c")
		if err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want.Results == nil) {
			t.Fatalf("#%d nil: go=%v jvm=%v", n, got == nil, want.Results == nil)
		}
		jvmCalls := map[string]string{"answered.getParticipantCount": "participants", "aggregated.getConsultationResultAggregated": "aggregated", "responses.getConsultationResponsesCount": "counts"}
		var wantCalls []string
		for _, c := range want.Calls {
			wantCalls = append(wantCalls, jvmCalls[c])
		}
		if !reflect.DeepEqual(src.calls, wantCalls) && !(len(src.calls) == 0 && len(wantCalls) == 0) {
			t.Fatalf("#%d calls go=%v jvm=%v", n, src.calls, wantCalls)
		}
		if got == nil {
			continue
		}
		known++
		dto := ToResultsJSON(got)
		if g := jsonjava.MarshalString(dto); g != want.Results.JSON {
			t.Fatalf("#%d JSON:\n go:  %s\n jvm: %s\nquestions %+v\ncounts %+v aggregated %+v participants %d", n, g, want.Results.JSON, qs, counts, aggregated, participants)
		}
		if g := string(xmljava.Marshal(dto)); g != want.Results.XML {
			t.Fatalf("#%d XML:\n go:  %s\n jvm: %s", n, g, want.Results.XML)
		}
	}
	if known < 500 {
		t.Fatalf("only %d known consultations", known)
	}
}

// ---------------------------------------------------------------------------
// the demographic maps, the ratios and the TSV
// ---------------------------------------------------------------------------

var (
	genderNames = []string{"MASCULIN", "FEMININ", "AUTRE"}
	ageNames    = []string{"LESS_THAN_18", "BETWEEN_18_AND_25", "BETWEEN_26_AND_35", "BETWEEN_36_AND_45", "BETWEEN_46_AND_55", "BETWEEN_56_AND_65", "MORE_THAN_65"}
	cityNames   = []string{"RURAL", "URBAIN", "AUTRE"}
	jobNames    = []string{"AGRICULTEUR", "ARTISAN", "CADRE", "PROFESSION_INTERMEDIAIRE", "EMPLOYE", "OUVRIER", "ETUDIANTS", "RETRAITES", "AUTRESOUSANSACTIVITEPRO", "UNKNOWN"}
	freqNames   = []string{"SOUVENT", "PARFOIS", "JAMAIS"}
)

func dimKeyCount(dim int) int {
	switch dim {
	case dimGender, dimCityType, dimVoteFrequency, dimPublicMeetingFrequency, dimConsultationFrequency:
		return 3
	case dimAgeRange:
		return 7
	case dimJobCategory:
		return 10
	}
	return len(domain.Departments)
}

func dimKeyName(dim, key int) any {
	if key == noKey {
		return nil
	}
	switch dim {
	case dimGender:
		return genderNames[key]
	case dimAgeRange:
		return ageNames[key]
	case dimDepartment:
		return domain.Departments[key].Name
	case dimCityType:
		return cityNames[key]
	case dimJobCategory:
		return jobNames[key]
	}
	return freqNames[key]
}

func randDemographic(r *rand.Rand, maxCount int) *DemographicInfoCount {
	d := emptyDemographicInfoCount()
	for dim := 0; dim < numDims; dim++ {
		n := dimKeyCount(dim)
		keys := []int{noKey}
		for k := 0; k < n; k++ {
			keys = append(keys, k)
		}
		r.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		take := r.Intn(min(len(keys), 6) + 1)
		if dim == dimDepartment {
			take = r.Intn(5)
		}
		for _, k := range keys[:take] {
			d[dim].put(k, r.Intn(maxCount+1))
		}
	}
	return d
}

func demographicJSON(d *DemographicInfoCount) [][][]any {
	out := make([][][]any, numDims)
	for dim := range out {
		out[dim] = [][]any{}
		for _, k := range d[dim].keys {
			out[dim] = append(out[dim], []any{dimKeyName(dim, k), d[dim].vals[k]})
		}
	}
	return out
}

type fakeDemographic struct {
	counts   []ResponseConsultationCount
	global   *DemographicInfoCount
	byChoice map[string]*DemographicInfoCountByChoices
}

func (f *fakeDemographic) GetConsultationResponsesCount(context.Context, string) ([]ResponseConsultationCount, error) {
	return f.counts, nil
}

func (f *fakeDemographic) GetParticipantDemographicInfo(context.Context, string) (*DemographicInfoCount, error) {
	return f.global, nil
}

func (f *fakeDemographic) participantDemographicInfoByChoices(_ context.Context, ids []string) ([]*DemographicInfoCountByChoices, error) {
	out := make([]*DemographicInfoCountByChoices, len(ids))
	for i, id := range ids {
		out[i] = f.byChoice[id]
		if out[i] == nil {
			out[i] = &DemographicInfoCountByChoices{Choices: map[string]*DemographicInfoCount{}}
		}
	}
	return out, nil
}

func TestOracleTsv(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(64))
	na, skip := "11111111-1111-1111-1111-111111111111", "00000000-0000-0000-0000-000000000000"
	nonEmpty := 0
	for n := 0; n < 400; n++ {
		qs := randQuestions(r)
		participants := []int{0, 1, 3, 7, 10, 128, 1000}[r.Intn(7)]
		var counts []ResponseConsultationCount
		byQuestion := map[string]any{}
		fd := &fakeDemographic{byChoice: map[string]*DemographicInfoCountByChoices{}}
		fd.global = randDemographic(r, max(participants, 1)+r.Intn(3))
		for _, q := range qs {
			if len(q.ChoixPossibleList) == 0 || q.Kind == consultation.KindOpen {
				continue
			}
			ids := []string{na, skip}
			for _, c := range q.ChoixPossibleList {
				ids = append(ids, c.ID)
			}
			perChoice := map[string]any{}
			bc := &DemographicInfoCountByChoices{Choices: map[string]*DemographicInfoCount{}}
			for _, id := range ids {
				if r.Intn(3) > 0 {
					counts = append(counts, ResponseConsultationCount{QuestionID: q.ID, ChoiceID: id, ResponseCount: r.Intn(participants + 2)})
				}
				if r.Intn(4) > 0 {
					d := randDemographic(r, max(participants, 1)+r.Intn(3))
					bc.Choices[id] = d
					perChoice[id] = demographicJSON(d)
				}
			}
			byQuestion[q.ID] = perChoice
			fd.byChoice[q.ID] = bc
		}
		fd.counts = counts
		var jc []map[string]any
		for _, c := range counts {
			jc = append(jc, map[string]any{"questionId": c.QuestionID, "choiceId": c.ChoiceID, "responseCount": c.ResponseCount})
		}
		info := &consultation.ConsultationInfo{ID: "c", Title: oTitles[r.Intn(len(oTitles))]}
		infoJSON := map[string]any{"id": "c", "title": info.Title, "slug": "s", "coverUrl": "u", "endDate": "2030-01-01T00:00:00"}
		var want struct {
			TSV *oText `json:"tsv"`
		}
		oracle.MustCall(t, "s6Tsv", map[string]any{"consultationId": "c", "info": infoJSON, "questions": questionsJSON(qs), "participantCount": participants, "counts": jc,
			"global": demographicJSON(fd.global), "byChoices": byQuestion}, &want)

		uc := &GetConsultationResultsWithDemographicRatiosUseCase{a: newApp(false), info: &fakeInfo{info: info}, questions: &fakeQuestions{list: qs},
			answered: &fakeResultsSources{participants: participants}, responses: fd}
		res, err := uc.GetConsultationResults(context.Background(), "c")
		if err != nil || res == nil {
			t.Fatalf("%v %v", res, err)
		}
		got := tsvMapper{nf: en}.BuildTsvBody(res)
		if want.TSV == nil {
			t.Fatal("the JVM returned no export")
		}
		if got != want.TSV.String() {
			g, w := strings.Split(got, "\n"), strings.Split(want.TSV.String(), "\n")
			for i := 0; i < len(g) && i < len(w); i++ {
				if g[i] != w[i] {
					t.Fatalf("#%d line %d:\n go:  %q\n jvm: %q\nparticipants %d", n, i, g[i], w[i], participants)
				}
			}
			t.Fatalf("#%d length: go %d lines, jvm %d lines", n, len(g), len(w))
		}
		if len(res.Results) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty < 100 {
		t.Fatalf("only %d exports with questions", nonEmpty)
	}
}

// ReponseConsultationMapper.toDomain
func TestOracleDemographicMapper(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(65))
	pools := [numDims][]any{
		{"M", "F", "A", "X", "", nil, "m"},
		{"2005", "2008", "2007", "2001", "2000", "1991", "1990", "1980", "1971", "1970", "1961", "1960", "1900", "3000", "abc", "", nil, "+2000", "9999999999", "-5"},
		{"01", "2A", "2B", "75", "99", "971", "1", "ZZ", nil, ""},
		{"R", "U", "A", "Z", nil},
		{"AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN", "ZZ", nil},
		{"S", "P", "J", "X", nil}, {"S", "P", "J", "X", nil}, {"S", "P", "J", "X", nil},
	}
	for n := 0; n < 300; n++ {
		var global [numDims][]demographicKeyed
		var globalJSON, byJSON [][][]any
		byJSON = make([][][]any, numDims)
		globalJSON = make([][][]any, numDims)
		var by [numDims][]demographicKeyed
		for dim := 0; dim < numDims; dim++ {
			globalJSON[dim], byJSON[dim] = [][]any{}, [][]any{}
			for k, c := 0, r.Intn(6); k < c; k++ {
				key := pools[dim][r.Intn(len(pools[dim]))]
				cnt := r.Intn(50)
				d := demographicKeyed{count: cnt}
				if key != nil {
					d.key = sp(key.(string))
				}
				global[dim] = append(global[dim], d)
				globalJSON[dim] = append(globalJSON[dim], []any{key, cnt})
			}
			for k, c := 0, r.Intn(8); k < c; k++ {
				key := pools[dim][r.Intn(len(pools[dim]))]
				cnt := r.Intn(50)
				choice := []string{"a", "b", "c"}[r.Intn(3)]
				d := demographicKeyed{choiceID: choice, count: cnt}
				if key != nil {
					d.key = sp(key.(string))
				}
				by[dim] = append(by[dim], d)
				byJSON[dim] = append(byJSON[dim], []any{choice, key, cnt})
			}
		}
		var want struct {
			Global    [][][]any            `json:"global"`
			ByChoices map[string][][][]any `json:"byChoices"`
		}
		oracle.MustCall(t, "s6DemographicMapper", map[string]any{"global": globalJSON, "byChoices": byJSON}, &want)
		cur := time.Now().Year()
		g := buildDemographicInfoCount(cur, global)
		if gj := demographicJSON(g); !reflect.DeepEqual(normalizeJSON(gj), normalizeJSON(want.Global)) {
			t.Fatalf("#%d global:\n go:  %v\n jvm: %v\ninput %v", n, gj, want.Global, globalJSON)
		}
		bc := toDemographicInfoCountByChoices(cur, by)
		gotBy := map[string][][][]any{}
		for id, d := range bc.Choices {
			gotBy[id] = demographicJSON(d)
		}
		if !reflect.DeepEqual(normalizeJSON(gotBy), normalizeJSON(want.ByChoices)) && !(len(gotBy) == 0 && len(want.ByChoices) == 0) {
			t.Fatalf("#%d byChoices:\n go:  %v\n jvm: %v", n, gotBy, want.ByChoices)
		}
	}
}

// normalizeJSON turns numbers into float64 like encoding/json does, so that the two sides compare equal
func normalizeJSON(v any) any {
	b := jsonjava.Marshal(v)
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// ---------------------------------------------------------------------------
// NumberFormat
// ---------------------------------------------------------------------------

func TestOracleNumberFormat(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(66))
	locales := map[string]string{"en": "en", "fr": "fr", "de": "de", "es": "es", "it": "it", "pt": "pt", "nl": "nl", "en-US": "en-US", "fr-FR": "fr-FR", "de-DE": "de-DE"}
	fixed := []float64{0, math.Copysign(0, -1), 1, -1, 0.5, 12.5, 100, 33.333333333333336, 66.66666666666667, 0.78125, 2.34375, 0.03125, 0.09375, 1e-5, -1e-5, 5e-5, 0.00005, 0.00015,
		0.00025, 0.00035, 999.99995, 9999.99995, 1234567.89125, 1e7, 1e9, 1e15, 1e21, 1e22, 1e23, 123456789012345678, math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.NaN(), math.Inf(1), math.Inf(-1), 0.1 + 0.2, 4.35 * 100, 1.005 * 100, 2.675, 100.0 / 3, 200.0 / 3, 1.0 / 7 * 100}
	var values []float64
	values = append(values, fixed...)
	for i := 0; i < 4000; i++ {
		switch r.Intn(6) {
		case 0: // a percentage of integers
			d := 1 + r.Intn(300)
			values = append(values, 100*(float64(r.Intn(d*3))/float64(d)))
		case 1: // ties at the 5th decimal: k / 32 and k / 128
			values = append(values, float64(r.Intn(100000))/[]float64{32, 128, 64, 16}[r.Intn(4)])
		case 2:
			values = append(values, (r.Float64()-0.5)*math.Pow(10, float64(r.Intn(14)-6)))
		case 3: // decimals with 5 fraction digits
			values = append(values, float64(r.Intn(2000000)-1000000)/100000)
		case 4:
			values = append(values, math.Float64frombits(r.Uint64()))
		default:
			values = append(values, float64(r.Int63n(1<<53))/float64(1+r.Intn(1000)))
		}
	}
	// The JDK 17 FloatingDecimal is not always the shortest representation (C-JDK17-DOUBLE: 1e23 is "9.999999999999999E22"): beyond 1e15 the
	// digits of some doubles differ. The TSV only formats 100 * count / count, far below that.
	var inRange []float64
	for _, v := range values {
		if a := math.Abs(v); math.IsNaN(v) || math.IsInf(v, 0) || a == 0 || (a < 1e15 && a > 1e-12) {
			inRange = append(inRange, v)
		}
	}
	for lang, tag := range locales {
		nf := numberFormatFor(strings.SplitN(lang, "-", 2)[0])
		for _, v := range inRange {
			var want oText
			oracle.MustCall(t, "s6NumberFormat", map[string]any{"locale": tag, "bits": math.Float64bits(v)}, &want)
			if got := nf.format(v); got != want.String() {
				t.Fatalf("%s format(%v / %#x) = %q, jvm %q", tag, v, math.Float64bits(v), got, want.String())
			}
		}
	}
}

// ---------------------------------------------------------------------------
// the request body
// ---------------------------------------------------------------------------

func TestOracleRequestBodyDecoding(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const class = "fr.gouv.agora.infrastructure.consultationResponse.ReponsesConsultationJson"
	dec := func(body string) (string, bool) {
		var b ReponsesConsultationJSON
		if err := jsonjava.Unmarshal([]byte(body), &b); err != nil {
			return err.Error(), false
		}
		// the mapper's NullPointerException is not part of the decoding
		return jsonjava.MarshalString(b), true
	}
	check := func(body string) {
		t.Helper()
		var want string
		err := oracle.Call("jsonDecode", map[string]any{"className": class, "json": body}, &want)
		javaOK := err == nil && want != "null"
		if err != nil {
			if _, ok := err.(*oracle.OracleError); !ok {
				t.Fatalf("oracle: %v", err)
			}
		}
		got, goOK := dec(body)
		if goOK != javaOK {
			t.Errorf("%q: go ok=%v (%s), jackson ok=%v (%s / %v)", body, goOK, got, javaOK, want, err)
			return
		}
		if goOK && got != want {
			t.Errorf("%q:\n go   %s\n java %s", body, got, want)
		}
	}
	resp := []string{`{"questionId":"1","choiceIds":["a"],"responseText":null}`, `{"questionId":"1","choiceIds":[],"responseText":"t"}`, `{"questionId":1,"choiceIds":[1,2],"responseText":3}`,
		`{"questionId":"1","choiceIds":[null],"responseText":true}`, `{"choiceIds":[]}`, `{"questionId":null,"choiceIds":[]}`, `{"questionId":"1"}`, `{"questionId":"1","choiceIds":null}`,
		`{"questionId":"1","choiceIds":"a"}`, `{"questionId":"1","choiceIds":{}}`, `{"questionId":"1","choiceIds":[],"responseText":{}}`, `{"questionId":"1","choiceIds":[],"responseText":[]}`,
		`{"questionId":"1","choiceIds":[[]]}`, `{"questionId":"1","choiceIds":[{}]}`, `{"questionId":"1.5","choiceIds":[1.5]}`, `{"questionId":"1","choiceIds":[],"x":1}`, `null`, `"x"`, `1`, `[]`,
		`{"questionId":"é😀","choiceIds":["\u00e9"],"responseText":"\ud83d\ude00"}`, `{"questionId":true,"choiceIds":[false]}`}
	for _, c := range []string{`"x"`, `null`, `1`, `12.5`, `true`, `{}`, `[]`} {
		check(`{"consultationId":` + c + `,"responses":[]}`)
	}
	for _, rs := range []string{"null", `"a"`, "1", "{}", "[]", "[null]", "[null,null]", "[[]]", `["a"]`, "[1]", "[{}]", "[true]"} {
		check(`{"consultationId":"x","responses":` + rs + `}`)
	}
	for _, e := range resp {
		check(`{"consultationId":"x","responses":[` + e + `]}`)
		check(`{"consultationId":"x","responses":[` + e + `,` + e + `]}`)
	}
	for _, body := range []string{`{}`, `{"responses":[]}`, `{"consultationId":"x"}`, `{"consultationId":"x","responses":[],"responses":[null]}`, `{"consultationId":"x","responses":[null],"responses":[]}`,
		`{"consultationid":"x","Responses":[]}`, `{"consultationId":"x","responses":[]} trailing`, ``, `{`, `{"consultationId":"x",}`} {
		check(body)
	}
}
