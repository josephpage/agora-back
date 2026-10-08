package qaglist

import (
	"context"
	"math"
	"sort"
	"time"

	"agora/internal/javacompat"
	"agora/internal/modules/qag"
	"agora/internal/modules/themehebdo"
	"agora/internal/modules/thematique"
)

// Constants of QagPaginatedV2UseCase.
const (
	maxPageListSize           = 20
	trendingOldThresholdHours = 72
	trendingMaxOldQags        = 3
	trendingSlots             = 9
	maxPerCluster             = 2

	filterTop        = "top"
	filterLatest     = "latest"
	filterSupporting = "supporting"
	filterTrending   = "trending"
)

// QagsAndMaxPageCountV2 is the data class of the same name.
type QagsAndMaxPageCountV2 struct {
	Qags         []qag.QagPreview
	Header       *HeaderQag
	MaxPageCount int
}

// qagListWithMaxPageCount is QagListWithMaxPageCount.
type qagListWithMaxPageCount struct {
	maxPageCount int
	qags         []qag.QagWithSupportCount
}

// The collaborators of QagPaginatedV2UseCase (the Kotlin constructor parameters).

type pageSource interface {
	// Count is QagInfoRepository.getQagsCount(thematiqueId) (shared, micro-cached).
	Count(ctx context.Context, thematiqueID *string) (int, error)
	// Page is getPopularQagsPaginatedV2 / getLatestQagsPaginatedV2 (shared, micro-cached).
	Page(ctx context.Context, filter string, offset int, thematiqueID *string) ([]qag.QagInfoWithSupportCount, error)
}

type supportedQagsSource interface {
	GetSupportedQagsPaginatedV2(ctx context.Context, userID string, offset int, thematiqueID *string) ([]qag.QagInfoWithSupportCount, error)
}

type supportReader interface {
	GetUserSupportedQagIDs(ctx context.Context, userID string) ([]string, error)
	GetSupportedQagCount(ctx context.Context, userID string, thematiqueID *string) (int, error)
}

type thematiqueReader interface {
	ByID(ctx context.Context, id string) *thematique.Thematique
}

type headerReader interface {
	Header(ctx context.Context, filterType string) (*HeaderQag, error)
}

// trendingSource is TrendingQagCacheRepository + QagInfoRepository.getTrendingQagsV3: the
// shared candidates, kept five minutes. The returned slice is shared: do not modify it.
type trendingSource interface {
	Candidates(ctx context.Context) ([]qag.QagInfoWithSupportCount, error)
}

type currentThemeReader interface {
	Current(ctx context.Context) (themehebdo.ThemeHebdo, error)
}

type clusterReader interface {
	Clusters(ctx context.Context) []TrendingCluster
}

// PaginatedUseCase is QagPaginatedV2UseCase.
type PaginatedUseCase struct {
	supported  supportedQagsSource
	shared     pageSource
	themes     thematiqueReader
	headers    headerReader
	trending   trendingSource
	supports   supportReader
	themeHebdo currentThemeReader
	clusters   clusterReader
	exponent   float64
	now        func() time.Time
}

// GetPopularQagPaginated is getPopularQagPaginated; nil is Kotlin's null (HTTP 400).
func (u *PaginatedUseCase) GetPopularQagPaginated(ctx context.Context, userID string, pageNumber int, thematiqueID *string) (*QagsAndMaxPageCountV2, error) {
	return u.paginated(ctx, filterTop, userID, pageNumber, thematiqueID)
}

// GetLatestQagPaginated is getLatestQagPaginated.
func (u *PaginatedUseCase) GetLatestQagPaginated(ctx context.Context, userID string, pageNumber int, thematiqueID *string) (*QagsAndMaxPageCountV2, error) {
	return u.paginated(ctx, filterLatest, userID, pageNumber, thematiqueID)
}

// GetSupportedQagPaginated is getSupportedQagPaginated.
func (u *PaginatedUseCase) GetSupportedQagPaginated(ctx context.Context, userID string, pageNumber int, thematiqueID *string) (*QagsAndMaxPageCountV2, error) {
	return u.paginated(ctx, filterSupporting, userID, pageNumber, thematiqueID)
}

