package acme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// RestTemplate of config/HttpClientConfig.kt: SimpleClientHttpRequestFactory
// (HttpURLConnection) with a 10 s connect timeout and a 30 s read timeout.
const (
	restConnectTimeout = 10 * time.Second
	restReadTimeout    = 30 * time.Second
	// Total deadline of a call (class C, see parity/divergences/S11.md): the
	// JVM only bounds each socket read.
	restTotalTimeout = 90 * time.Second
)

// RestClient reproduces `restTemplate.exchange(url, method, entity, Map::class.java)`:
//   - 4xx and 5xx answers raise an exception (DefaultResponseErrorHandler);
//   - the body of any other answer is extracted as a Map: no body (1xx, 204,
//     304, Content-Length: 0, empty stream) gives nil, a JSON content type
//     (application/json, application/*+json) is decoded to an object, any
//     other content type (default application/octet-stream) is an error even
//     for a 2xx status;
//   - GET redirects are followed to the same protocol, other methods never
//     (SimpleClientHttpRequestFactory.prepareConnection).
type RestClient struct {
	HTTP *http.Client
}

// NewRestClient builds the client of the production wiring.
func NewRestClient() *RestClient {
	return &RestClient{HTTP: &http.Client{
		Transport: newOutboundTransport(restConnectTimeout, restReadTimeout),
		Timeout:   restTotalTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if via[0].Method != http.MethodGet || len(via) >= 20 || req.URL.Scheme != via[len(via)-1].URL.Scheme {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

// HTTPStatusError is HttpClientErrorException / HttpServerErrorException.
type HTTPStatusError struct {
	Status int
	Body   string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HttpStatusCodeException: %d: %s", e.Status, e.Body)
}

// RestResponse is the ResponseEntity<Map>.
type RestResponse struct {
	Status int
	// Body is the decoded JSON object, nil when the answer has no body.
	Body map[string]any
}

// Is2xx is statusCode.is2xxSuccessful.
func (r *RestResponse) Is2xx() bool { return r.Status >= 200 && r.Status < 300 }

// Exchange performs the request. headers are set as given.
func (c *RestClient) Exchange(ctx context.Context, method, target string, headers [][2]string, body []byte) (*RestResponse, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, fmt.Errorf("IllegalArgumentException: %w", err)
	}
	req.Header.Set("Accept", "application/json, application/*+json")
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ResourceAccessException: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("ResourceAccessException: %w", err)
	}
	if res.StatusCode >= 400 {
		return nil, &HTTPStatusError{Status: res.StatusCode, Body: string(raw)}
	}
	out := &RestResponse{Status: res.StatusCode}
	// MessageBodyClientHttpResponseWrapper.hasMessageBody / hasEmptyMessageBody
	if res.StatusCode < 200 || res.StatusCode == 204 || res.StatusCode == 304 || res.Header.Get("Content-Length") == "0" || len(raw) == 0 {
		return out, nil
	}
	ct := res.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(ct)
	if ct == "" {
		mt = "application/octet-stream"
	} else if err != nil {
		return nil, fmt.Errorf("UnknownContentTypeException: invalid content type %q", ct)
	}
	if mt != "application/json" && !(strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json")) {
		return nil, fmt.Errorf("UnknownContentTypeException: Could not extract response: no suitable HttpMessageConverter found for response type [class java.util.Map] and content type [%s]", mt)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("RestClientException: Error while extracting response: %w", err)
	}
	switch t := v.(type) {
	case nil: // JSON null: Jackson returns null
		return out, nil
	case map[string]any:
		out.Body = t
		return out, nil
	}
	return nil, errors.New("RestClientException: Error while extracting response: cannot deserialize the JSON value into a Map")
}
