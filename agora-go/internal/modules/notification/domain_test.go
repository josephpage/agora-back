package notification

import (
	"errors"
	"testing"
	"time"

	"agora/internal/httpx"
)

func TestTypeByName(t *testing.T) {
	for i, name := range []string{"ALL_REPONSES_QAGS", "HOME_QAGS", "DETAILS_QAG", "HOME_CONSULTATIONS", "DETAILS_CONSULTATION", "REPONSE_SUPPORT"} {
		got, ok := TypeByName(name)
		if !ok || int(got) != i || got.Name() != name || got.Ordinal() != i {
			t.Fatalf("%s: got %v %v", name, got, ok)
		}
	}
	// Enum.valueOf: the exact constant name only (Spring trims the text before, see typeParam)
	for _, bad := range []string{"", "home_qags", " HOME_QAGS", "HOME_QAGS ", "UNKNOWN"} {
		if _, ok := TypeByName(bad); ok {
			t.Fatalf("%q must not be a constant", bad)
		}
	}
}

func TestTypeByOrdinal(t *testing.T) {
	for _, o := range []int{-1, 6, 100} {
		if _, ok := TypeByOrdinal(o); ok {
			t.Fatalf("ordinal %d must not exist", o)
		}
	}
	if got, ok := TypeByOrdinal(5); !ok || got != ReponseSupport {
		t.Fatalf("ordinal 5: %v %v", got, ok)
	}
}

// Raise is what DefaultControllerAdvice makes of the exceptions of this slice.
func TestRaise(t *testing.T) {
	cases := []struct {
		err    error
		status int
		title  string
	}{
		{&UserIDInconnuError{UserID: "x"}, 404, "Veuillez renseigner un id d'utilisateur existant."},
		{&FcmTokenVideError{}, 400, "Veuillez renseigner un FcmToken."},
		{&QagIDInconnuError{QagID: "x"}, 404, "Veuillez renseigner un id de qag existant."},
	}
	for _, c := range cases {
		func() {
			defer func() {
				e, ok := recover().(*httpx.AdviceError)
				if !ok || e.Status != c.status || e.Title != c.title {
					t.Fatalf("%v: got %v", c.err, e)
				}
			}()
			Raise(c.err)
		}()
	}
	// any other error is an uncaught exception (HTTP 500)
	boom := errors.New("boom")
	defer func() {
		if r := recover(); r != boom {
			t.Fatalf("want the error itself to be re-panicked, got %v", r)
		}
	}()
	Raise(boom)
}

func TestErrorMessages(t *testing.T) {
	if got := (&UserIDInconnuError{UserID: "u1"}).Error(); got != "User with id: u1 does not exist." {
		t.Fatal(got)
	}
	if got := (&QagIDInconnuError{QagID: "q1"}).Error(); got != "There is no qag with id q1" {
		t.Fatal(got)
	}
	if got := (&FcmTokenVideError{}).Error(); got != "Fcm token should not be empty" {
		t.Fatal(got)
	}
}

// NotificationPaginatedJsonMapper: the category of each type and the date format.
func TestToJSON(t *testing.T) {
	d := time.Date(2026, 10, 1, 9, 5, 7, 0, time.Local)
	page := &Page{HasMoreNotifications: true, Notifications: []Notification{
		{Title: "a", Description: "da", Type: AllReponsesQags, Date: d},
		{Title: "b", Description: "db", Type: HomeQags, Date: d},
		{Title: "c", Description: "dc", Type: DetailsQag, Date: d},
		{Title: "d", Description: "dd", Type: HomeConsultations, Date: d},
		{Title: "e", Description: "de", Type: DetailsConsultation, Date: d},
		{Title: "f", Description: "df", Type: ReponseSupport, Date: d},
	}}
	out := ToJSON(page)
	if !out.HasMoreNotifications || len(out.Notifications) != 6 {
		t.Fatalf("%+v", out)
	}
	wantTypes := []string{"Questions citoyennes", "Questions citoyennes", "Questions citoyennes", "Consultations", "Consultations", "Réponse du support"}
	for i, n := range out.Notifications {
		if n.Type != wantTypes[i] {
			t.Fatalf("%d: type %q", i, n.Type)
		}
		if n.Date != "2026-10-01 09:05:07" {
			t.Fatalf("%d: date %q", i, n.Date)
		}
	}
	empty := ToJSON(&Page{})
	if empty.Notifications == nil || len(empty.Notifications) != 0 || empty.HasMoreNotifications {
		t.Fatalf("an empty page is an empty array: %+v", empty)
	}
}

// Kotlin String.toIntOrNull and the stored type of the notifications (Hibernate ORDINAL, read through getByte).
func TestParseStoredType(t *testing.T) {
	ok := map[string]TypeNotification{
		"0": AllReponsesQags, "1": HomeQags, "5": ReponseSupport,
		" 2 ": DetailsQag, "+3": HomeConsultations, "1.0": HomeQags, "4.99": DetailsConsultation,
		"-0.5": AllReponsesQags, "1e0": HomeQags, "500e-2": ReponseSupport, ".5": AllReponsesQags, "1.": HomeQags,
	}
	for in, want := range ok {
		got, valid := parseStoredType(in)
		if !valid || got != want {
			t.Fatalf("%q: got %v %v, want %v", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "   ", "6", "-1", "128", "abc", "1x", "1e3", "NaN"} {
		if got, valid := parseStoredType(in); valid {
			t.Fatalf("%q must fail, got %v", in, got)
		}
	}
}
