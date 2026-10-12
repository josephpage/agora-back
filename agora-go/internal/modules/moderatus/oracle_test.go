package moderatus

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"agora/internal/javacompat"
	"agora/internal/jsonjava"
	"agora/internal/modules/qag"
	"agora/internal/store"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// These tests compare the port with the REAL Kotlin code of the reference jar (PARITY_ORACLE=1):
// parity/oracle/java/S7Functions.java.

type bodiesWant struct {
	JSON     string `json:"json"`
	XML      string `json:"xml"`
	XMLError string `json:"xmlError"`
}

// checkBodies compares the JSON and XML bytes (or the XML failure: Woodstox refuses a value it cannot write, the
// foundation then answers a 500).
func checkBodies(t *testing.T, label string, v any, want bodiesWant) {
	t.Helper()
	if got := jsonjava.MarshalString(v); got != want.JSON {
		t.Fatalf("%s json:\n go   %q\n java %q", label, got, want.JSON)
	}
	got, err := xmljava.MarshalChecked(v)
	if want.XMLError != "" {
		if err == nil {
			t.Fatalf("%s: Jackson fails (%s), Go writes %q", label, want.XMLError, got)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: Go fails (%v), Jackson writes %q", label, err, want.XML)
	}
	if string(got) != want.XML {
		t.Fatalf("%s xml:\n go   %q\n java %q", label, got, want.XML)
	}
}

// foundationEscapeTexts are the code points that Woodstox writes as lowercase `&#x..;` references: in plain element
// text U+007F..U+009F, U+FFFE and U+FFFF, in attribute values U+FFFE and U+FFFF. internal/xmljava writes them raw
// (finding F-XML-ESC, parity/ledger/S7.md). Inside CDATA both write them raw: TestOracleXMLCDATAFoundationChars keeps
// that path under test; TestOracleXMLAttributeC1Chars checks the attributes that agree; TestOracleXMLEscapeFoundation
// is the known divergence (skipped until xmljava is fixed).
var foundationEscapeTexts = []string{"\u007f del", "\u0085 nel", "\uffff nonchar", "\ufffe nonchar"}

// controlTexts are the control characters U+0000-U+001F other than TAB, LF and CR. Woodstox refuses them in plain
// element text (a 500 for both sides) but writes them raw inside CDATA, where xmljava.MarshalChecked refuses them
// too (finding F-XML-CTRL, TestOracleXMLCDATAControlFoundation). They are used by the plain-text tests only.
var controlTexts = []string{"\u0000 nul", "\u0001 ctrl", "\u0008 bs", "\u000b vt", "\u000c ff", "\u001f us", "tab\tand\u0001ctrl"}

// texts exercise the CDATA writer, the escaping and the UTF-16 handling. The plain-text checks use texts
// without foundationEscapeTexts.
var texts = []string{
	"", " ", "a", "Titre simple ?", "<b>html</b> & entités &amp; &lt;", "x ]] y", "]]", "a>b", ">", "]>", "]]>x", "x]]>", "a ]]> b",
	"émojis 😀 👨‍👩‍👧‍👦 accents éàüçœ ﬁ", "guillemets \"doubles\" et 'simples' \\ backslash", "retour\nà la ligne", "tab\tchar", "cr\rchar", "crlf\r\nchar",
	"ünï\u2028code\u2029", "\u00a0nbsp\u00a0", "\ufeffbom", "\ufffd replacement",
	strings.Repeat("x", 511) + ">", strings.Repeat("é", 600), strings.Repeat("a]", 300) + "]>",
	strings.Repeat("<&>", 300), "line1\nline2\r\nline3\r", "<![CDATA[inner]]>", "&#x41; &#65;", "<?xml version=\"1.0\"?>", "100% ça marche", "x'\"y",
}

func randText(r *rand.Rand) string {
	if r.Intn(4) == 0 {
		var b strings.Builder
		for i, n := 0, r.Intn(40); i < n; i++ {
			b.WriteRune(rune([]rune("ab]>& <\"é😀\n\r\t'\\")[r.Intn(15)]))
		}
		return b.String()
	}
	return texts[r.Intn(len(texts))]
}

func TestOracleListXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(71))
	run := func(label string, qags []ModeratusQag) {
		t.Helper()
		args := make([]map[string]any, len(qags))
		for i, q := range qags {
			args[i] = map[string]any{"qagId": q.QagID, "title": q.Title, "description": q.Description, "dateMs": q.Date.UnixMilli(), "username": q.Username, "userId": q.UserID}
		}
		var want bodiesWant
		oracle.MustCall(t, "moderatusListXml", map[string]any{"qags": args}, &want)
		checkBodies(t, label, toListXML(qags), want)
	}
	run("empty", nil)
	for _, s := range texts {
		run("text "+fmt.Sprintf("%q", s), []ModeratusQag{{QagID: "id", Title: s, Description: s, Date: time.UnixMilli(1704380400123), Username: s, UserID: "user"}})
		run("title only "+fmt.Sprintf("%q", s), []ModeratusQag{{QagID: s, Title: s, Description: "d", Date: time.UnixMilli(0), Username: "u", UserID: s}})
	}
	for _, s := range controlTexts { // a control character in a plain element: both sides refuse the whole body
		run("control id "+fmt.Sprintf("%q", s), []ModeratusQag{{QagID: s, Title: "t", Description: "d", Date: time.UnixMilli(0), Username: "u", UserID: "x"}})
	}
	for i := 0; i < 400; i++ {
		var qags []ModeratusQag
		for n := r.Intn(6); n > 0; n-- {
			qags = append(qags, ModeratusQag{
				QagID: fmt.Sprintf("00000000-0000-4000-8000-%012x", r.Intn(1<<30)), Title: randText(r), Description: randText(r),
				Date: time.UnixMilli(r.Int63n(130*365*86400) * 1000).Add(time.Duration(r.Intn(1000)) * time.Millisecond), Username: randText(r),
				UserID: fmt.Sprintf("00000000-0000-4000-9000-%012x", r.Intn(1<<30)),
			})
		}
		run(fmt.Sprintf("random %d", i), qags)
	}
	// dates outside 1..9999 and around the epoch / DST changes
	for _, ms := range []int64{-62135596800000, -62135596800001, -1, 0, 253402300799000, 253402300800000, 1711846799000, 1711846800000, 1729994400000, 1729998000000, 4102444800000, 1e14, -1e14} {
		run(fmt.Sprintf("date %d", ms), []ModeratusQag{{QagID: "i", Title: "t", Description: "d", Date: time.UnixMilli(ms), Username: "u", UserID: "x"}})
	}
}

