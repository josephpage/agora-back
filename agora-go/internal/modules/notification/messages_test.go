package notification

import "testing"

// The texts are compared with the Kotlin constants by the JVM oracle (TestOracleMessages).
func TestMessages(t *testing.T) {
	var m Messages
	if n := len(m.FindAllByStatusAccepted()); n != 8 {
		t.Fatalf("8 accepted texts, got %d", n)
	}
	seen := map[MessageDTO]bool{}
	for _, x := range m.FindAllByStatusAccepted() {
		if seen[x] {
			t.Fatalf("duplicate text %v", x)
		}
		seen[x] = true
	}
	for i := 0; i < 200; i++ {
		if got := m.RandomAccepted(); !seen[got] {
			t.Fatalf("random text outside the set: %v", got)
		}
	}
	if got := m.GetQagRejected(); got.Title != "Question hors Charte." {
		t.Fatal(got)
	}
	if got := m.GetQagAcceptedAfterReject(); got.Title != "Bonne nouvelle !" {
		t.Fatal(got)
	}
}
