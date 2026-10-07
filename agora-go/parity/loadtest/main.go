// Command loadtest drives realistic traffic profiles against one backend and
// reports throughput and latency percentiles, to compare the Kotlin reference
// and the Go rewrite under identical resource limits.
//
//	go run ./parity/loadtest -target http://localhost:8082 -profile app-open -c 200 -d 60s -users 2000
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"agora/internal/auth"
)

type request struct {
	name    string
	weight  int
	method  string
	path    func(r *rand.Rand, u int) string
	auth    bool
	body    func(r *rand.Rand, u int) string
	headers map[string]string
}

func bulkUser(n int) string { return fmt.Sprintf("00000000-0000-4000-9001-%012x", n) }

var thematiques = []string{"th0000000000000000000001", "th0000000000000000000002", "th0000000000000000000003", "th0000000000000000000004", "th0000000000000000000005", "th0000000000000000000006"}

func static(p string) func(*rand.Rand, int) string { return func(*rand.Rand, int) string { return p } }

// profiles model what clients do (agora-app startup flow and screens).
var profiles = map[string][]request{
	// App opening: login + home screens (the dominant traffic).
	"app-open": {
		{name: "qags-top", weight: 20, method: "GET", path: static("/v2/qags?pageNumber=1&filterType=top"), auth: true},
		{name: "qags-trending", weight: 20, method: "GET", path: static("/v2/qags?pageNumber=1&filterType=trending"), auth: true},
		{name: "qags-latest-them", weight: 8, method: "GET", path: func(r *rand.Rand, _ int) string {
			return "/v2/qags?pageNumber=1&filterType=latest&thematiqueId=" + thematiques[r.Intn(len(thematiques))]
		}, auth: true},
		{name: "qags-supporting", weight: 5, method: "GET", path: static("/v2/qags?pageNumber=1&filterType=supporting"), auth: true},
		{name: "consultations", weight: 15, method: "GET", path: static("/consultations"), auth: true},
		{name: "thematiques", weight: 8, method: "GET", path: static("/thematiques")},
		{name: "theme-hebdo", weight: 6, method: "GET", path: static("/theme_hebdo")},
		{name: "last-news", weight: 6, method: "GET", path: static("/welcome_page/last_news")},
		{name: "qag-responses", weight: 5, method: "GET", path: static("/qags/responses")},
		{name: "notifications", weight: 4, method: "GET", path: static("/notifications/paginated/1"), auth: true},
		{name: "profile", weight: 3, method: "GET", path: static("/profile"), auth: true},
	},
	// Consultation launch: details + questions + answers + results.
	"consultation": {
		{name: "details", weight: 30, method: "GET", path: static("/v2/consultations/co0000000000000000000001"), auth: true},
		{name: "questions", weight: 25, method: "GET", path: static("/consultations/co0000000000000000000001/questions")},
		{name: "results", weight: 20, method: "GET", path: static("/v2/consultations/co0000000000000000000004/responses")},
		{name: "answer", weight: 10, method: "POST", path: static("/consultations/co0000000000000000000001/responses"), auth: true, body: func(r *rand.Rand, u int) string {
			return `{"consultationId":"co0000000000000000000001","responses":[{"questionId":"101","choiceIds":["1011"],"responseText":""},{"questionId":"103","choiceIds":[],"responseText":"Réponse libre de test"}]}`
		}, headers: map[string]string{"X-Forwarded-For": ""}},
		{name: "list", weight: 15, method: "GET", path: static("/consultations"), auth: true},
	},
	// Support burst on a popular QaG.
	"support": {
		{name: "support", weight: 40, method: "POST", path: static("/qags/00000000-0000-4000-8000-000000000001/support"), auth: true},
		{name: "unsupport", weight: 30, method: "DELETE", path: static("/qags/00000000-0000-4000-8000-000000000001/support"), auth: true},
		{name: "details", weight: 30, method: "GET", path: static("/qags/00000000-0000-4000-8000-000000000001"), auth: true},
	},
	// Routes of the slices already ported (S0, S1): early Kotlin/Go comparison.
	"ported": {
		{name: "thematiques", weight: 30, method: "GET", path: static("/thematiques")},
		{name: "theme-hebdo", weight: 20, method: "GET", path: static("/theme_hebdo")},
		{name: "referentiels", weight: 10, method: "GET", path: static("/referentiels/regions-et-departements")},
		{name: "profile", weight: 40, method: "GET", path: static("/profile"), auth: true},
	},
	// Public web pages (SSR).
	"web": {
		{name: "home", weight: 25, method: "GET", path: static("/content/page-site-vitrine-accueil")},
		{name: "public-qag", weight: 20, method: "GET", path: static("/api/public/qags/00000000-0000-4000-8000-0000000000a1")},
		{name: "fiches", weight: 15, method: "GET", path: static("/fiches_inventaire")},
		{name: "responses", weight: 20, method: "GET", path: static("/qags/responses/1")},
		{name: "charter", weight: 10, method: "GET", path: static("/participation_charter")},
		{name: "referentiels", weight: 10, method: "GET", path: static("/referentiels/regions-et-departements")},
	},
}

type stat struct {
	mu        sync.Mutex
	latencies []time.Duration
	codes     map[int]int
	errors    int
}

