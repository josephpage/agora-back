package javacompat

import (
	"testing"
	"time"
)

func BenchmarkJavaCalendarTimeParis(b *testing.B) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		b.Skip(err)
	}
	for i := 0; i < b.N; i++ {
		JavaCalendarTime(2026, 10, 12, 14, 30, 0, 0, loc)
	}
}
