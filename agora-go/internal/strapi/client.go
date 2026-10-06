package strapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"agora/internal/jsonjava"
)

// Client is CmsStrapiHttpClient.
type Client struct {
	baseURL   string
	token     string
	suspended bool
	http      *http.Client
	log       *slog.Logger
}

// Options configures the client.
type Options struct {
	BaseURL   string // cms.api.url (CMS_API_URL), e.g. http://host/api/
	Token     string // cms.auth.token
	Suspended bool   // strapi.suspended
	// Timeout bounds a whole request. Kotlin had a 20s connect timeout and no
	// read timeout (divergence C-STRAPI-TIMEOUT).
	Timeout time.Duration
	Logger  *slog.Logger
}

// NewClient builds the client.
func NewClient(o Options) *Client {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Timeout == 0 {
		o.Timeout = 60 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 64
	tr.ResponseHeaderTimeout = o.Timeout
	return &Client{
		baseURL:   o.BaseURL,
		token:     o.Token,
		suspended: o.Suspended,
		http:      &http.Client{Transport: tr, Timeout: o.Timeout},
		log:       o.Logger,
	}
}

// ErrSuspended is StrapiTrafficSuspendedException.
var ErrSuspended = errors.New("StrapiTrafficSuspendedException")

// Suspended reports STRAPI_SUSPENDED.
func (c *Client) Suspended() bool { return c.suspended }

// fetch performs GET {CMS_API_URL}{uri with spaces as %20}.
func (c *Client) fetch(ctx context.Context, b *RequestBuilder) ([]byte, error) {
	uri, err := b.Build()
	if err != nil {
		return nil, err
	}
	uri = strings.ReplaceAll(uri, " ", "%20")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Kotlin never checked the status code: the body is parsed whatever it is.
	return io.ReadAll(resp.Body)
}

// Meta is StrapiMetadata.
type Meta struct {
	Pagination Pagination `json:"pagination"`
}

// Pagination is StrapiMetaPagination (all Int, primitives).
type Pagination struct {
	Page      int `json:"page"`
	PageSize  int `json:"pageSize"`
	PageCount int `json:"pageCount"`
	Total     int `json:"total"`
}

// Envelope is StrapiDTO<T>.
type Envelope[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// Collection is CmsStrapiHttpClient.request: any failure (suspended, HTTP,
// unparsable body, missing required field anywhere) → StrapiDTO.ofEmpty().
func Collection[T any](ctx context.Context, c *Client, b *RequestBuilder) Envelope[T] {
	if c.suspended {
		c.log.Warn("Trafic Strapi suspendu (STRAPI_SUSPENDED=true) — requête ignorée : " + b.String())
		return Envelope[T]{Data: []T{}}
	}
	body, err := c.fetch(ctx, b)
	if err == nil {
		var env Envelope[T]
		if err = jsonjava.Unmarshal(body, &env); err == nil {
			return env
		}
	}
	c.log.Error(fmt.Sprintf("Erreur lors de la requête du builder %s: %v", b.String(), err))
	return Envelope[T]{Data: []T{}}
}

// SingleEnvelope is StrapiSingleTypeDTO<T>.
type SingleEnvelope[T any] struct {
	Data T `json:"data"`
}

// Single is CmsStrapiHttpClient.requestSingleType: failures are returned
// (the Kotlin code rethrew them → HTTP 500).
func Single[T any](ctx context.Context, c *Client, b *RequestBuilder) (T, error) {
	var zero T
	if c.suspended {
		c.log.Warn("Trafic Strapi suspendu (STRAPI_SUSPENDED=true) — requête ignorée : " + b.String())
		return zero, ErrSuspended
	}
	body, err := c.fetch(ctx, b)
	if err != nil {
		c.log.Error(fmt.Sprintf("Erreur lors de la requête du builder %s: %v", b.String(), err))
		return zero, err
	}
	var env SingleEnvelope[T]
	if err := jsonjava.Unmarshal(body, &env); err != nil {
		c.log.Error(fmt.Sprintf("Erreur lors de la requête du builder %s: %v", b.String(), err))
		return zero, err
	}
	return env.Data, nil
}
