// Package strapi is the Strapi v5 CMS client, byte-for-byte compatible with
// fr.gouv.agora.config.CmsStrapiHttpClient + infrastructure.common.StrapiRequestBuilder.
package strapi

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"agora/internal/javacompat"
)

// RequestBuilder reproduces StrapiRequestBuilder (same URI byte for byte).
type RequestBuilder struct {
	cmsModel     string
	filters      string
	sort         string
	populate     string
	pageSize     string
	unpublished  string
	builderError string
}

// NewRequest is StrapiRequestBuilder(cmsModel).
func NewRequest(cmsModel string) *RequestBuilder {
	return &RequestBuilder{
		cmsModel: cmsModel,
		populate: "&populate=*",
		pageSize: "pagination[pageSize]=100",
	}
}

func (b *RequestBuilder) applyFilter(key, operator string, value *string) *RequestBuilder {
	if value == nil || *value == "" {
		b.builderError = "filterIn : aucune valeur donnée pour le champs " + key
		return b
	}
	b.filters += "&filters" + key + "[$" + operator + "]=" + javacompat.URLEncode(*value)
	return b
}

// FilterIn is filterIn(field, values).
func (b *RequestBuilder) FilterIn(field string, values []string) *RequestBuilder {
	return b.FilterInPath([]string{field}, values)
}

// FilterInNullable is filterIn(field, List<String?>?) where elements may be null.
func (b *RequestBuilder) FilterInNullable(field []string, values []*string, isNull bool) *RequestBuilder {
	fieldParam := ""
	for _, f := range field {
		fieldParam += "[" + f + "]"
	}
	if !isNull && len(values) > 80 {
		slog.Warn(fmt.Sprintf("attention : ne peut pas gérer plus de ~100 filtres dans l'url (%d/100)", len(values)))
	}
	if isNull || len(values) == 0 {
		b.builderError = "filterIn : aucune valeur donnée pour le champs " + kotlinListString(field)
		return b
	}
	for _, v := range values {
		b.applyFilter(fieldParam, "in", v)
	}
	return b
}

// FilterInPath is filterIn(List<String> field, values).
func (b *RequestBuilder) FilterInPath(field []string, values []string) *RequestBuilder {
	ptrs := make([]*string, len(values))
	for i := range values {
		v := values[i]
		ptrs[i] = &v
	}
	return b.FilterInNullable(field, ptrs, values == nil)
}

func kotlinListString(l []string) string { return "[" + strings.Join(l, ", ") + "]" }

// Contains is contains(field, value) ($containsi).
func (b *RequestBuilder) Contains(field string, value *string) *RequestBuilder {
	return b.applyFilter("["+field+"]", "containsi", value)
}

// GetByIDs is getByIds(ids) (documentId $in).
func (b *RequestBuilder) GetByIDs(ids []string) *RequestBuilder { return b.FilterIn("documentId", ids) }

// WithDateBefore is withDateBefore(date, field) ($lt, ISO_DATE_TIME of a LocalDateTime).
func (b *RequestBuilder) WithDateBefore(date time.Time, field string) *RequestBuilder {
	b.filters += "&filters[" + field + "][$lt]=" + ISODateTime(date)
	return b
}

// WithDateAfter is withDateAfter(date, field) ($gt).
func (b *RequestBuilder) WithDateAfter(date time.Time, field string) *RequestBuilder {
	b.filters += "&filters[" + field + "][$gt]=" + ISODateTime(date)
	return b
}

// SortBy is sortBy(field, direction).
func (b *RequestBuilder) SortBy(field, direction string) *RequestBuilder {
	b.sort += "&sort[0]=" + field + ":" + direction
	return b
}

// WithPageSize is withPageSize(n).
func (b *RequestBuilder) WithPageSize(n int) *RequestBuilder {
	b.pageSize = "pagination[pageSize]=" + strconv.Itoa(n)
	return b
}

// Populate is populate(newPopulate): "&populate" + newPopulate.
func (b *RequestBuilder) Populate(p string) *RequestBuilder {
	b.populate = "&populate" + p
	return b
}

// WithUnpublished is withUnpublished() (Strapi v5 status=draft).
func (b *RequestBuilder) WithUnpublished() *RequestBuilder {
	b.unpublished = "&status=draft"
	return b
}

// ErrBuilder mirrors the Exception thrown by build() on a builder error.
var ErrBuilder = errors.New("strapi builder error")

// Build is build().
func (b *RequestBuilder) Build() (string, error) {
	if strings.TrimSpace(b.builderError) != "" {
		return "", fmt.Errorf("%w: %s", ErrBuilder, b.builderError)
	}
	return b.cmsModel + "?" + b.pageSize + b.unpublished + b.populate + b.filters + b.sort, nil
}

// String mirrors the data class toString used in log messages.
func (b *RequestBuilder) String() string {
	return "StrapiRequestBuilder(cmsModel=" + b.cmsModel + ")"
}

// ISODateTime formats a LocalDateTime (wall clock in time.Local) like
// DateTimeFormatter.ISO_DATE_TIME: yyyy-MM-ddTHH:mm:ss[.fraction without trailing zeros].
// LocalDateTime.now() has microsecond precision on Linux JDK 17.
func ISODateTime(t time.Time) string {
	t = t.In(time.Local).Truncate(time.Microsecond)
	s := t.Format("2006-01-02T15:04:05")
	if ns := t.Nanosecond(); ns != 0 {
		frac := fmt.Sprintf("%09d", ns)
		frac = strings.TrimRight(frac, "0")
		s += "." + frac
	}
	return s
}