func main() {
	target := flag.String("target", "http://localhost:8082", "base URL")
	profile := flag.String("profile", "app-open", "traffic profile")
	conc := flag.Int("c", 100, "concurrent virtual users")
	dur := flag.Duration("d", 30*time.Second, "duration")
	warmup := flag.Duration("warmup", 5*time.Second, "warmup (not measured)")
	users := flag.Int("users", 1000, "distinct bulk users (seed scale ≥ users/40)")
	secret := flag.String("jwt-secret", os.Getenv("JWT_SECRET"), "JWT secret (base64)")
	report := flag.String("report", "", "JSON report path")
	flag.Parse()

	reqs, ok := profiles[*profile]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown profile", *profile)
		os.Exit(2)
	}
	total := 0
	for _, r := range reqs {
		total += r.weight
	}
	jwt := auth.NewJWT(*secret, nil)
	tokens := make([]string, *users)
	for i := range tokens {
		tokens[i], _, _ = jwt.Generate(bulkUser(i + 1))
	}
	tr := &http.Transport{
		MaxIdleConns:        *conc * 2,
		MaxIdleConnsPerHost: *conc * 2,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	stats := map[string]*stat{}
	for _, r := range reqs {
		stats[r.name] = &stat{codes: map[int]int{}}
	}
	var measuring atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), *warmup+*dur)
	defer cancel()
	time.AfterFunc(*warmup, func() { measuring.Store(true) })
	var wg sync.WaitGroup
	for vu := 0; vu < *conc; vu++ {
		wg.Add(1)
		go func(vu int) {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(int64(vu) + 1))
			for ctx.Err() == nil {
				pick := rnd.Intn(total)
				var rq request
				for _, r := range reqs {
					if pick < r.weight {
						rq = r
						break
					}
					pick -= r.weight
				}
				u := rnd.Intn(*users)
				var body io.Reader
				if rq.body != nil {
					body = strings.NewReader(rq.body(rnd, u))
				}
				req, _ := http.NewRequestWithContext(ctx, rq.method, *target+rq.path(rnd, u), body)
				req.Header.Set("User-Agent", "agora-loadtest")
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				for k := range rq.headers {
					if k == "X-Forwarded-For" {
						req.Header.Set(k, fmt.Sprintf("10.%d.%d.%d", u>>16&255, u>>8&255, u&255))
					}
				}
				if rq.auth {
					req.Header.Set("Authorization", "Bearer "+tokens[u])
				}
				start := time.Now()
				resp, err := client.Do(req)
				lat := time.Since(start)
				code := 0
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					code = resp.StatusCode
				}
				if !measuring.Load() || ctx.Err() != nil {
					continue
				}
				s := stats[rq.name]
				s.mu.Lock()
				if err != nil {
					s.errors++
				} else {
					s.codes[code]++
					s.latencies = append(s.latencies, lat)
				}
				s.mu.Unlock()
			}
		}(vu)
	}
	wg.Wait()

	type line struct {
		Name     string      `json:"name"`
		Count    int         `json:"count"`
		RPS      float64     `json:"rps"`
		P50      float64     `json:"p50ms"`
		P90      float64     `json:"p90ms"`
		P99      float64     `json:"p99ms"`
		Max      float64     `json:"maxms"`
		Errors   int         `json:"errors"`
		Statuses map[int]int `json:"statuses"`
	}
	var lines []line
	var all []time.Duration
	totalErrors, non2xx := 0, 0
	for _, r := range reqs {
		s := stats[r.name]
		sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
		all = append(all, s.latencies...)
		pct := func(p float64) float64 {
			if len(s.latencies) == 0 {
				return 0
			}
			return float64(s.latencies[int(p*float64(len(s.latencies)-1))].Microseconds()) / 1000
		}
		for c, n := range s.codes {
			if c >= 400 {
				non2xx += n
			}
		}
		totalErrors += s.errors
		lines = append(lines, line{r.name, len(s.latencies), float64(len(s.latencies)) / dur.Seconds(), pct(.5), pct(.9), pct(.99), pct(1), s.errors, s.codes})
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	p := func(q float64) float64 {
		if len(all) == 0 {
			return 0
		}
		return float64(all[int(q*float64(len(all)-1))].Microseconds()) / 1000
	}
	fmt.Printf("%-18s %8s %9s %8s %8s %8s %8s %6s  %s\n", "endpoint", "count", "rps", "p50ms", "p90ms", "p99ms", "maxms", "errs", "statuses")
	for _, l := range lines {
		fmt.Printf("%-18s %8d %9.1f %8.1f %8.1f %8.1f %8.1f %6d  %v\n", l.Name, l.Count, l.RPS, l.P50, l.P90, l.P99, l.Max, l.Errors, l.Statuses)
	}
	fmt.Printf("TOTAL %d req, %.1f req/s, p50 %.1fms p90 %.1fms p99 %.1fms, transport errors %d, 4xx/5xx %d\n",
		len(all), float64(len(all))/dur.Seconds(), p(.5), p(.9), p(.99), totalErrors, non2xx)
	if *report != "" {
		b, _ := json.MarshalIndent(map[string]any{"profile": *profile, "concurrency": *conc, "durationS": dur.Seconds(),
			"rps": float64(len(all)) / dur.Seconds(), "p50ms": p(.5), "p90ms": p(.9), "p99ms": p(.99), "errors": totalErrors, "non2xx": non2xx, "endpoints": lines}, "", "  ")
		_ = os.WriteFile(*report, b, 0o644)
	}
}