func (u *PaginatedUseCase) paginated(ctx context.Context, filter, userID string, pageNumber int, thematiqueID *string) (*QagsAndMaxPageCountV2, error) {
	list, err := u.getQagPaginated(ctx, filter, userID, pageNumber, thematiqueID)
	if err != nil || list == nil {
		return nil, err
	}
	return u.mapQags(ctx, list, userID, filter, pageNumber)
}

// getQagPaginated: the count first, then the page (Kotlin's order). Offsets are
// Int arithmetic: a huge page number wraps around like in Kotlin (a negative OFFSET
// is a PostgreSQL error, HTTP 500).
func (u *PaginatedUseCase) getQagPaginated(ctx context.Context, filter, userID string, pageNumber int, thematiqueID *string) (*qagListWithMaxPageCount, error) {
	if pageNumber < 1 {
		return nil, nil
	}
	offset := int(int32(pageNumber-1) * maxPageListSize)

	var count int
	var err error
	if filter == filterSupporting {
		count, err = u.supports.GetSupportedQagCount(ctx, userID, thematiqueID)
	} else {
		count, err = u.shared.Count(ctx, thematiqueID)
	}
	if err != nil {
		return nil, err
	}
	if offset > count {
		return nil, nil
	}

	var qags []qag.QagInfoWithSupportCount
	switch filter {
	case filterSupporting:
		qags, err = u.supported.GetSupportedQagsPaginatedV2(ctx, userID, offset, thematiqueID)
	default:
		qags, err = u.shared.Page(ctx, filter, offset, thematiqueID)
	}
	if err != nil {
		return nil, err
	}
	withThematique := make([]qag.QagWithSupportCount, 0, len(qags))
	for _, q := range qags {
		if th := u.themes.ByID(ctx, q.ThematiqueID); th != nil {
			withThematique = append(withThematique, qag.QagWithSupportCount{QagInfo: q, Thematique: *th})
		}
	}
	return &qagListWithMaxPageCount{maxPageCount: ceilDiv(count, maxPageListSize), qags: withThematique}, nil
}

// ceilDiv is ceil(count.toDouble() / size.toDouble()).toInt() for count >= 0.
func ceilDiv(count, size int) int {
	return int(math.Ceil(float64(count) / float64(size)))
}

// mapQags: the header of the tab (first page only), then the user's supports.
func (u *PaginatedUseCase) mapQags(ctx context.Context, list *qagListWithMaxPageCount, userID, filter string, pageNumber int) (*QagsAndMaxPageCountV2, error) {
	var header *HeaderQag
	if pageNumber == 1 {
		h, err := u.headers.Header(ctx, filter)
		if err != nil {
			return nil, err
		}
		header = h
	}
	previews, err := u.previews(ctx, list.qags, userID)
	if err != nil {
		return nil, err
	}
	return &QagsAndMaxPageCountV2{Qags: previews, Header: header, MaxPageCount: list.maxPageCount}, nil
}

// previews maps the QaGs of a page with the user's overlay: supported by the user
// (one indexed query per request) and written by the user.
func (u *PaginatedUseCase) previews(ctx context.Context, qags []qag.QagWithSupportCount, userID string) ([]qag.QagPreview, error) {
	ids, err := u.supports.GetUserSupportedQagIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	supported := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		supported[id] = struct{}{}
	}
	out := make([]qag.QagPreview, len(qags))
	for i, q := range qags {
		_, isSupported := supported[q.QagInfo.ID]
		out[i] = qag.ToPreviewWithThematique(q, isSupported, q.QagInfo.UserID == userID)
	}
	return out, nil
}

