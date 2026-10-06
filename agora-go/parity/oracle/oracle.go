// Package oracle is the Go client of the "JVM oracle": a long-running Java
// process (parity/oracle/java) that answers function calls with the REAL
// Kotlin/JVM implementations of the reference jar, so that Go code can be
// fuzz-compared against them.
//
// # Enabling
//
// Everything is opt-in: Available reports false (and MustCall skips the test)
// unless PARITY_ORACLE=1. When enabled, the Java sources are compiled on first
// use with build.sh (into parity/oracle/.build) if the build is missing or
// stale, then run.sh starts one JVM per test process.
//
//	PARITY_ORACLE=1 go test ./parity/oracle/ -v -count=1
//
// # Environment
//
// The Kotlin code under test reads its configuration with System.getenv
// (LOGIN_TOKEN_*, JWT_SECRET, REMOTE_ADDRESS_*). The child JVM receives the Go
// process environment; in addition the KEY=VALUE lines of
// parity/env/common.env (override the path with PARITY_ORACLE_ENV_FILE) are
// loaded as defaults for the variables the process environment does not
// define. Single calls can override variables with CallEnv.
//
// Other knobs: REFJAR_DIR (extracted reference jar), ORACLE_JAVA_HOME (JDK 17),
// ORACLE_JAVA_OPTS, PARITY_ORACLE_DIR (location of this directory when it
// cannot be derived), PARITY_ORACLE_TIMEOUT (per-call timeout, default 30s),
// PARITY_ORACLE_VERBOSE=1 (forward the JVM stderr).
//
// # Protocol
//
// One JSON object per line on the JVM stdin: {"id":N,"fn":"name","args":{...}}
// (optionally "env":{"VAR":"value","OTHER":null}); one line per request on
// stdout: {"id":N,"ok":true,"result":...} or
// {"id":N,"ok":false,"error":"SimpleClassName: message", ...}. Requests may be
// pipelined by concurrent callers; the JVM answers them in order.
//
// # Strings with lone surrogates
//
// Go strings cannot hold lone UTF-16 surrogates, and encoding/json replaces
// them with U+FFFD. To send one use the Utf16 type as an argument; functions
// that can return one answer {"text":..., "utf16":[...]} (see Text), and any
// string-returning function does so too when the arguments carry
// "utf16": true. Doubles that JSON cannot carry exactly (-0, NaN, +-Inf) are
// sent with F64.
package oracle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
)

// Timeout is the maximum duration of one Call. When it expires the JVM is
// killed (it may be stuck, e.g. in a catastrophic regex) and restarted by the
// next call. PARITY_ORACLE_TIMEOUT (a Go duration) overrides the default.
var Timeout = 30 * time.Second

// ErrDisabled is returned by Call when PARITY_ORACLE=1 is not set.
var ErrDisabled = errors.New("oracle: disabled (set PARITY_ORACLE=1)")

// OracleError is returned by Call when the JVM answered {"ok":false}.
type OracleError struct {
	// Msg is "<exception simple class name>: <message>".
	Msg string
	// Exception is the fully qualified class name of the exception.
	Exception string
	// Original is Jackson's message without the source location, when the
	// exception is a Jackson JsonProcessingException.
	Original string
}

func (e *OracleError) Error() string { return e.Msg }

// Class is the exception simple class name ("IllegalArgumentException").
func (e *OracleError) Class() string {
	c, _, _ := strings.Cut(e.Msg, ": ")
	return c
}

// Message is the exception message without the class name.
func (e *OracleError) Message() string {
	_, m, _ := strings.Cut(e.Msg, ": ")
	return m
}

// Text is the result shape of the functions that may return lone surrogates:
// the (lossy for lone surrogates) text and its exact UTF-16 code units.
type Text struct {
	Text  string   `json:"text"`
	Utf16 []uint16 `json:"utf16"`
}

