package consultationresponse

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestMemoryRateStorePrunesExpiredWindows(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	s := newMemoryRateStore()
	s.now = func() time.Time { return now }
	for i := 0; i < 1000; i++ {
		if _, err := s.checkAndIncrement(context.Background(), "ip"+strconv.Itoa(i), 20, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(2 * time.Hour)
	if limited, _ := s.checkAndIncrement(context.Background(), "ip1", 20, time.Hour); limited {
		t.Fatal("an expired window must count as 0")
	}
	if len(s.counts) != 1 || len(s.expires) != 1 {
		t.Fatalf("not pruned: %d counts, %d expiries", len(s.counts), len(s.expires))
	}
}
