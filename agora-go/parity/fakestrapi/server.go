// Package fakestrapi is a fake Strapi v5 REST API used by the Kotlin -> Go
// parity harness. It serves JSON fixtures through the small subset of the
// Strapi query language that the Kotlin StrapiRequestBuilder emits, records
// every request it receives, and offers control endpoints to inject faults.
package fakestrapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// model holds the data of one Strapi content type.
type model struct {
	single bool
	docs   []*Object // collection types
	obj    *Object   // single types
}

// RequestLog is one recorded request.
type RequestLog struct {
	Method        string `json:"method"`
	URI           string `json:"uri"`
	Authorization string `json:"authorization"`
	Time          string `json:"time"`
	Status        int    `json:"status"`
}

// Fault describes an injected failure for one model (or "*").
type Fault struct {
	Model   string `json:"model"`
	Mode    string `json:"mode"`
	DelayMs int    `json:"delayMs"`
}

// Fault modes.
const (
	FaultNone         = "none"
	Fault500          = "500"
	FaultMalformed    = "malformed"
	FaultNullData     = "nulldata"
	FaultSlow         = "slow"
	FaultMissingField = "missingfield"
)

const maxLogEntries = 100000

// Server is the fake Strapi. It implements http.Handler.
type Server struct {
	fixturesDir string
	now         time.Time

	mu        sync.RWMutex
	base      map[string]*model // as loaded from the fixtures
	cur       map[string]*model // base + runtime overrides
	faults    map[string]Fault
	log       []*RequestLog
	logStatus sync.Mutex
}

