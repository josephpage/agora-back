package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Side is one backend under test.
type Side struct {
	Name          string
	BaseURL       string
	DBURL         string
	RedisAddr     string
	RedisPassword string
	StrapiControl string   // fake Strapi base URL (control endpoints)
	Command       []string // argv prefix used to run custom commands
	// GoCache marks the Go side (L1 flush through pub/sub after FLUSHALL).
	GoCache bool

	client *http.Client
	vars   map[string]string
}

// Response is a captured HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	Err    error
}

func (s *Side) init() {
	if s.client == nil {
		s.client = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 32,
				DisableCompression:  true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if s.vars == nil {
		s.vars = map[string]string{}
	}
}

// buildRequest materializes a step for this side.
func (s *Side) buildRequest(st Step, vars map[string]string, mint func(string) string) (*http.Request, error) {
	method := st.Method
	if method == "" {
		method = "GET"
	}
	path := expand(st.Path, vars)
	if len(st.Query) > 0 {
		q := url.Values{}
		keys := make([]string, 0, len(st.Query))
		for k := range st.Query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			q.Add(k, expand(st.Query[k], vars))
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + q.Encode()
	}
	var body io.Reader
	contentType := st.ContentType
	if st.BodyRaw != nil {
		body = strings.NewReader(expand(*st.BodyRaw, vars))
	} else if st.Body != nil {
		b, err := json.Marshal(expandAny(normalizeYAML(st.Body), vars))
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
		if contentType == "" {
			contentType = "application/json"
		}
	}
	req, err := http.NewRequest(method, s.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	// raw path preserved (firewall tests)
	if u, err := url.Parse(s.BaseURL + path); err == nil {
		req.URL = u
	}
	if contentType != "" && contentType != "none" {
		req.Header.Set("Content-Type", contentType)
	}
	hasUA := false
	for k, v := range st.Headers {
		if strings.EqualFold(k, "User-Agent") {
			hasUA = true
		}
		req.Header[k] = append(req.Header[k], expand(v, vars))
	}
	if !hasUA {
		req.Header.Set("User-Agent", "parity-runner")
	}
	if st.As != "" {
		req.Header.Set("Authorization", "Bearer "+mint(expand(st.As, vars)))
	}
	if st.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+expand(st.Bearer, vars))
	}
	return req, nil
}

// normalizeYAML converts map[string]interface{} trees decoded by yaml.v3.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range t {
			out[k] = normalizeYAML(e)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, e := range t {
			out[fmt.Sprint(k)] = normalizeYAML(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeYAML(e)
		}
		return out
	}
	return v
}

func (s *Side) do(req *http.Request) Response {
	resp, err := s.client.Do(req)
	if err != nil {
		return Response{Err: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: b, Err: err}
}

// runCommand runs a custom command (cron task) on this side.
func (s *Side) runCommand(ctx context.Context, a *AdminAction) (string, error) {
	args := append([]string(nil), s.Command[1:]...)
	args = append(args, "--run-custom-command="+a.Command)
	keys := make([]string, 0, len(a.Args))
	for k := range a.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--"+k+"="+a.Args[k])
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.Command[0], args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// parallelDo runs n identical requests concurrently and returns statuses.
func (s *Side) parallelDo(n int, build func() (*http.Request, error)) ([]int, error) {
	var wg sync.WaitGroup
	statuses := make([]int, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func(i int, req *http.Request) {
			defer wg.Done()
			<-start
			r := s.do(req)
			statuses[i], errs[i] = r.Status, r.Err
		}(i, req)
	}
	close(start)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	sort.Ints(statuses)
	return statuses, nil
}

// jsonPathGet extracts a value with a minimal JSON path ($.a.b[0].c).
func jsonPathGet(body []byte, path string) (string, bool) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	p := strings.TrimPrefix(path, "$")
	for p != "" {
		switch {
		case strings.HasPrefix(p, "."):
			p = p[1:]
			end := strings.IndexAny(p, ".[")
			key := p
			if end >= 0 {
				key, p = p[:end], p[end:]
			} else {
				p = ""
			}
			m, ok := v.(map[string]any)
			if !ok {
				return "", false
			}
			v, ok = m[key]
			if !ok {
				return "", false
			}
		case strings.HasPrefix(p, "["):
			end := strings.IndexByte(p, ']')
			if end < 0 {
				return "", false
			}
			var idx int
			fmt.Sscanf(p[1:end], "%d", &idx)
			p = p[end+1:]
			a, ok := v.([]any)
			if !ok || idx >= len(a) {
				return "", false
			}
			v = a[idx]
		default:
			return "", false
		}
	}
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case nil:
		return "null", true
	default:
		b, _ := json.Marshal(t)
		return string(b), true
	}
}