// TestOracleXMLCDATAFoundationChars: inside CDATA (title, description, username) Woodstox and xmljava both write the
// F-XML-ESC code points raw.
func TestOracleXMLCDATAFoundationChars(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	date := time.UnixMilli(1704380400123)
	for _, s := range foundationEscapeTexts {
		args := []map[string]any{{"qagId": "id", "title": s, "description": s, "dateMs": date.UnixMilli(), "username": s, "userId": "user"}}
		var want bodiesWant
		oracle.MustCall(t, "moderatusListXml", map[string]any{"qags": args}, &want)
		q := []ModeratusQag{{QagID: "id", Title: s, Description: s, Date: date, Username: s, UserID: "user"}}
		checkBodies(t, fmt.Sprintf("cdata %q", s), toListXML(q), want)
	}
}

// TestOracleXMLEscapeFoundation is the known foundation divergence F-XML-ESC: Woodstox writes the element-text code
// points of foundationEscapeTexts and the attribute non-characters U+FFFE / U+FFFF as &#x..; (lowercase hex),
// internal/xmljava writes them the same way (formerly the foundation divergence F-XML-ESC).
func TestOracleXMLEscapeFoundation(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, s := range foundationEscapeTexts {
		var want bodiesWant
		oracle.MustCall(t, "moderatusListErrorXml", map[string]any{"message": s}, &want)
		checkBodies(t, fmt.Sprintf("error %q", s), ModeratusQagListErrorXML{ErrorMessage: s}, want)
	}
	for _, s := range []string{"￾ nonchar", "￿ nonchar"} {
		var want bodiesWant
		oracle.MustCall(t, "moderatusLockXml", map[string]any{"ids": []string{s}, "errorMessage": "x"}, &want)
		checkBodies(t, fmt.Sprintf("attribute %q", s), lockResultsXML(errorLockResults([]string{s}, "x")), want)
	}
}

// TestOracleXMLAttributeC1Chars: U+007F..U+009F are written raw in attribute values by Woodstox and by xmljava
// (the lock results echo the ids of the request in content_id).
func TestOracleXMLAttributeC1Chars(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, s := range []string{"\u007f del", "\u0085 nel", "\u0080x\u009f"} {
		var want bodiesWant
		oracle.MustCall(t, "moderatusLockXml", map[string]any{"ids": []string{s}, "errorMessage": "x"}, &want)
		checkBodies(t, fmt.Sprintf("attribute %q", s), lockResultsXML(errorLockResults([]string{s}, "x")), want)
	}
}