// New loads every `<model>.json` file of fixturesDir and resolves the
// {{now...}} templates against now.
func New(fixturesDir string, now time.Time) (*Server, error) {
	s := &Server{
		fixturesDir: fixturesDir,
		now:         now.UTC(),
		faults:      map[string]Fault{},
	}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Now returns the reference time used for date templating.
func (s *Server) Now() time.Time { return s.now }

// Models lists the loaded model names, sorted.
func (s *Server) Models() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.cur))
	for n := range s.cur {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Reload re-reads the fixtures from disk (dropping runtime overrides).
func (s *Server) Reload() error {
	entries, err := os.ReadDir(s.fixturesDir)
	if err != nil {
		return fmt.Errorf("read fixtures dir: %w", err)
	}
	base := map[string]*model{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(s.fixturesDir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		m, err := s.parseModel(data)
		if err != nil {
			return fmt.Errorf("fixture %s: %w", e.Name(), err)
		}
		base[name] = m
	}
	if len(base) == 0 {
		return fmt.Errorf("no *.json fixtures found in %s", s.fixturesDir)
	}
	s.mu.Lock()
	s.base = base
	s.cur = cloneModels(base)
	s.mu.Unlock()
	return nil
}

func (s *Server) parseModel(data []byte) (*model, error) {
	v, err := ParseJSON(data)
	if err != nil {
		return nil, err
	}
	v, err = applyTemplates(v, s.now)
	if err != nil {
		return nil, err
	}
	switch t := v.(type) {
	case []any:
		m := &model{}
		for i, e := range t {
			o, ok := e.(*Object)
			if !ok {
				return nil, fmt.Errorf("element %d is not a JSON object", i)
			}
			m.docs = append(m.docs, o)
		}
		return m, nil
	case *Object:
		return &model{single: true, obj: t}, nil
	}
	return nil, fmt.Errorf("top-level JSON must be an array (collection) or an object (single type)")
}

func cloneModels(in map[string]*model) map[string]*model {
	out := make(map[string]*model, len(in))
	for k, m := range in {
		c := &model{single: m.single}
		if m.obj != nil {
			c.obj = clone(m.obj).(*Object)
		}
		for _, d := range m.docs {
			c.docs = append(c.docs, clone(d).(*Object))
		}
		out[k] = c
	}
	return out
}

// statusWriter captures the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/__control/") {
		s.serveControl(w, r)
		return
	}

	entry := &RequestLog{
		Method:        r.Method,
		URI:           r.RequestURI,
		Authorization: r.Header.Get("Authorization"),
		Time:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	if entry.URI == "" { // in-process requests built without a RequestURI
		entry.URI = r.URL.RequestURI()
	}
	s.mu.Lock()
	s.log = append(s.log, entry)
	if len(s.log) > maxLogEntries {
		s.log = append([]*RequestLog(nil), s.log[len(s.log)-maxLogEntries:]...)
	}
	s.mu.Unlock()

	sw := &statusWriter{ResponseWriter: w}
	defer func() {
		s.logStatus.Lock()
		entry.Status = sw.status
		s.logStatus.Unlock()
	}()

	s.serveAPI(sw, r)
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
		return
	}
	if r.Method != http.MethodGet {
		writeStrapiError(w, http.StatusMethodNotAllowed, "MethodNotAllowedError", "Method Not Allowed")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/"), "/")
	modelName := parts[0]

	// Fault injection (specific model first, then "*").
	s.mu.RLock()
	fault, hasFault := s.faults[modelName]
	if !hasFault {
		fault, hasFault = s.faults["*"]
	}
	s.mu.RUnlock()
	if hasFault {
		switch fault.Mode {
		case Fault500:
			writeStrapiError(w, http.StatusInternalServerError, "InternalServerError", "Internal Server Error")
			return
		case FaultMalformed:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"data":[{"documentId":"broken","titre":`)
			return
		case FaultNullData:
			writeJSON(w, http.StatusOK, []byte(`{"data":null,"meta":{}}`))
			return
		case FaultSlow:
			select {
			case <-time.After(time.Duration(fault.DelayMs) * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	}

	s.mu.RLock()
	m, ok := s.cur[modelName]
	s.mu.RUnlock()
	if !ok {
		writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
		return
	}

	// url.ParseQuery semantics ('+' and %XX decoded), strict on malformed pairs.
	rawValues, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", "Invalid query string: "+err.Error())
		return
	}
	query, err := ParseQuery(rawValues)
	if err != nil {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return
	}

	var body any
	switch {
	case m.single:
		if len(parts) > 1 {
			writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
			return
		}
		if !query.Draft && !isPublished(m.obj) {
			writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
			return
		}
		env := NewObject()
		env.Set("data", clone(m.obj))
		env.Set("meta", NewObject())
		body = env
	case len(parts) > 1: // GET /api/<model>/<documentId>
		var found *Object
		for _, d := range m.docs {
			if id, _ := d.Get("documentId"); id == parts[1] && (query.Draft || isPublished(d)) {
				found = d
				break
			}
		}
		if found == nil {
			writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
			return
		}
		env := NewObject()
		env.Set("data", clone(found))
		env.Set("meta", NewObject())
		body = env
	default:
		res := query.Apply(m.docs)
		data := make([]any, len(res.Docs))
		for i, d := range res.Docs {
			data[i] = clone(d)
		}
		env := NewObject()
		env.Set("data", data)
		env.Set("meta", query.Meta(res.Total))
		body = env
	}

	if hasFault && fault.Mode == FaultMissingField {
		dropFirstStringField(body.(*Object))
	}

	out, err := Marshal(body)
	if err != nil {
		writeStrapiError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// metadataKeys are never removed by the missingfield fault so that the
// failure hits a content field rather than Strapi bookkeeping.
var metadataKeys = map[string]bool{
	"id": true, "documentId": true, "createdAt": true, "updatedAt": true,
	"publishedAt": true, "locale": true,
}

// dropFirstStringField removes the first (in fixture order) top-level string
// field of the first data element (or of the single-type object).
func dropFirstStringField(env *Object) {
	data, _ := env.Get("data")
	var target *Object
	switch t := data.(type) {
	case *Object:
		target = t
	case []any:
		if len(t) > 0 {
			target, _ = t[0].(*Object)
		}
	}
	if target == nil {
		return
	}
	for _, k := range target.Keys() {
		if metadataKeys[k] {
			continue
		}
		if v, _ := target.Get(k); v != nil {
			if _, isStr := v.(string); isStr {
				target.Delete(k)
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeStrapiError(w http.ResponseWriter, status int, name, message string) {
	errObj := NewObject()
	errObj.Set("status", json.Number(fmt.Sprint(status)))
	errObj.Set("name", name)
	errObj.Set("message", message)
	errObj.Set("details", NewObject())
	env := NewObject()
	env.Set("data", nil)
	env.Set("error", errObj)
	b, _ := Marshal(env)
	writeJSON(w, status, b)
}

// ---------------------------------------------------------------------------
// control endpoints

func (s *Server) serveControl(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/__control/")
	switch {
	case path == "requests" && r.Method == http.MethodGet:
		s.mu.RLock()
		entries := make([]RequestLog, 0, len(s.log))
		s.logStatus.Lock()
		for _, e := range s.log {
			entries = append(entries, *e)
		}
		s.logStatus.Unlock()
		s.mu.RUnlock()
		b, _ := json.Marshal(entries)
		writeJSON(w, http.StatusOK, b)

	case path == "requests" && r.Method == http.MethodDelete:
		s.mu.Lock()
		n := len(s.log)
		s.log = nil
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, []byte(fmt.Sprintf(`{"ok":true,"cleared":%d}`, n)))

	case path == "fault" && r.Method == http.MethodPost:
		var f Fault
		if err := decodeBody(r, &f); err != nil {
			writeStrapiError(w, http.StatusBadRequest, "ValidationError", err.Error())
			return
		}
		if f.Model == "" {
			f.Model = "*"
		}
		switch f.Mode {
		case FaultNone, Fault500, FaultMalformed, FaultNullData, FaultSlow, FaultMissingField:
		default:
			writeStrapiError(w, http.StatusBadRequest, "ValidationError", fmt.Sprintf("unknown fault mode %q", f.Mode))
			return
		}
		s.mu.Lock()
		if f.Mode == FaultNone {
			delete(s.faults, f.Model)
		} else {
			s.faults[f.Model] = f
		}
		faults := make([]Fault, 0, len(s.faults))
		for _, v := range s.faults {
			faults = append(faults, v)
		}
		s.mu.Unlock()
		sort.Slice(faults, func(i, j int) bool { return faults[i].Model < faults[j].Model })
		b, _ := json.Marshal(map[string]any{"ok": true, "faults": faults})
		writeJSON(w, http.StatusOK, b)

	case path == "reset" && r.Method == http.MethodPost:
		s.mu.Lock()
		s.faults = map[string]Fault{}
		s.log = nil
		s.cur = cloneModels(s.base)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))

	case path == "reload" && r.Method == http.MethodPost:
		if err := s.Reload(); err != nil {
			writeStrapiError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))

	case path == "override" && r.Method == http.MethodPost:
		s.serveOverride(w, r)

	default:
		writeStrapiError(w, http.StatusNotFound, "NotFoundError", "Not Found")
	}
}

// serveOverride replaces one model's data at runtime. Body:
//
//	{"model":"theme-hebdos","data":[...]}            inline data, or
//	{"model":"theme-hebdos","fixture":"variants/x.json"}  a file under the fixtures dir.
//
// {{now...}} templates are resolved. POST /__control/reset restores the
// fixtures as loaded.
func (s *Server) serveOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model   string          `json:"model"`
		Data    json.RawMessage `json:"data"`
		Fixture string          `json:"fixture"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return
	}
	if req.Model == "" {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", "model is required")
		return
	}
	data := []byte(req.Data)
	if req.Fixture != "" {
		clean := filepath.Clean(req.Fixture)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			writeStrapiError(w, http.StatusBadRequest, "ValidationError", "fixture must be a relative path inside the fixtures dir")
			return
		}
		b, err := os.ReadFile(filepath.Join(s.fixturesDir, clean))
		if err != nil {
			writeStrapiError(w, http.StatusBadRequest, "ValidationError", err.Error())
			return
		}
		data = b
	}
	if len(bytes.TrimSpace(data)) == 0 {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", "data or fixture is required")
		return
	}
	m, err := s.parseModel(data)
	if err != nil {
		writeStrapiError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return
	}
	s.mu.Lock()
	s.cur[req.Model] = m
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}

func decodeBody(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("empty request body")
	}
	return json.Unmarshal(body, v)
}
