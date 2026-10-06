package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"agora/internal/auth"
)

// Runner executes scenarios against both sides.
type Runner struct {
	Ref, Go  *Side
	JWT      *auth.JWT
	Tokens   *auth.LoginTokens
	SeedVars map[string]string
	Now      time.Time
	Verbose  bool
	// StrapiCheck compares the URIs sent to the fake Strapi (go ⊆ ref).
	StrapiCheck bool
}

// StepResult is the outcome of one step.
type StepResult struct {
	ID        string `json:"id"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	RefStatus int    `json:"refStatus"`
	GoStatus  int    `json:"goStatus"`
	Diffs     []Diff `json:"diffs,omitempty"`
}

// ScenarioResult is the outcome of one scenario.
type ScenarioResult struct {
	Name    string       `json:"name"`
	File    string       `json:"file"`
	Steps   []StepResult `json:"steps"`
	Diffs   []Diff       `json:"diffs,omitempty"` // scenario-level (db, strapi)
	Error   string       `json:"error,omitempty"`
	Skipped string       `json:"skipped,omitempty"`
}

// Failed reports whether the scenario has any difference or error.
func (r *ScenarioResult) Failed() bool {
	if r.Error != "" || len(r.Diffs) > 0 {
		return true
	}
	for _, s := range r.Steps {
		if len(s.Diffs) > 0 {
			return true
		}
	}
	return false
}

var seedConstRe = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)

// LoadSeedVars extracts the string constants of parity/seed/ids.go as
// template variables "seed.<Name>".
func LoadSeedVars(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	for _, m := range seedConstRe.FindAllStringSubmatch(string(b), -1) {
		vars["seed."+m[1]] = m[2]
	}
	return vars, nil
}

func (r *Runner) varsFor(s *Side) map[string]string {
	v := map[string]string{}
	for k, val := range r.SeedVars {
		v[k] = val
	}
	for k, val := range s.vars {
		v[k] = val
	}
	return v
}

func (r *Runner) mint(userID string) string {
	tok, _, err := r.JWT.Generate(userID)
	if err != nil {
		return "invalid"
	}
	return tok
}

// Run executes one scenario.
func (r *Runner) Run(ctx context.Context, sc *Scenario) *ScenarioResult {
	res := &ScenarioResult{Name: sc.Name, File: sc.file}
	if sc.Skip != "" {
		res.Skipped = sc.Skip
		return res
	}
	r.Ref.init()
	r.Go.init()
	r.Ref.vars, r.Go.vars = map[string]string{}, map[string]string{}
	binder := NewBinder(r.Tokens)
	start := time.Now()
	if err := r.resetBoth(ctx, sc.Setup); err != nil {
		res.Error = "setup: " + err.Error()
		return res
	}
	recentAfter := time.Now().Add(-2 * time.Second)
	for i := range sc.Steps {
		st := sc.Steps[i]
		if st.SleepMs > 0 {
			time.Sleep(time.Duration(st.SleepMs) * time.Millisecond)
		}
		sr := r.runStep(ctx, binder, st)
		if st.ID == "" {
			sr.ID = fmt.Sprint(i)
		}
		res.Steps = append(res.Steps, sr)
		if sc.DBDiff == "step" {
			res.Steps[len(res.Steps)-1].Diffs = append(res.Steps[len(res.Steps)-1].Diffs, r.dbDiff(ctx, binder, recentAfter)...)
		}
	}
	if sc.DBDiff == "" || sc.DBDiff == "end" {
		res.Diffs = append(res.Diffs, r.dbDiff(ctx, binder, recentAfter)...)
	}
	if r.StrapiCheck {
		res.Diffs = append(res.Diffs, r.strapiDiff(ctx)...)
	}
	if r.Verbose {
		fmt.Fprintf(os.Stderr, "  %s (%s)\n", sc.Name, time.Since(start).Round(time.Millisecond))
	}
	return res
}

func (r *Runner) resetBoth(ctx context.Context, setup Setup) error {
	var wg sync.WaitGroup
	var e1, e2 error
	wg.Add(2)
	go func() { defer wg.Done(); e1 = r.Ref.reset(ctx, setup, r.Now) }()
	go func() { defer wg.Done(); e2 = r.Go.reset(ctx, setup, r.Now) }()
	wg.Wait()
	if e1 != nil {
		return e1
	}
	return e2
}

func (r *Runner) dbDiff(ctx context.Context, b *Binder, recentAfter time.Time) []Diff {
	var rs, gs map[string][]dbRow
	var e1, e2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); rs, e1 = snapshotDB(ctx, r.Ref.DBURL) }()
	go func() { defer wg.Done(); gs, e2 = snapshotDB(ctx, r.Go.DBURL) }()
	wg.Wait()
	if e1 != nil || e2 != nil {
		return []Diff{{Where: "db", Ref: fmt.Sprint(e1), Go: fmt.Sprint(e2), Note: "snapshot error"}}
	}
	return b.diffDB(rs, gs, recentAfter)
}

var isoInURIRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?`)

