package qaglist

import (
	"cmp"
	"math"
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"agora/internal/modules/qag"
)

// The (index, score) sort with the index as tie-break gives the order of the stable
// sortedByDescending (sort.SliceStable on javaDoubleCompare) used before.
func TestTrendingSortMatchesStableSort(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	values := []float64{0, math.Copysign(0, -1), 1, 0.5, 2, math.Inf(1), math.Inf(-1), math.NaN(), 1e-300, 3}
	type scored struct {
		idx   int
		score float64
	}
	for round := 0; round < 300; round++ {
		n := rnd.Intn(400)
		in := make([]scored, n)
		for i := range in {
			if rnd.Intn(3) == 0 {
				in[i] = scored{i, rnd.Float64()}
			} else {
				in[i] = scored{i, values[rnd.Intn(len(values))]}
			}
		}
		stable := slices.Clone(in)
		sort.SliceStable(stable, func(i, j int) bool { return javaDoubleCompare(stable[j].score, stable[i].score) < 0 })
		fast := slices.Clone(in)
		slices.SortFunc(fast, func(a, b scored) int {
			if c := javaDoubleCompare(b.score, a.score); c != 0 {
				return c
			}
			return cmp.Compare(a.idx, b.idx)
		})
		for i := range stable {
			if stable[i].idx != fast[i].idx {
				t.Fatalf("round %d position %d: stable %v, fast %v", round, i, stable[i], fast[i])
			}
		}
	}
}

func TestNextHourChange(t *testing.T) {
	rnd := rand.New(rand.NewSource(9))
	base := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	q := qag.QagInfoWithSupportCount{}
	for k := 0; k < 20000; k++ {
		q.Date = base
		now := base.Add(time.Duration(rnd.Int63n(int64(400*time.Hour))) - 200*time.Hour)
		if k%3 == 0 { // exactly on a second or an hour
			now = now.Truncate(time.Second)
			if k%2 == 0 {
				now = now.Truncate(time.Hour)
			}
		}
		next := nextHourChange(base, now)
		h := hoursSince(q, now)
		if !next.After(now) {
			t.Fatalf("now %s: next %s not after", now, next)
		}
		if hoursSince(q, next) == h {
			t.Fatalf("now %s: no change at %s", now, next)
		}
		if hoursSince(q, next.Add(-time.Nanosecond)) != h {
			t.Fatalf("now %s: change before %s", now, next)
		}
	}
}

// The memoized order is the order computed afresh, whatever the clock does.
func TestTrendingRankMemo(t *testing.T) {
	rnd := rand.New(rand.NewSource(10))
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	all := make([]qag.QagInfoWithSupportCount, 300)
	for i := range all {
		all[i].ID = strconv.Itoa(i)
		all[i].Date = start.Add(-time.Duration(rnd.Int63n(int64(80 * time.Hour))))
		all[i].SupportCount = rnd.Intn(5)
		if rnd.Intn(4) == 0 {
			m := all[i].Date.Add(time.Duration(rnd.Int63n(int64(2 * time.Hour))))
			all[i].ModeratedDate = &m
		}
	}
	var r trendingRank
	now := start
	for k := 0; k < 3000; k++ {
		now = now.Add(time.Duration(rnd.Int63n(int64(3 * time.Minute))))
		if k%50 == 0 {
			now = now.Add(-time.Duration(rnd.Int63n(int64(time.Second)))) // a request that read the clock earlier
		}
		if k == 1500 { // a reloaded candidate list
			all = slices.Clone(all)
			all[7].SupportCount += 3
		}
		score := func(q qag.QagInfoWithSupportCount) float64 {
			return float64(q.SupportCount+1) / javaPow(float64(hoursSince(q, now))+2.0, 1.5)
		}
		got := r.ranked(all, now, score)
		want := rankTrending(all, score)
		if !slices.Equal(got, want) {
			t.Fatalf("step %d (%s): memo order differs", k, now)
		}
	}
}
