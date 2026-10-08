package xmljava_test

import (
	"encoding/json"
	"testing"

	"agora/internal/xmljava"
	"agora/parity/oracle"
)

type thematique struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Picto string `json:"picto"`
}
type thematiques struct {
	Thematiques []thematique `json:"thematiques"`
}

func (thematiques) JavaName() string { return "ThematiquesJson" }

type errResp struct {
	Title string `json:"title"`
}

func (errResp) JavaName() string { return "ErrorResponse" }

type modQag struct {
	QagID       string `json:"qagId" xml:"content_id"`
	PostDate    string `json:"postDate" xml:"date"`
	UserID      string `json:"userId" xml:"user_id"`
	Username    string `json:"username" xml:"pseudo,cdata"`
	Title       string `json:"title" xml:"title,cdata"`
	Description string `json:"description" xml:"body,cdata"`
	MessageType string `json:"messageType" xml:"message_type"`
}
type modList struct {
	Count int      `json:"qagToModerateCount" xml:"nb_content"`
	Qags  []modQag `json:"qagsToModerate" xml:"msg,unwrapped"`
}

func (modList) RootName() string { return "contents" }

type lockResult struct {
	QagID   string  `json:"qagId" xml:"content_id,attr"`
	Result  string  `json:"result" xml:"result,attr"`
	Comment *string `json:"comment" xml:"comment,attr"`
}
type lockResults struct {
	Results []lockResult `json:"qagLockedResults" xml:"confirmation,unwrapped"`
}

func (lockResults) RootName() string { return "contents" }

type moderateResult struct {
	Result string  `json:"result" xml:"result"`
	Error  *string `json:"error" xml:"error"`
}

func (moderateResult) RootName() string { return "contents" }

func check(t *testing.T, className string, v any) {
	t.Helper()
	in, _ := json.Marshal(v)
	var want string
	oracle.MustCall(t, "xmlSerialize", map[string]any{"className": className, "json": string(in)}, &want)
	got := string(xmljava.Marshal(v))
	if got != want {
		t.Errorf("%s:\n go   %s\n java %s", className, got, want)
	}
}

func TestOracleXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	tricky := "a & b < c > d \" e ' f \r\n\t g é 😀 ]]> h"
	check(t, "fr.gouv.agora.infrastructure.thematique.ThematiquesJson", thematiques{Thematiques: []thematique{{"1", tricky, "x"}, {"2", "", ""}}})
	check(t, "fr.gouv.agora.infrastructure.thematique.ThematiquesJson", thematiques{Thematiques: []thematique{}})
	check(t, "fr.gouv.agora.infrastructure.common.ErrorResponse", errResp{Title: tricky})
	check(t, "fr.gouv.agora.infrastructure.moderatus.ModeratusQagListXml", modList{Count: 2, Qags: []modQag{
		{"id1", "2026-01-01 10:00:00", "u1", "pseudo & <x>", "title é", "body\nline", "question"},
		{"id2", "2026-01-02 10:00:00", "u2", "", "t", "b", "question"},
	}})
	check(t, "fr.gouv.agora.infrastructure.moderatus.ModeratusQagListXml", modList{Count: 0, Qags: []modQag{}})
	c := "Password invalide"
	check(t, "fr.gouv.agora.infrastructure.moderatus.ModeratusQagLockResultsXml", lockResults{Results: []lockResult{{"a", "OK", nil}, {"b", "ERROR", &c}, {"c&\"<", "NOTFOUND", &tricky}}})
	e := "Status invalide"
	check(t, "fr.gouv.agora.infrastructure.moderatus.ModeratusQagModerateResultPageXml", moderateResult{Result: "ERROR", Error: &e})
	check(t, "fr.gouv.agora.infrastructure.moderatus.ModeratusQagModerateResultPageXml", moderateResult{Result: "OK", Error: nil})
}

type goal struct {
	Picto string `json:"picto"`
}

type nullableLists struct {
	Footer *goal    `json:"footer"`
	Goals  []goal   `json:"goals,nullable"`
	Items  []goal   `json:"items"`
	Names  []string `json:"names,nullable"`
}

// Jackson XML omits a null Kotlin List? property (ConsultationDetailsV2Json.goals),
// writes a null object as an empty element and a non-null empty list as <x/>.
func TestNullableListOmitted(t *testing.T) {
	got := string(xmljava.Marshal(nullableLists{}))
	want := "<nullableLists><footer/><items/></nullableLists>"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	got = string(xmljava.Marshal(nullableLists{Goals: []goal{}, Names: []string{"a"}}))
	want = "<nullableLists><footer/><goals/><items/><names><names>a</names></names></nullableLists>"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