func (r *Runner) strapiDiff(ctx context.Context) []Diff {
	ru, err1 := r.Ref.strapiRequests(ctx)
	gu, err2 := r.Go.strapiRequests(ctx)
	if err1 != nil || err2 != nil {
		return nil
	}
	norm := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, u := range l {
			m[isoInURIRe.ReplaceAllString(u, "<datetime>")] = true
		}
		return m
	}
	rm, gm := norm(ru), norm(gu)
	var diffs []Diff
	for u := range gm {
		if !rm[u] {
			diffs = append(diffs, Diff{Where: "strapi", Ref: "(not requested)", Go: u, Note: "URI only requested by Go"})
		}
	}
	for u := range rm {
		if !gm[u] {
			diffs = append(diffs, Diff{Where: "strapi", Ref: u, Go: "(not requested)", Note: "URI only requested by Kotlin (may be a Go cache hit)"})
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Ref+diffs[i].Go < diffs[j].Ref+diffs[j].Go })
	return diffs
}

func (r *Runner) runStep(ctx context.Context, b *Binder, st Step) StepResult {
	sr := StepResult{ID: st.ID, Method: st.Method, Path: st.Path}
	if sr.Method == "" {
		sr.Method = "GET"
	}
	if st.Admin != nil {
		var wg sync.WaitGroup
		var o1, o2 string
		var e1, e2 error
		wg.Add(2)
		go func() { defer wg.Done(); o1, e1 = r.Ref.runCommand(ctx, st.Admin) }()
		go func() { defer wg.Done(); o2, e2 = r.Go.runCommand(ctx, st.Admin) }()
		wg.Wait()
		if (e1 == nil) != (e2 == nil) {
			sr.Diffs = append(sr.Diffs, Diff{Where: "command", Ref: fmt.Sprint(e1, tail(o1)), Go: fmt.Sprint(e2, tail(o2))})
		}
		sr.Method, sr.Path = "CMD", st.Admin.Command
		return sr
	}
	rv, gv := r.varsFor(r.Ref), r.varsFor(r.Go)
	if st.Parallel > 1 {
		rs, err1 := r.Ref.parallelDo(st.Parallel, func() (*http.Request, error) { return r.Ref.buildRequest(st, rv, r.mint) })
		gs, err2 := r.Go.parallelDo(st.Parallel, func() (*http.Request, error) { return r.Go.buildRequest(st, gv, r.mint) })
		if err1 != nil || err2 != nil {
			sr.Diffs = append(sr.Diffs, Diff{Where: "transport", Ref: fmt.Sprint(err1), Go: fmt.Sprint(err2)})
			return sr
		}
		if fmt.Sprint(rs) != fmt.Sprint(gs) {
			sr.Diffs = append(sr.Diffs, Diff{Where: "status(parallel)", Ref: fmt.Sprint(rs), Go: fmt.Sprint(gs)})
		}
		return sr
	}
	repeat := st.Repeat
	if repeat < 1 {
		repeat = 1
	}
	for n := 0; n < repeat; n++ {
		rreq, err1 := r.Ref.buildRequest(st, rv, r.mint)
		greq, err2 := r.Go.buildRequest(st, gv, r.mint)
		if err1 != nil || err2 != nil {
			sr.Diffs = append(sr.Diffs, Diff{Where: "build", Ref: fmt.Sprint(err1), Go: fmt.Sprint(err2)})
			return sr
		}
		var rr, gr Response
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); rr = r.Ref.do(rreq) }()
		go func() { defer wg.Done(); gr = r.Go.do(greq) }()
		wg.Wait()
		sr.RefStatus, sr.GoStatus = rr.Status, gr.Status
		if rr.Err != nil || gr.Err != nil {
			sr.Diffs = append(sr.Diffs, Diff{Where: "transport", Ref: fmt.Sprint(rr.Err), Go: fmt.Sprint(gr.Err)})
			return sr
		}
		if rr.Status != gr.Status {
			sr.Diffs = append(sr.Diffs, Diff{Where: "status", Ref: fmt.Sprint(rr.Status), Go: fmt.Sprint(gr.Status), Note: short(rr.Body) + " ||| " + short(gr.Body)})
		}
		mode := st.Compare.Body
		if mode != "none" && mode != "status" {
			sr.Diffs = append(sr.Diffs, b.compareHeaders(rr.Header, gr.Header, st.Compare.Headers)...)
			if strings.Contains(rr.Header.Get("Content-Type"), "json") || mode == "json" {
				sr.Diffs = append(sr.Diffs, b.compareJSON(rr.Body, gr.Body, st.Compare)...)
			} else {
				sr.Diffs = append(sr.Diffs, b.compareText(rr.Body, gr.Body)...)
			}
		}
		for name, path := range st.Capture {
			if v, ok := jsonPathGet(rr.Body, path); ok {
				r.Ref.vars[name] = v
			}
			if v, ok := jsonPathGet(gr.Body, path); ok {
				r.Go.vars[name] = v
			}
		}
	}
	return sr
}

func tail(s string) string {
	if len(s) > 2000 {
		return s[len(s)-2000:]
	}
	return s
}

// Summary aggregates results.
type Summary struct {
	Total, Passed, Failed, Skipped int
	Results                        []*ScenarioResult
}

// WriteJSON writes the full report.
func (s *Summary) WriteJSON(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