// TestOracleXMLCDATAControlFoundation is the known foundation divergence F-XML-CTRL: a control character inside CDATA
// (here the username of a QaG, `pseudo`) is written raw by Woodstox (HTTP 200) and refused by xmljava.MarshalChecked
// (HTTP 500) before xmljava accepted them (formerly the foundation divergence F-XML-CTRL).
func TestOracleXMLCDATAControlFoundation(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, s := range controlTexts {
		args := []map[string]any{{"qagId": "id", "title": "t", "description": "d", "dateMs": int64(0), "username": s, "userId": "user"}}
		var want bodiesWant
		oracle.MustCall(t, "moderatusListXml", map[string]any{"qags": args}, &want)
		q := []ModeratusQag{{QagID: "id", Title: "t", Description: "d", Date: time.UnixMilli(0), Username: s, UserID: "user"}}
		checkBodies(t, fmt.Sprintf("cdata %q", s), toListXML(q), want)
	}
}

func TestOracleListErrorXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, m := range append(append([]string{loginFailedMessage}, texts...), controlTexts...) {
		var want bodiesWant
		oracle.MustCall(t, "moderatusListErrorXml", map[string]any{"message": m}, &want)
		checkBodies(t, fmt.Sprintf("%q", m), ModeratusQagListErrorXML{ErrorMessage: m}, want)
	}
}

func TestOracleLockXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(72))
	// results of the use case
	for i := 0; i < 300; i++ {
		var results []ModeratusQagLockResult
		var args []map[string]any
		for n := r.Intn(5); n > 0; n-- {
			id := randText(r)
			lr, name := LockSuccess, "SUCCESS"
			if r.Intn(2) == 0 {
				lr, name = LockNotFound, "NOT_FOUND"
			}
			results = append(results, ModeratusQagLockResult{QagID: id, LockResult: lr})
			args = append(args, map[string]any{"qagId": id, "lockResult": name})
		}
		var want bodiesWant
		oracle.MustCall(t, "moderatusLockXml", map[string]any{"results": args}, &want)
		checkBodies(t, fmt.Sprintf("results %d", i), lockResultsXML(toLockResultsXML(results)), want)
	}
	// the error answer (wrong password) for the ids of the request
	for i := 0; i < 300; i++ {
		var ids []string
		for n := r.Intn(5); n > 0; n-- {
			ids = append(ids, randText(r))
		}
		var want bodiesWant
		oracle.MustCall(t, "moderatusLockXml", map[string]any{"ids": append([]string{}, ids...), "errorMessage": lockLoginFailedMessage}, &want)
		checkBodies(t, fmt.Sprintf("error %d", i), lockResultsXML(errorLockResults(ids, lockLoginFailedMessage)), want)
	}
	for _, m := range append(append([]string{}, texts...), controlTexts...) {
		var want bodiesWant
		oracle.MustCall(t, "moderatusLockXml", map[string]any{"ids": []string{"a"}, "errorMessage": m}, &want)
		checkBodies(t, fmt.Sprintf("message %q", m), lockResultsXML(errorLockResults([]string{"a"}, m)), want)
	}
}

func TestOracleModerateXML(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	names := map[ModeratusQagModerateResult]string{ModerateSuccess: "SUCCESS", ModerateNotFound: "NOT_FOUND", ModerateFailure: "FAILURE"}
	for _, m := range append(append([]string{moderateDatabaseErrorMsg, loginFailedMessage, "Status invalide"}, texts...), controlTexts...) {
		for res, name := range names {
			var want bodiesWant
			oracle.MustCall(t, "moderatusModerateXml", map[string]any{"result": name, "message": m}, &want)
			checkBodies(t, fmt.Sprintf("%s %q", name, m), toModerateResultXML(res, m), want)
		}
		var want bodiesWant
		oracle.MustCall(t, "moderatusModerateXml", map[string]any{"message": m}, &want)
		checkBodies(t, fmt.Sprintf("toErrorXml %q", m), toModerateResultXML(ModerateFailure, m), want)
	}
}