// Units returns the exact UTF-16 code units of a Go string (invalid UTF-8
// becomes U+FFFD, as everywhere in Go), for comparing with Text.Utf16.
func Units(s string) []uint16 { return utf16.Encode([]rune(s)) }

// Utf16 is a string given as UTF-16 code units. It is marshalled as a JSON
// string made of \uXXXX escapes, so lone surrogates reach the JVM intact.
type Utf16 []uint16

// MarshalJSON implements json.Marshaler.
func (u Utf16) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.Grow(2 + 6*len(u))
	b.WriteByte('"')
	for _, c := range u {
		fmt.Fprintf(&b, `\u%04x`, c)
	}
	b.WriteByte('"')
	return b.Bytes(), nil
}

// F64 encodes a float64 as the exact raw bits ({"bits":N}), which doubleToString,
// mathRound and numberFormat accept in place of {"d":...}. Use it for -0, NaN,
// infinities and whenever exactness matters.
func F64(d float64) map[string]any { return map[string]any{"bits": math.Float64bits(d)} }

// ---------------------------------------------------------------------------
// Availability, build

var (
	prepOnce  sync.Once
	prepErr   error
	oracleDir string
)

func enabled() bool { return os.Getenv("PARITY_ORACLE") == "1" }

// Available reports whether the oracle can be used: PARITY_ORACLE=1 is set
// and the Java build exists (it is built on first use if needed). Use
// UnavailableReason to know why it is not.
func Available() bool {
	return enabled() && prepare() == nil
}

// UnavailableReason explains why Available is false (nil when it is true).
func UnavailableReason() error {
	if !enabled() {
		return ErrDisabled
	}
	return prepare()
}

func prepare() error {
	prepOnce.Do(func() {
		dir, err := locateDir()
		if err != nil {
			prepErr = err
			return
		}
		oracleDir = dir
		if exec.Command("bash", filepath.Join(dir, "build.sh"), "--check").Run() == nil {
			return
		}
		cmd := exec.Command("bash", filepath.Join(dir, "build.sh"))
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			prepErr = fmt.Errorf("oracle: build.sh failed: %w\n%s", err, out.String())
		}
	})
	return prepErr
}

func locateDir() (string, error) {
	isDir := func(d string) bool {
		st, err := os.Stat(filepath.Join(d, "build.sh"))
		return err == nil && !st.IsDir()
	}
	if d := os.Getenv("PARITY_ORACLE_DIR"); d != "" {
		if !isDir(d) {
			return "", fmt.Errorf("oracle: PARITY_ORACLE_DIR=%q has no build.sh", d)
		}
		return filepath.Abs(d)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		if d := filepath.Dir(file); isDir(d) {
			return d, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		for d := wd; ; d = filepath.Dir(d) {
			if c := filepath.Join(d, "parity", "oracle"); isDir(c) {
				return c, nil
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return "", errors.New("oracle: cannot locate parity/oracle (set PARITY_ORACLE_DIR)")
}

// ---------------------------------------------------------------------------
// Child process environment

// parseEnvFile reads KEY=VALUE lines (blank lines and # comments ignored, an
// optional "export " prefix and one pair of surrounding quotes are stripped).
func parseEnvFile(path string) (map[string]string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	vars := map[string]string{}
	var order []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, dup := vars[k]; !dup {
			order = append(order, k)
		}
		vars[k] = v
	}
	return vars, order, nil
}

func childEnv() []string {
	env := os.Environ()
	path := os.Getenv("PARITY_ORACLE_ENV_FILE")
	if path == "" {
		path = filepath.Join(oracleDir, "..", "env", "common.env")
	}
	vars, order, err := parseEnvFile(path)
	if err != nil {
		return env
	}
	for _, k := range order {
		if _, set := os.LookupEnv(k); !set {
			env = append(env, k+"="+vars[k])
		}
	}
	return env
}

// ---------------------------------------------------------------------------
// JVM process

type request struct {
	ID   uint64         `json:"id"`
	Fn   string         `json:"fn"`
	Args any            `json:"args"`
	Env  map[string]any `json:"env,omitempty"`
}

type response struct {
	ID        uint64          `json:"id"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error"`
	Exception string          `json:"exception"`
	Original  string          `json:"originalMessage"`
}

type outcome struct {
	resp *response
	err  error
}

type proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tail
	done   chan struct{} // closed when the stdout reader ended

	wmu sync.Mutex // serialises stdin writes

	mu      sync.Mutex // guards pending, closed, exitErr
	pending map[uint64]chan outcome
	closed  bool
	exitErr error
}

var (
	curMu  sync.Mutex
	cur    *proc
	nextID atomic.Uint64
)

// tail keeps the last bytes written to it (the JVM stderr).
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailMax = 16 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailMax:]...)
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

