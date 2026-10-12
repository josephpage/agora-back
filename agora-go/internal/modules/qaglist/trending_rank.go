package qaglist

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"agora/internal/modules/qag"
)

// trendingRank memoizes the order of the trending candidates. Kotlin scores and sorts
// them at each request; the scores depend only on the candidates (the cached
// trendingQagCache list) and on hoursSince(q, now), a whole number of hours. So the
// order computed at one instant is the order of every later instant until the first
// candidate's hour count changes, or until the candidate list is reloaded: reusing it
// in that window gives exactly the order Kotlin computes.
type trendingRank struct {
	mu    sync.Mutex
	all   []qag.QagInfoWithSupportCount // the snapshot the order belongs to
	from  time.Time                     // computed at
	until time.Time                     // first instant an hour count changes
	order []int                         // indexes in all, pinned (all[0]) excluded
}

// sameSlice reports whether a and b are the same snapshot (same backing array and length).
func sameSlice(a, b []qag.QagInfoWithSupportCount) bool {
	return len(a) == len(b) && len(a) > 0 && &a[0] == &b[0]
}

// ranked returns the candidates other than the pinned one, best score first
// (sortedByDescending: stable, Double.compareTo order).
func (r *trendingRank) ranked(all []qag.QagInfoWithSupportCount, now time.Time, score func(qag.QagInfoWithSupportCount) float64) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sameSlice(r.all, all) && !now.Before(r.from) && now.Before(r.until) {
		return r.order
	}
	order := rankTrending(all, score)
	if now.Before(r.from) && sameSlice(r.all, all) {
		return order // a request that read the clock before the memo was computed: do not move the window back
	}
	until := time.Time{}
	for _, q := range all {
		if t := nextHourChange(moderatedOrPosted(q), now); until.IsZero() || t.Before(until) {
			until = t
		}
	}
	r.all, r.from, r.until, r.order = all, now, until, order
	return order
}

// rankTrending sorts the candidates as (index, score) pairs with the index as the
// tie-break: the order of the stable sort, without moving the QaG values around.
func rankTrending(all []qag.QagInfoWithSupportCount, score func(qag.QagInfoWithSupportCount) float64) []int {
	type scored struct {
		idx   int
		score float64
	}
	pinned := all[0]
	candidates := make([]scored, 0, len(all))
	for i, q := range all {
		if q.ID != pinned.ID {
			candidates = append(candidates, scored{i, score(q)})
		}
	}
	slices.SortFunc(candidates, func(a, b scored) int {
		if c := javaDoubleCompare(b.score, a.score); c != 0 {
			return c
		}
		return cmp.Compare(a.idx, b.idx)
	})
	order := make([]int, len(candidates))
	for i, c := range candidates {
		order[i] = c.idx
	}
	return order
}

func moderatedOrPosted(q qag.QagInfoWithSupportCount) time.Time {
	if q.ModeratedDate != nil {
		return *q.ModeratedDate
	}
	return q.Date
}

// nextHourChange is the first instant after now at which hoursSince (whole seconds
// floored, then divided by 3600 with truncation toward zero) of a QaG moderated at base
// differs from its value at now.
func nextHourChange(base, now time.Time) time.Time {
	ns := now.Sub(base).Nanoseconds()
	sec := ns / 1_000_000_000
	if ns%1_000_000_000 < 0 {
		sec--
	}
	h := sec / 3600
	var next int64
	switch {
	case sec >= 0:
		next = (h + 1) * 3600
	case h == 0: // -3599 .. -1 → 0 until 3600
		next = 3600
	default: // h*3600 - 3599 .. h*3600 → h, then h+1
		next = h*3600 + 1
	}
	return base.Add(time.Duration(next) * time.Second)
}
