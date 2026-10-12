package notification

import (
	"math/rand"
	"testing"
	"time"

	"agora/internal/common"
	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// JVM oracle (PARITY_ORACLE=1): the real NotificationPaginatedJsonMapper, DateMapper, TypeNotification and
// NotificationMessageRepositoryImpl of the reference jar (parity/oracle/java/S8Functions.java).

func TestOraclePaginatedJSON(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(8))
	titles := []string{"Titre", "é à ü", "<b>&\"'</b>", "emoji 😀", "nbsp\u00a0x", "", "tab\tx", "\u2028", "a/b\\c"}
	for n := 0; n < 400; n++ {
		count := r.Intn(7)
		in := make([]map[string]any, 0, count)
		notes := make([]Notification, 0, count)
		for i := 0; i < count; i++ {
			title := titles[r.Intn(len(titles))]
			desc := titles[r.Intn(len(titles))]
			typ := TypeNotification(r.Intn(6))
			ms := int64(1_577_836_800_000) + r.Int63n(400*24*3600*1000) // 2020 and the following year: DST changes included
			in = append(in, map[string]any{"title": title, "description": desc, "type": typ.Name(), "millis": ms})
			notes = append(notes, Notification{Title: title, Description: desc, Type: typ, Date: time.UnixMilli(ms)})
		}
		hasMore := r.Intn(2) == 0
		var want struct {
			JSON string `json:"json"`
			XML  string `json:"xml"`
		}
		oracle.MustCall(t, "s8PaginatedJson", map[string]any{"hasMore": hasMore, "notifications": in}, &want)
		dto := ToJSON(&Page{Notifications: notes, HasMoreNotifications: hasMore})
		if got := jsonjava.MarshalString(dto); got != want.JSON {
			t.Fatalf("json (case %d):\n go   %s\n java %s", n, got, want.JSON)
		}
		if got := string(xmljava.Marshal(dto)); got != want.XML {
			t.Fatalf("xml (case %d):\n go   %s\n java %s", n, got, want.XML)
		}
	}
}

func TestOracleFormatDate(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(9))
	for n := 0; n < 300; n++ {
		ms := r.Int63n(4_102_444_800_000) // 1970 .. 2100
		var want string
		oracle.MustCall(t, "s8FormatDate", map[string]any{"millis": ms}, &want)
		if got := common.FormatDate(time.UnixMilli(ms)); got != want {
			t.Fatalf("%d: go %q, java %q", ms, got, want)
		}
	}
}

func TestOracleTypes(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	var want []struct {
		Name    string `json:"name"`
		Ordinal int    `json:"ordinal"`
	}
	oracle.MustCall(t, "s8Types", map[string]any{}, &want)
	if len(want) != 6 {
		t.Fatalf("%d constants", len(want))
	}
	for i, w := range want {
		got, ok := TypeByName(w.Name)
		if !ok || got.Ordinal() != w.Ordinal || w.Ordinal != i || got.Name() != w.Name {
			t.Fatalf("%s: go %v %v, java ordinal %d", w.Name, got, ok, w.Ordinal)
		}
	}
}

func TestOracleMessages(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	var want struct {
		Accepted    []MessageDTO `json:"accepted"`
		Rejected    MessageDTO   `json:"rejected"`
		AfterReject MessageDTO   `json:"afterReject"`
	}
	oracle.MustCall(t, "s8Messages", map[string]any{}, &want)
	var m Messages
	got := m.FindAllByStatusAccepted()
	if len(got) != len(want.Accepted) {
		t.Fatalf("%d accepted texts, java %d", len(got), len(want.Accepted))
	}
	for i := range got {
		if got[i] != want.Accepted[i] {
			t.Fatalf("accepted %d:\n go   %q\n java %q", i, got[i], want.Accepted[i])
		}
	}
	if m.GetQagRejected() != want.Rejected {
		t.Fatalf("rejected: go %q java %q", m.GetQagRejected(), want.Rejected)
	}
	if m.GetQagAcceptedAfterReject() != want.AfterReject {
		t.Fatalf("after reject: go %q java %q", m.GetQagAcceptedAfterReject(), want.AfterReject)
	}
}