func startProc() (*proc, error) {
	cmd := exec.Command("bash", filepath.Join(oracleDir, "run.sh"))
	cmd.Dir = oracleDir
	cmd.Env = childEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	p := &proc{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  &tail{},
		done:    make(chan struct{}),
		pending: map[uint64]chan outcome{},
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("oracle: starting JVM: %w", err)
	}
	var sink io.Writer = p.stderr
	if os.Getenv("PARITY_ORACLE_VERBOSE") == "1" {
		sink = io.MultiWriter(p.stderr, os.Stderr)
	}
	errDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(sink, stderr)
		close(errDone)
	}()
	go p.readLoop(stdout, errDone)
	return p, nil
}

func (p *proc) readLoop(stdout io.Reader, errDone <-chan struct{}) {
	r := bufio.NewReaderSize(stdout, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var resp response
			if jerr := json.Unmarshal(line, &resp); jerr != nil {
				fmt.Fprintf(p.stderr, "oracle: non-protocol stdout line: %.200q\n", line)
			} else {
				p.mu.Lock()
				ch := p.pending[resp.ID]
				delete(p.pending, resp.ID)
				p.mu.Unlock()
				if ch != nil {
					ch <- outcome{resp: &resp}
				}
			}
		}
		if err != nil {
			break
		}
	}
	<-errDone // drain stderr before Wait closes the pipe
	waitErr := p.cmd.Wait()
	p.mu.Lock()
	p.closed = true
	msg := "oracle: JVM exited"
	if waitErr != nil {
		msg += ": " + waitErr.Error()
	}
	if s := p.stderr.String(); s != "" {
		msg += "\n--- JVM stderr ---\n" + s
	}
	p.exitErr = errors.New(msg)
	for id, ch := range p.pending {
		ch <- outcome{err: p.exitErr}
		delete(p.pending, id)
	}
	p.mu.Unlock()
	close(p.done)
}

func (p *proc) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *proc) send(id uint64, line []byte, ch chan outcome) error {
	p.mu.Lock()
	if p.closed {
		err := p.exitErr
		p.mu.Unlock()
		return err
	}
	p.pending[id] = ch
	p.mu.Unlock()

	p.wmu.Lock()
	_, err := p.stdin.Write(line)
	p.wmu.Unlock()
	if err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		// The process is most likely gone: report how it died.
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			p.kill()
			<-p.done
		}
		return p.exitErr
	}
	return nil
}

func (p *proc) forget(id uint64) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *proc) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func getProc() (*proc, error) {
	curMu.Lock()
	defer curMu.Unlock()
	if cur != nil && !cur.isClosed() {
		return cur, nil
	}
	p, err := startProc()
	if err != nil {
		return nil, err
	}
	cur = p
	return p, nil
}

// Shutdown stops the JVM (it is restarted by the next call). Calling it from
// TestMain is optional: the JVM exits by itself when the Go process dies.
func Shutdown() {
	curMu.Lock()
	p := cur
	cur = nil
	curMu.Unlock()
	if p == nil {
		return
	}
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		p.kill()
		<-p.done
	}
}

// ---------------------------------------------------------------------------
// Calls

