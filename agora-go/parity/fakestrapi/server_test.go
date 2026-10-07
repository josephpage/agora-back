package fakestrapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var refNow = time.Date(2026, 10, 6, 21, 52, 47, 0, time.UTC)

func writeFixtures(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := writeFixtures(t, map[string]string{
		"things.json": `[
		  {"id":1,"documentId":"t1","name":"alpha","when":"{{now-1d}}","day":"{{date:now+3d}}","publishedAt":"2024-01-01T00:00:00.000Z","n":1},
		  {"id":2,"documentId":"t2","name":"beta","when":"{{now+1d}}","day":"{{date:now}}","publishedAt":"2024-01-01T00:00:00.000Z","n":2},
		  {"id":3,"documentId":"t3","name":"gamma","when":"{{now-10d}}","day":"{{date:now}}","publishedAt":null,"n":3}
		]`,
		"home.json":             `{"id":9,"documentId":"h1","title":"Accueil","subtitle":"sub","body":[{"type":"paragraph","children":[{"type":"text","text":"x"}]}],"publishedAt":"2024-01-01T00:00:00.000Z"}`,
		"unpublished-page.json": `{"id":10,"documentId":"h2","title":"Draft only","publishedAt":null}`,
	})
	s, err := New(dir, refNow)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(t *testing.T, h http.Handler, method, target string, body string, hdr map[string]string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return rec, string(b)
}

func TestCollectionResponse(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/things?pagination[pageSize]=100&populate=*", "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type %q", ct)
	}
	var out struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Pagination map[string]int `json:"pagination"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if len(out.Data) != 2 { // t3 is a draft
		t.Fatalf("len(data)=%d: %s", len(out.Data), body)
	}
	want := map[string]int{"page": 1, "pageSize": 100, "pageCount": 1, "total": 2}
	for k, v := range want {
		if out.Meta.Pagination[k] != v {
			t.Errorf("pagination[%s]=%d want %d", k, out.Meta.Pagination[k], v)
		}
	}
	// templates were resolved against the reference time
	if out.Data[0]["when"] != "2026-10-05T21:52:47.000Z" || out.Data[0]["day"] != "2026-10-09" {
		t.Errorf("templating: %v", out.Data[0])
	}
	// key order of the fixture is preserved
	if !strings.HasPrefix(body, `{"data":[{"id":1,"documentId":"t1","name":"alpha"`) {
		t.Errorf("body does not preserve key order: %s", body)
	}
}

func TestDraftStatus(t *testing.T) {
	s := newTestServer(t)
	_, body := do(t, s, "GET", "/api/things?status=draft&pagination[pageSize]=100", "", nil)
	if !strings.Contains(body, `"t3"`) || !strings.Contains(body, `"total":3`) {
		t.Errorf("draft listing: %s", body)
	}
	_, body = do(t, s, "GET", "/api/things?filters[documentId][$in]=t3", "", nil)
	if !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"total":0`) || !strings.Contains(body, `"pageCount":0`) {
		t.Errorf("unpublished doc leaked: %s", body)
	}
}

func TestFiltersSortAndDatesOverHTTP(t *testing.T) {
	s := newTestServer(t)
	_, body := do(t, s, "GET", "/api/things?pagination[pageSize]=100&filters[when][$lt]=2026-10-06T21:52:47.123456&sort[0]=when:desc", "", nil)
	if !strings.Contains(body, `"t1"`) || strings.Contains(body, `"t2"`) {
		t.Errorf("$lt: %s", body)
	}
	_, body = do(t, s, "GET", "/api/things?sort[0]=name:desc&filters[name][$containsi]=A", "", nil)
	if strings.Index(body, `"t2"`) > strings.Index(body, `"t1"`) {
		t.Errorf("sort desc: %s", body)
	}
}

func TestSingleType(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/home?populate=*", "", nil)
	if rec.Code != 200 || !strings.HasPrefix(body, `{"data":{"id":9,"documentId":"h1"`) || !strings.HasSuffix(body, `,"meta":{}}`) {
		t.Errorf("single type: %d %s", rec.Code, body)
	}
	rec, _ = do(t, s, "GET", "/api/unpublished-page", "", nil)
	if rec.Code != 404 {
		t.Errorf("unpublished single type = %d, want 404", rec.Code)
	}
	rec, _ = do(t, s, "GET", "/api/unpublished-page?status=draft", "", nil)
	if rec.Code != 200 {
		t.Errorf("draft single type = %d, want 200", rec.Code)
	}
}

func TestByDocumentID(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/things/t2", "", nil)
	if rec.Code != 200 || !strings.Contains(body, `"documentId":"t2"`) || !strings.HasSuffix(body, `"meta":{}}`) {
		t.Errorf("%d %s", rec.Code, body)
	}
	if rec, _ = do(t, s, "GET", "/api/things/t3", "", nil); rec.Code != 404 {
		t.Errorf("draft by id = %d", rec.Code)
	}
}

func TestUnknownModel404(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/nope?x=1", "", nil)
	want := `{"data":null,"error":{"status":404,"name":"NotFoundError","message":"Not Found","details":{}}}`
	if rec.Code != 404 || body != want {
		t.Errorf("%d %s", rec.Code, body)
	}
	if rec, _ = do(t, s, "GET", "/elsewhere", "", nil); rec.Code != 404 {
		t.Errorf("non-api path = %d", rec.Code)
	}
	if rec, _ = do(t, s, "POST", "/api/things", "{}", nil); rec.Code != 405 {
		t.Errorf("POST = %d", rec.Code)
	}
}

func TestInvalidQueryIs400(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/things?filters[name][$wat]=1", "", nil)
	if rec.Code != 400 || !strings.Contains(body, `"ValidationError"`) {
		t.Errorf("%d %s", rec.Code, body)
	}
}

type logEntry struct {
	Method        string `json:"method"`
	URI           string `json:"uri"`
	Authorization string `json:"authorization"`
	Time          string `json:"time"`
	Status        int    `json:"status"`
}

func getLog(t *testing.T, s *Server) []logEntry {
	t.Helper()
	rec, body := do(t, s, "GET", "/__control/requests", "", nil)
	if rec.Code != 200 {
		t.Fatalf("log status %d", rec.Code)
	}
	var entries []logEntry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return entries
}

func TestRequestLog(t *testing.T) {
	s := newTestServer(t)
	if got := getLog(t, s); got == nil || len(got) != 0 {
		t.Fatalf("empty log should be [] got %v", got)
	}
	raw := "/api/things?pagination[pageSize]=100&populate=*&filters[name][$containsi]=a%20b+c&sort[0]=name:asc"
	do(t, s, "GET", raw, "", map[string]string{"Authorization": "Bearer parity-token"})
	do(t, s, "GET", "/api/missing", "", nil)

	entries := getLog(t, s)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v (control calls must not be logged)", entries)
	}
	if entries[0].URI != raw || entries[0].Method != "GET" || entries[0].Authorization != "Bearer parity-token" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if _, err := time.Parse(time.RFC3339Nano, entries[0].Time); err != nil {
		t.Errorf("time %q: %v", entries[0].Time, err)
	}
	if entries[0].Status != 200 || entries[1].Status != 404 || entries[1].Authorization != "" {
		t.Errorf("statuses = %+v", entries)
	}

	if rec, _ := do(t, s, "DELETE", "/__control/requests", "", nil); rec.Code != 200 {
		t.Errorf("DELETE = %d", rec.Code)
	}
	if got := getLog(t, s); len(got) != 0 {
		t.Errorf("log not cleared: %v", got)
	}
}

func setFault(t *testing.T, s *Server, body string) {
	t.Helper()
	if rec, out := do(t, s, "POST", "/__control/fault", body, nil); rec.Code != 200 {
		t.Fatalf("fault %s => %d %s", body, rec.Code, out)
	}
}

func TestFaults(t *testing.T) {
	s := newTestServer(t)

	setFault(t, s, `{"model":"things","mode":"500"}`)
	rec, body := do(t, s, "GET", "/api/things", "", nil)
	if rec.Code != 500 || !strings.Contains(body, `"InternalServerError"`) {
		t.Errorf("500: %d %s", rec.Code, body)
	}
	if rec, _ = do(t, s, "GET", "/api/home", "", nil); rec.Code != 200 {
		t.Errorf("fault must be scoped to its model, got %d", rec.Code)
	}

	setFault(t, s, `{"model":"things","mode":"malformed"}`)
	rec, body = do(t, s, "GET", "/api/things", "", nil)
	var tmp any
	if rec.Code != 200 || json.Unmarshal([]byte(body), &tmp) == nil {
		t.Errorf("malformed: %d valid=%v %s", rec.Code, json.Unmarshal([]byte(body), &tmp) == nil, body)
	}

	setFault(t, s, `{"model":"things","mode":"nulldata"}`)
	rec, body = do(t, s, "GET", "/api/things", "", nil)
	if rec.Code != 200 || body != `{"data":null,"meta":{}}` {
		t.Errorf("nulldata: %d %s", rec.Code, body)
	}

	setFault(t, s, `{"model":"things","mode":"missingfield"}`)
	_, body = do(t, s, "GET", "/api/things?pagination[pageSize]=100", "", nil)
	if strings.Contains(body, `"name":"alpha"`) || !strings.Contains(body, `"name":"beta"`) {
		t.Errorf("missingfield should drop the first string field of the first element only: %s", body)
	}
	setFault(t, s, `{"model":"home","mode":"missingfield"}`)
	_, body = do(t, s, "GET", "/api/home", "", nil)
	if strings.Contains(body, `"title"`) || !strings.Contains(body, `"subtitle"`) {
		t.Errorf("missingfield single type: %s", body)
	}

	setFault(t, s, `{"model":"things","mode":"slow","delayMs":150}`)
	start := time.Now()
	rec, _ = do(t, s, "GET", "/api/things", "", nil)
	if rec.Code != 200 || time.Since(start) < 140*time.Millisecond {
		t.Errorf("slow: %d after %v", rec.Code, time.Since(start))
	}

	setFault(t, s, `{"model":"things","mode":"none"}`)
	setFault(t, s, `{"model":"home","mode":"none"}`)
	setFault(t, s, `{"model":"*","mode":"500"}`)
	if rec, _ = do(t, s, "GET", "/api/home", "", nil); rec.Code != 500 {
		t.Errorf("wildcard fault: %d", rec.Code)
	}
	if rec, _ = do(t, s, "POST", "/__control/reset", "", nil); rec.Code != 200 {
		t.Fatalf("reset = %d", rec.Code)
	}
	if rec, _ = do(t, s, "GET", "/api/home", "", nil); rec.Code != 200 {
		t.Errorf("after reset: %d", rec.Code)
	}
	if got := getLog(t, s); len(got) != 1 { // only the request issued after the reset
		t.Errorf("reset did not clear the log: %d entries", len(got))
	}
	if rec, _ = do(t, s, "POST", "/__control/fault", `{"model":"x","mode":"explode"}`, nil); rec.Code != 400 {
		t.Errorf("bad mode = %d", rec.Code)
	}
}

func TestOverrideAndReset(t *testing.T) {
	s := newTestServer(t)
	rec, out := do(t, s, "POST", "/__control/override", `{"model":"things","data":[{"documentId":"z","publishedAt":"x","at":"{{now+1h}}"}]}`, nil)
	if rec.Code != 200 {
		t.Fatalf("override: %d %s", rec.Code, out)
	}
	_, body := do(t, s, "GET", "/api/things", "", nil)
	if !strings.Contains(body, `"documentId":"z"`) || !strings.Contains(body, `"at":"2026-10-06T22:52:47.000Z"`) || !strings.Contains(body, `"total":1`) {
		t.Errorf("override not applied: %s", body)
	}
	do(t, s, "POST", "/__control/reset", "", nil)
	_, body = do(t, s, "GET", "/api/things", "", nil)
	if !strings.Contains(body, `"total":2`) {
		t.Errorf("reset did not restore: %s", body)
	}
	if rec, _ = do(t, s, "POST", "/__control/override", `{"model":"things","fixture":"../etc/passwd"}`, nil); rec.Code != 400 {
		t.Errorf("path traversal accepted: %d", rec.Code)
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing"), refNow); err == nil {
		t.Error("missing dir accepted")
	}
	if _, err := New(t.TempDir(), refNow); err == nil {
		t.Error("empty dir accepted")
	}
	if _, err := New(writeFixtures(t, map[string]string{"a.json": `[1]`}), refNow); err == nil {
		t.Error("non-object element accepted")
	}
	if _, err := New(writeFixtures(t, map[string]string{"a.json": `[{"x":"{{bogus}}"}]`}), refNow); err == nil {
		t.Error("unknown template accepted")
	}
	if _, err := New(writeFixtures(t, map[string]string{"a.json": `{"x":`}), refNow); err == nil {
		t.Error("invalid JSON accepted")
	}
}

func TestMalformedQueryStringIs400(t *testing.T) {
	s := newTestServer(t)
	rec, body := do(t, s, "GET", "/api/things?filters[name][$in]=%zz", "", nil)
	if rec.Code != 400 || !strings.Contains(body, "ValidationError") {
		t.Errorf("%d %s", rec.Code, body)
	}
}