func TestOracleModerateOptions(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	statuses := []string{"OK", "NOK", "ok", "Ok", "nok", "", " OK", "OK ", "KO", "Coucou !", "OK,OK"}
	reasons := []*string{nil, sp(""), sp("Parce que !"), sp("é😀")}
	for _, st := range statuses {
		for _, rs := range reasons {
			for _, flag := range []*int{nil, intp(0), intp(1), intp(2), intp(-1), intp(56)} {
				for _, motif := range []*string{nil, sp("motif42")} {
					args := map[string]any{"qagId": "q", "userId": "u", "status": st, "reason": rs, "motifId": motif}
					if flag != nil {
						args["shouldDeleteFlag"] = *flag
					} else {
						args["shouldDeleteFlag"] = nil
					}
					var want struct {
						Error        *string `json:"error"`
						QagID        string  `json:"qagId"`
						UserID       string  `json:"userId"`
						IsAccepted   bool    `json:"isAccepted"`
						Reason       *string `json:"reason"`
						ShouldDelete bool    `json:"shouldDelete"`
						MotifID      *string `json:"motifId"`
					}
					oracle.MustCall(t, "moderateOptions", args, &want)
					got := ToModerateQagOptions("q", "u", st, rs, flag, motif)
					if want.Error != nil {
						if got.Error == nil || *got.Error != *want.Error {
							t.Fatalf("%q: go %+v java error %q", st, got, *want.Error)
						}
						continue
					}
					w := ModerateQagOptions{QagID: want.QagID, UserID: want.UserID, IsAccepted: want.IsAccepted, Reason: want.Reason, ShouldDelete: want.ShouldDelete, MotifID: want.MotifID}
					if got.Error != nil || !reflect.DeepEqual(got.Options, w) {
						t.Fatalf("%q %v %v: go %+v java %+v", st, rs, flag, got, w)
					}
				}
			}
		}
	}
}

func intp(n int) *int { return &n }

func TestOracleNotificationMessages(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	type msg struct{ Title, Message string }
	var want struct {
		Rejected            msg   `json:"rejected"`
		AcceptedAfterReject msg   `json:"acceptedAfterReject"`
		Accepted            []msg `json:"accepted"`
	}
	oracle.MustCall(t, "qagNotificationMessages", map[string]any{}, &want)
	if want.Rejected != (msg{qagRejectedTitle, qagRejectedMessage}) || want.AcceptedAfterReject != (msg{qagAcceptedAfterRejectTitle, qagAcceptedAfterRejectMessage}) {
		t.Fatalf("%+v", want)
	}
	if len(want.Accepted) != len(QagAcceptedNotifications) {
		t.Fatalf("%d accepted messages in Kotlin, %d in Go", len(want.Accepted), len(QagAcceptedNotifications))
	}
	for i, m := range QagAcceptedNotifications {
		if (msg{m.Title, m.Message}) != want.Accepted[i] {
			t.Fatalf("accepted %d:\n go   %+v\n java %+v", i, m, want.Accepted[i])
		}
	}
}

type flowWant struct {
	Result string   `json:"result"`
	Calls  []string `json:"calls"`
}

// The whole moderation (use case + the real SendQagNotificationUseCase) on recording repositories.
func TestOracleModerateFlow(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	statuses := []qag.QagStatus{qag.StatusOpen, qag.StatusModeratedAccepted, qag.StatusModeratedRejected, qag.StatusArchived, qag.StatusSelectedForResponse}
	for _, st := range statuses {
		for _, accepted := range []bool{true, false} {
			for _, fails := range []bool{false, true} {
				for _, token := range []*string{sp("fcm-token"), nil} {
					for _, missing := range []bool{false, true} {
						for _, o := range []ModerateQagOptions{{}, {Reason: sp("raison"), ShouldDelete: true, MotifID: sp("motif")}} {
							label := fmt.Sprintf("%s accepted=%t fails=%t token=%v missing=%t", st, accepted, fails, token, missing)
							optionsID := "qagId"
							if missing {
								optionsID = "other"
							}
							args := map[string]any{
								"qag": map[string]any{"id": "qagId", "status": st.String(), "userId": "author"}, "optionsQagId": optionsID,
								"isAccepted": accepted, "reason": o.Reason, "shouldDelete": o.ShouldDelete, "motifId": o.MotifID, "updateFails": fails,
								"userFcmToken": token,
							}
							var want flowWant
							oracle.MustCall(t, "moderateFlow", args, &want)

							log := &callLog{}
							qags := &fakeQags{log: log, byID: map[string]*qag.QagInfo{"qagId": {ID: "qagId", Status: st, UserID: "author"}}, updateFails: fails}
							u := &ModerateModeratusQagUseCase{
								qags: qags, updates: &fakeUpdates{log: log}, locks: &fakeLocks{log: log},
								notifier: newTestNotifier(log, qags, token, 0),
							}
							o.QagID, o.UserID, o.IsAccepted = optionsID, "moderator", accepted
							res, err := u.ModerateQag(ctx, o)
							got := map[ModeratusQagModerateResult]string{ModerateSuccess: "SUCCESS", ModerateNotFound: "NOT_FOUND", ModerateFailure: "FAILURE"}[res]
							if err != nil {
								got = "THROWS " + err.Error()
							}
							if strings.HasPrefix(want.Result, "THROWS") {
								if err == nil {
									t.Fatalf("%s: Kotlin throws (%s), Go returns %s\n%v", label, want.Result, got, want.Calls)
								}
								got = want.Result // the exception class is not compared
							}
							if got != want.Result || !equalStrings(collapseGetQagInfo(log.list()), collapseGetQagInfo(want.Calls)) {
								t.Fatalf("%s:\n go   %s %q\n java %s %q", label, got, collapseGetQagInfo(log.list()), want.Result, collapseGetQagInfo(want.Calls))
							}
						}
					}
				}
			}
		}
	}
}