func timeout() time.Duration {
	if s := os.Getenv("PARITY_ORACLE_TIMEOUT"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return Timeout
}

// Call invokes the oracle function fn with args (any JSON-marshalable value,
// normally a map or struct; nil means no arguments) and unmarshals the result
// into out (which may be nil to ignore it). When the JVM answers ok=false, the
// error is an *OracleError. It is safe for concurrent use.
func Call(fn string, args any, out any) error {
	return CallEnv(nil, fn, args, out)
}

// CallEnv is Call with environment variable overrides for this call only: a
// string value sets the variable, a nil value unsets it (as seen by the
// Kotlin code through System.getenv).
func CallEnv(env map[string]any, fn string, args any, out any) error {
	if !enabled() {
		return ErrDisabled
	}
	if err := prepare(); err != nil {
		return err
	}
	if args == nil {
		args = struct{}{}
	}
	id := nextID.Add(1)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(request{ID: id, Fn: fn, Args: args, Env: env}); err != nil {
		return fmt.Errorf("oracle: encoding request: %w", err)
	}

	// A request that could not even be written never reached the JVM, so when the process was
	// found dead (killed by a previous timeout, crashed...) it is safe to start a fresh one once.
	var p *proc
	ch := make(chan outcome, 1)
	for attempt := 0; ; attempt++ {
		var err error
		if p, err = getProc(); err != nil {
			return err
		}
		if err = p.send(id, buf.Bytes(), ch); err == nil {
			break
		}
		if attempt > 0 || !p.isClosed() {
			return err
		}
	}
	timer := time.NewTimer(timeout())
	defer timer.Stop()
	var o outcome
	select {
	case o = <-ch:
	case <-timer.C:
		p.forget(id)
		p.kill()
		select { // let the reader notice the death so that the next call starts a fresh JVM
		case <-p.done:
		case <-time.After(5 * time.Second):
		}
		return fmt.Errorf("oracle: %s timed out after %v (JVM killed, it restarts on the next call)", fn, timeout())
	}
	if o.err != nil {
		return o.err
	}
	if !o.resp.OK {
		return &OracleError{Msg: o.resp.Error, Exception: o.resp.Exception, Original: o.resp.Original}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(o.resp.Result, out); err != nil {
		return fmt.Errorf("oracle: decoding result of %s (%s): %w", fn, o.resp.Result, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers

func skipOrFail(t testing.TB) {
	t.Helper()
	if !enabled() {
		t.Skip("JVM oracle disabled (set PARITY_ORACLE=1)")
	}
	if err := prepare(); err != nil {
		t.Fatalf("JVM oracle unavailable although PARITY_ORACLE=1: %v", err)
	}
}

// MustCall is Call for tests: it skips the test when the oracle is not
// enabled (PARITY_ORACLE=1) and fails it on any error, including an
// ok=false answer (use Call to inspect those, or MustFail to expect them).
// If PARITY_ORACLE=1 is set but the oracle cannot be built, the test fails
// instead of being skipped.
func MustCall(t testing.TB, fn string, args any, out any) {
	t.Helper()
	skipOrFail(t)
	if err := Call(fn, args, out); err != nil {
		t.Fatalf("oracle %s(%s): %v", fn, describeArgs(args), err)
	}
}

// MustFail is MustCall for calls that must be rejected by the JVM: it returns
// the *OracleError, and fails the test if the call succeeded.
func MustFail(t testing.TB, fn string, args any) *OracleError {
	t.Helper()
	skipOrFail(t)
	var out json.RawMessage
	err := Call(fn, args, &out)
	if err == nil {
		t.Fatalf("oracle %s(%s): expected an error, got result %s", fn, describeArgs(args), out)
	}
	var oe *OracleError
	if !errors.As(err, &oe) {
		t.Fatalf("oracle %s(%s): %v", fn, describeArgs(args), err)
	}
	return oe
}

func describeArgs(args any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	if len(b) > 300 {
		return string(b[:300]) + "..."
	}
	return string(b)
}
