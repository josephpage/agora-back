package qag

import (
	"context"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/modules/thematique"
)

// responseQagCache is the L1 name of the micro-cache of Strapi responses.
const responseQagCache = "qagResponse"

// qagInfoReader is the part of QagInfoRepository the details need.
type qagInfoReader interface {
	// GetQagWithSupportCountCached is getQagWithSupportCount.
	GetQagWithSupportCountCached(ctx context.Context, qagID string) (*QagInfoWithSupportCount, error)
}

// thematiqueReader is ThematiqueRepository.getThematique.
type thematiqueReader interface {
	ByID(ctx context.Context, id string) *thematique.Thematique
}

// responseReader is ResponseQagRepository.getResponseQag.
type responseReader interface {
	getResponseQag(ctx context.Context, qagID string) *ResponseQag
}

// feedbackReader is FeedbackQagUseCase as the details need it.
type feedbackReader interface {
	GetFeedbackResults(ctx context.Context, qagID string) (*FeedbackResults, error)
	GetFeedbackForQagAndUser(ctx context.Context, qagID, userID string) (*bool, error)
}

// supportReader is SupportQagUseCase / GetSupportQagRepository as the details need it.
type supportReader interface {
	// isQagSupportedByUser is `getUserSupportedQagIds(userId).any { it == qagId }` in one query.
	isQagSupportedByUser(ctx context.Context, userID, qagID string) (bool, error)
}

// DetailsAggregate is QagDetailsAggregate.
type DetailsAggregate struct {
	info      qagInfoReader
	responses responseReader
	feedbacks feedbackReader
	themes    thematiqueReader
}

// GetQag is QagDetailsAggregate.getQag(qagId): the QaG with its thematique, and
// for a QaG selected for response its Government response and the feedback
// results. nil when the QaG or its thematique does not exist.
func (g *DetailsAggregate) GetQag(ctx context.Context, qagID string) (*QagDetails, error) {
	info, err := g.info.GetQagWithSupportCountCached(ctx, qagID)
	if err != nil || info == nil {
		return nil, err
	}
	th := g.themes.ByID(ctx, info.ThematiqueID)
	if th == nil {
		return nil, nil
	}
	var response *ResponseQag
	if info.Status == StatusSelectedForResponse {
		response = g.responses.getResponseQag(ctx, qagID)
	}
	var feedbackResults *FeedbackResults
	if response != nil {
		feedbackResults, err = g.feedbacks.GetFeedbackResults(ctx, qagID)
		if err != nil {
			return nil, err
		}
	}
	return &QagDetails{
		ID: info.ID, Thematique: *th, Title: info.Title, Description: info.Description, Date: info.Date,
		Status: info.Status, Username: info.Username, UserID: info.UserID, SupportCount: info.SupportCount,
		Response: response, FeedbackResults: feedbackResults,
	}, nil
}

// QagResult is the sealed class of GetQagDetailsUseCase.
type QagResult struct {
	Kind QagResultKind
	Qag  QagWithUserData // Kind == QagResultSuccess
}

// QagResultKind tells which QagResult it is.
type QagResultKind int

// The QagResult variants.
const (
	QagResultSuccess QagResultKind = iota
	QagResultRejectedStatus
	QagResultNotFound
)

// GetQagDetailsUseCase is GetQagDetailsUseCase.
type GetQagDetailsUseCase struct {
	aggregate *DetailsAggregate
	supports  supportReader
	feedbacks feedbackReader
}

// GetQagDetails is getQagDetails(qagId, userId).
func (u *GetQagDetailsUseCase) GetQagDetails(ctx context.Context, qagID, userID string) (QagResult, error) {
	qag, err := u.aggregate.GetQag(ctx, qagID)
	if err != nil || qag == nil {
		return QagResult{Kind: QagResultNotFound}, err
	}
	switch qag.Status {
	case StatusArchived:
		return QagResult{Kind: QagResultNotFound}, nil
	case StatusModeratedRejected:
		return QagResult{Kind: QagResultRejectedStatus}, nil
	}
	if qag.Status == StatusOpen && userID != qag.UserID {
		return QagResult{Kind: QagResultNotFound}, nil
	}

	var isHelpful *bool
	// The user's feedback only shows in the response blocks of the JSON, which
	// exist when the QaG has a response: it is not read (nor cached) otherwise.
	if qag.Response != nil {
		isHelpful, err = u.feedbacks.GetFeedbackForQagAndUser(ctx, qagID, userID)
		if err != nil {
			return QagResult{}, err
		}
	}
	supported, err := u.isSupportedByUser(ctx, qag, userID)
	if err != nil {
		return QagResult{}, err
	}
	details := *qag
	if isHelpful == nil {
		details.FeedbackResults = nil // QagDetailsMapper.toQagWithoutFeedbackResults
	}
	return QagResult{Kind: QagResultSuccess, Qag: QagWithUserData{
		QagDetails:        details,
		CanShare:          qag.Status == StatusModeratedAccepted || qag.Status == StatusSelectedForResponse,
		CanSupport:        qag.Status == StatusOpen || qag.Status == StatusModeratedAccepted,
		CanDelete:         qag.UserID == userID && qag.Status != StatusSelectedForResponse,
		IsAuthor:          qag.UserID == userID,
		IsSupportedByUser: supported,
		IsHelpful:         isHelpful,
	}}, nil
}

func (u *GetQagDetailsUseCase) isSupportedByUser(ctx context.Context, qag *QagDetails, userID string) (bool, error) {
	switch qag.Status {
	case StatusArchived, StatusModeratedRejected:
		return false, nil
	case StatusSelectedForResponse:
		return true, nil
	}
	return u.supports.isQagSupportedByUser(ctx, userID, qag.ID)
}

// GetPublicQagDetailsUseCase is GetPublicQagDetailsUseCase.
type GetPublicQagDetailsUseCase struct{ aggregate *DetailsAggregate }

// GetQagDetails is getQagDetails(qagId): accepted and selected QaGs only.
func (u *GetPublicQagDetailsUseCase) GetQagDetails(ctx context.Context, qagID string) (*QagDetails, error) {
	qag, err := u.aggregate.GetQag(ctx, qagID)
	if err != nil || qag == nil {
		return nil, err
	}
	switch qag.Status {
	case StatusOpen, StatusArchived, StatusModeratedRejected:
		return nil, nil
	}
	return qag, nil
}

// ---------------------------------------------------------------------------
// Wiring of the Strapi response (micro-cached)
// ---------------------------------------------------------------------------

// microResponses reads the Government response of a QaG through a micro-cache:
// Kotlin asked Strapi at every call. Only a response found is kept.
type microResponses struct {
	a    *app.App
	repo *ResponseRepository
}

func (m microResponses) getResponseQag(ctx context.Context, qagID string) *ResponseQag {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil
	}
	ttl := m.a.Cfg.MicroCacheTTL
	if ttl <= 0 {
		return m.repo.fetch(ctx, uid)
	}
	r, _ := microLoad(m.a, responseQagCache, uid, ttl, func() (*ResponseQag, bool, error) {
		// the shared load must not die with the request that started it
		v := m.repo.fetch(context.WithoutCancel(ctx), uid)
		return v, v != nil, nil
	})
	return r
}