// GetTrendingQag is getTrendingQag(userId): the most recent accepted QaG pinned, then
// the nine best by score with the guards (3 QaGs older than 72 h, 2 per cluster in a
// free-theme week). It never returns nil.
func (u *PaginatedUseCase) GetTrendingQag(ctx context.Context, userID string) (*QagsAndMaxPageCountV2, error) {
	now := u.now()
	all, err := u.trending.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return u.buildTrendingResult(ctx, nil, userID)
	}

	pinned := all[0]
	score := func(q qag.QagInfoWithSupportCount) float64 {
		return float64(q.SupportCount+1) / javaPow(float64(hoursSince(q, now))+2.0, u.exponent)
	}

	theme, err := u.themeHebdo.Current(ctx)
	if err != nil {
		return nil, err
	}
	estThemeLibre := theme.EstThemeLibre
	var clusters []TrendingCluster
	if estThemeLibre {
		clusters = u.clusters.Clusters(ctx)
	}
	clusterCount := map[string]int{}

	type scored struct {
		q     qag.QagInfoWithSupportCount
		score float64
	}
	var candidates []scored
	for _, q := range all {
		if q.ID != pinned.ID {
			candidates = append(candidates, scored{q, score(q)})
		}
	}
	// sortedByDescending: stable, Double.compareTo order
	sort.SliceStable(candidates, func(i, j int) bool {
		return javaDoubleCompare(candidates[j].score, candidates[i].score) < 0
	})

	slots := make([]qag.QagInfoWithSupportCount, 0, trendingSlots)
	oldQagCount := 0
	for _, c := range candidates {
		if len(slots) >= trendingSlots {
			break
		}
		q := c.q
		isOld := hoursSince(q, now) > trendingOldThresholdHours
		if isOld && oldQagCount >= trendingMaxOldQags {
			continue
		}
		if isOld {
			oldQagCount++
		}

		if estThemeLibre {
			// every cluster that matches (a QaG can belong to several clusters)
			var matching []TrendingCluster
			for _, cl := range clusters {
				for _, mot := range cl.Mots {
					if javacompat.ContainsIgnoreCase(q.Title, mot) {
						matching = append(matching, cl)
						break
					}
				}
			}
			if len(matching) > 0 {
				rejected := false
				for _, cl := range matching {
					if clusterCount[cl.ID] >= maxPerCluster {
						rejected = true
						break
					}
				}
				if rejected {
					continue
				}
				for _, cl := range matching {
					clusterCount[cl.ID] = clusterCount[cl.ID] + 1
				}
			}
		}
		slots = append(slots, q)
	}
	return u.buildTrendingResult(ctx, append([]qag.QagInfoWithSupportCount{pinned}, slots...), userID)
}

func (u *PaginatedUseCase) buildTrendingResult(ctx context.Context, qags []qag.QagInfoWithSupportCount, userID string) (*QagsAndMaxPageCountV2, error) {
	withThematique := make([]qag.QagWithSupportCount, 0, len(qags))
	for _, q := range qags {
		if th := u.themes.ByID(ctx, q.ThematiqueID); th != nil {
			withThematique = append(withThematique, qag.QagWithSupportCount{QagInfo: q, Thematique: *th})
		}
	}
	ids, err := u.supports.GetUserSupportedQagIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	header, err := u.headers.Header(ctx, filterTrending)
	if err != nil {
		return nil, err
	}
	supported := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		supported[id] = struct{}{}
	}
	previews := make([]qag.QagPreview, len(withThematique))
	for i, q := range withThematique {
		_, isSupported := supported[q.QagInfo.ID]
		previews[i] = qag.ToPreviewWithThematique(q, isSupported, q.QagInfo.UserID == userID)
	}
	return &QagsAndMaxPageCountV2{Qags: previews, Header: header, MaxPageCount: 1}, nil
}

// hoursSince is Duration.between(moderatedDate ?: date, now).toHours() (whole hours of the
// floor-normalised seconds, truncated toward zero).
func hoursSince(q qag.QagInfoWithSupportCount, now time.Time) int64 {
	moderated := q.Date
	if q.ModeratedDate != nil {
		moderated = *q.ModeratedDate
	}
	ns := now.Sub(moderated).Nanoseconds()
	sec := ns / 1_000_000_000
	if ns%1_000_000_000 < 0 {
		sec--
	}
	return sec / 3600
}

// javaDoubleCompare is Double.compare: -0.0 < 0.0 and NaN is greater than everything.
func javaDoubleCompare(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	ab, bb := int64(math.Float64bits(a)), int64(math.Float64bits(b))
	if math.IsNaN(a) {
		ab = 0x7ff8000000000000
	}
	if math.IsNaN(b) {
		bb = 0x7ff8000000000000
	}
	switch {
	case ab == bb:
		return 0
	case ab < bb:
		return -1
	}
	return 1
}

// javaPow is Math.pow.
func javaPow(x, y float64) float64 { return math.Pow(x, y) }