func TestOracleLockFlow(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	const (
		open   = "00000000-0000-4000-8000-000000000023"
		openB  = "00000000-0000-4000-8000-000000000021"
		locked = "00000000-0000-4000-8000-000000000022"
		arch   = "00000000-0000-4000-8000-000000000041"
	)
	existing := map[string]string{open: "OPEN", openB: "OPEN", locked: "OPEN", arch: "ARCHIVED"}
	idLists := [][]string{
		{}, {open}, {open, open}, {locked}, {arch}, {"unknown"}, {open, locked, arch, openB}, {strings.ToUpper(open)}, {strings.ToUpper(locked)}, {open, strings.ToUpper(open)},
		{"0-0-4000-8000-23"}, {" " + open}, {open, "not-a-uuid", openB}, {"", open}, {open + "0"},
	}
	for _, ids := range idLists {
		var want struct {
			Results []struct {
				QagID      string `json:"qagId"`
				LockResult string `json:"lockResult"`
			} `json:"results"`
			Calls []string `json:"calls"`
		}
		oracle.MustCall(t, "lockFlow", map[string]any{"ids": ids, "existing": existing, "locked": []string{locked}}, &want)

		log := &callLog{}
		qags := &fakeQags{log: log, byID: map[string]*qag.QagInfo{}}
		for id, st := range existing {
			s, _ := qag.ParseStatus(st)
			qags.byID[id] = &qag.QagInfo{ID: id, Status: s}
		}
		locks := &fakeLocks{log: log, locked: []string{locked}}
		u := &LockModeratusQagListUseCase{qags: qags, locks: locks}
		got, err := u.LockQagIDs(ctx, ids)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want.Results) {
			t.Fatalf("%q: go %v java %v", ids, got, want.Results)
		}
		for i, g := range got {
			lr := map[QagLockResult]string{LockSuccess: "SUCCESS", LockNotFound: "NOT_FOUND"}[g.LockResult]
			if g.QagID != want.Results[i].QagID || lr != want.Results[i].LockResult {
				t.Fatalf("%q: go %v java %v", ids, got, want.Results)
			}
		}
		// calls: getQagInfo for each distinct id; then saveAll of the canonical UUIDs of the successes that parse
		var goInfo, javaInfo []string
		for _, c := range log.list() {
			if strings.HasPrefix(c, "getQagInfo(") {
				goInfo = append(goInfo, c)
			}
		}
		javaSaved := ""
		for _, c := range want.Calls {
			if strings.HasPrefix(c, "getQagInfo(") {
				javaInfo = append(javaInfo, c)
			}
			if strings.HasPrefix(c, "saveAll") {
				javaSaved = strings.TrimPrefix(c, "saveAll")
			}
		}
		if !reflect.DeepEqual(goInfo, javaInfo) {
			t.Fatalf("%q: getQagInfo calls go %q java %q", ids, goInfo, javaInfo)
		}
		goSaved := ""
		if uuids := lockedUUIDs(locks.added); len(uuids) > 0 {
			goSaved = "[" + strings.Join(uuids, ", ") + "]"
		}
		if goSaved != javaSaved {
			t.Fatalf("%q: saved go %q java %q", ids, goSaved, javaSaved)
		}
	}
}

