package httpx

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"agora/internal/jsonjava"
)

// BodyKind tells how a Response body must be written.
type BodyKind int

const (
	// BodyNone: no body at all (ResponseEntity.build()).
	BodyNone BodyKind = iota
	// BodyValue: a DTO written through the negotiated converter (JSON or XML).
	BodyValue
	// BodyString: a Kotlin String body, written raw by StringHttpMessageConverter
	// with the negotiated content type (application/json by default).
	BodyString
	// BodyBytes: pre-encoded bytes with an explicit content type (TSV export,
	// ACME challenge). Still subject to ?mediaType negotiation when Negotiate.
	BodyBytes
)

// Header is an ordered response header.
type Header struct{ Name, Value string }

// Response is what a handler returns (ResponseEntity equivalent).
type Response struct {
	Status      int
	Headers     []Header
	Kind        BodyKind
	Value       any
	Str         string
	Bytes       []byte
	ContentType string // for BodyBytes
	// PrecomputedJSON, when set for BodyValue, is used instead of encoding Value
	// in JSON mode (hot shared responses serialized once).
	PrecomputedJSON []byte
}

// OK is ResponseEntity.ok().body(v).
func OK(v any) *Response { return &Response{Status: 200, Kind: BodyValue, Value: v} }

// JSON is ResponseEntity.status(status).body(v).
func JSON(status int, v any) *Response { return &Response{Status: status, Kind: BodyValue, Value: v} }

// Unit is ResponseEntity.status(status).body(Unit) → "{}".
func Unit(status int) *Response { return JSON(status, jsonjava.Unit{}) }

// Empty is ResponseEntity.status(status).build() (no body, no content type).
func Empty(status int) *Response { return &Response{Status: status, Kind: BodyNone} }

// String is a Kotlin String body (ResponseEntity<String>).
func String(status int, s string) *Response { return &Response{Status: status, Kind: BodyString, Str: s} }

// Bytes writes raw bytes with an explicit content type.
func Bytes(status int, contentType string, b []byte) *Response {
	return &Response{Status: status, Kind: BodyBytes, Bytes: b, ContentType: contentType}
}

// With adds a header (keeps exact casing) and returns r for chaining.
func (r *Response) With(name, value string) *Response {
	r.Headers = append(r.Headers, Header{name, value})
	return r
}

// CacheControl adds "Cache-Control: max-age=N, public|private" like
// ResponseEntity.ok().cacheControl(CacheControl.maxAge(N, SECONDS).cachePublic()).
func (r *Response) CacheControl(maxAgeSeconds int, public bool) *Response {
	v := "max-age=" + strconv.Itoa(maxAgeSeconds)
	if public {
		v += ", public"
	} else {
		v += ", private"
	}
	return r.With("Cache-Control", v)
}

// SpringError is thrown (panic) to produce Spring Boot's BasicErrorController
// response: {"timestamp":<epoch ms>,"status":N,"error":"<reason>","path":"<uri>"}.
// It covers MissingRequestHeaderException, MissingServletRequestParameterException,
// MethodArgumentTypeMismatchException, HttpMessageNotReadableException (400),
// HttpMediaTypeNotSupportedException (415), etc.
type SpringError struct {
	Status int
	Cause  string // for logs only
	// Extra headers such as Allow (405) or Accept (415/406).
	Headers []Header
}

func (e *SpringError) Error() string { return fmt.Sprintf("spring error %d: %s", e.Status, e.Cause) }

// AdviceError is a domain exception mapped by DefaultControllerAdvice to
// {"title": Title} with Status.
type AdviceError struct {
	Status int
	Title  string
}

func (e *AdviceError) Error() string { return fmt.Sprintf("%d %s", e.Status, e.Title) }

// ErrorBody is Spring Boot's DefaultErrorAttributes output (no message/trace).
type ErrorBody struct {
	Timestamp jsonjava.EpochMillis `json:"timestamp"`
	Status    int                  `json:"status"`
	Error     string               `json:"error"`
	Path      string               `json:"path"`
}

// JavaName is the XML root element (BasicErrorController returns Map<String, Object>).
func (ErrorBody) JavaName() string { return "Map" }

// AdviceBody is fr.gouv.agora.infrastructure.common.ErrorResponse.
type AdviceBody struct {
	Title string `json:"title"`
}

// JavaName for XML serialization.
func (AdviceBody) JavaName() string { return "ErrorResponse" }

// ReasonPhrase returns Spring's HttpStatus reason phrase.
func ReasonPhrase(status int) string {
	switch status {
	case 418:
		return "I'm a teapot"
	case 999:
		return "None"
	case 413:
		return "Payload Too Large"
	case 414:
		return "URI Too Long"
	case 416:
		return "Requested range not satisfiable"
	}
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "Http Status " + strconv.Itoa(status)
}

func newErrorBody(status int, path string, now time.Time) ErrorBody {
	return ErrorBody{
		Timestamp: jsonjava.EpochMillis(now.UnixMilli()),
		Status:    status,
		Error:     ReasonPhrase(status),
		Path:      path,
	}
}