func TestOracleRemoveLocked(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, id := range []string{
		"00000000-0000-4000-8000-000000000023", "00000000-0000-4000-8000-00000000002A", "0-0-4000-8000-23", "lockedQagId with invalid UUID", "", " 00000000-0000-4000-8000-000000000023",
		"00000000-0000-4000-8000-0000000000230", "+0-0-4000-8000-23", "-0-0-4000-8000-23", "0x1-0-4000-8000-23", "00000000-0000-4000-8000-00000000002٣",
	} {
		var want []string
		oracle.MustCall(t, "removeLockedFlow", map[string]any{"qagId": id}, &want)
		_, parses := javacompat.ParseUUID(id)
		if parses != (len(want) == 1) {
			t.Errorf("%q: Go reaches the delete: %t, Kotlin: %v", id, parses, want)
		}
	}
}

// ---------------------------------------------------------------------------

func TestOracleArchiveDates(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(77))
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local)
	for i := 0; i < 1500; i++ {
		now := start.Add(time.Duration(r.Int63n(int64(3*365*24*time.Hour)/int64(time.Millisecond))) * time.Millisecond)
		if i%3 == 0 {
			// around the Monday 10:00 boundary and midnight
			monday := time.Date(now.Year(), now.Month(), now.Day()-((int(now.Weekday())+6)%7), 10, 0, 0, 0, time.Local)
			now = monday.Add(time.Duration(r.Intn(5)-2) * time.Second).Add(time.Duration(r.Intn(3)-1) * time.Millisecond)
		}
		extra := int64(r.Intn(1000)) * 1000
		now = now.Add(time.Duration(extra))
		flags := map[string]bool{"QagSelect": r.Intn(4) != 0, "QagAnonymization": r.Intn(4) != 0}
		var want struct {
			Archive   string `json:"archive"`
			Anonymize string `json:"anonymize"`
			Calls     []struct {
				Call   string `json:"call"`
				DateMs int64  `json:"dateMs"`
			} `json:"calls"`
		}
		oracle.MustCall(t, "archiveDates", map[string]any{"nowMs": now.UnixMilli(), "nowExtraNanos": now.Nanosecond() % 1_000_000, "flags": flags}, &want)

		type call struct {
			name string
			ms   int64
		}
		var got []call
		store := &recordingArchiver{rec: func(name string, d time.Time) { got = append(got, call{name, d.UnixMilli()}) }}
		nowFn := func() time.Time { return now }
		arch := &ArchiveOldQagUseCase{qagSelect: constFlag(flags["QagSelect"]), qags: store, now: nowFn, afterSave: func(context.Context) {}, logf: func(string, ...any) {}}
		anon := &AnonymizeOldQagUseCase{anonymization: constFlag(flags["QagAnonymization"]), qags: store, now: nowFn, afterSave: func(context.Context) {}, logf: func(string, ...any) {}}
		ar, err := arch.ArchiveOldQag(ctx)
		if err != nil {
			t.Fatal(err)
		}
		an, err := anon.AnonymizeOldQag(ctx)
		if err != nil {
			t.Fatal(err)
		}
		gotArchive := map[ArchiveQagListResult]string{ArchiveSuccess: "SUCCESS", ArchiveFailure: "FAILURE"}[ar]
		gotAnon := map[AnonymizeQagListResult]string{AnonymizeSuccess: "SUCCESS", AnonymizeFailure: "FAILURE"}[an]
		if gotArchive != want.Archive || gotAnon != want.Anonymize || len(got) != len(want.Calls) {
			t.Fatalf("now %s flags %v: go %s %s %v java %+v", now.Format(time.RFC3339Nano), flags, gotArchive, gotAnon, got, want)
		}
		for i, c := range got {
			if c.name != want.Calls[i].Call || c.ms != want.Calls[i].DateMs {
				t.Fatalf("now %s: go %v java %+v", now.Format(time.RFC3339Nano), got, want.Calls)
			}
		}
	}
}

func constFlag(b bool) FeatureChecker {
	return func(context.Context) (bool, error) { return b, nil }
}

type recordingArchiver struct {
	rec func(name string, d time.Time)
}

func (r *recordingArchiver) ArchiveOldQags(_ context.Context, d time.Time) error {
	r.rec("archiveOldQags", store.Millis(d))
	return nil
}

func (r *recordingArchiver) AnonymizeOldQags(_ context.Context, d time.Time) error {
	r.rec("anonymizeOldQags", store.Millis(d))
	return nil
}
