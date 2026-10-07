// Package qag ports the QaG write side and the QaG details: QaG details
// (authenticated and public), insertion, deletion, ask status, support /
// unsupport and feedback, with the repositories and use cases that the QaG lists
// (S3), the moderation (S7), the tasks and the users deletion reuse.
//
// Other slices use the exported Service (see Get):
//
//	s := qag.Get(a)
//	s.Info.GetQagByKeywordsList / GetPopularQagsPaginatedV2 / GetTrendingQags ...   QagInfoRepository
//	s.Supports.GetUserSupportedQags / GetSupportedQagCount / ...                    SupportQagRepository + GetSupportQagRepository
//	s.Updates, s.DeleteLog, s.LowPriority, s.Feedbacks                              the other repositories
//	s.AdminUpdateQagStatus(ctx, id, status)                                         AdminUpdateQagStatusUseCase
//	s.GetQagByKeywords(ctx, userID, keywords)                                       GetQagByKeywordsUseCase
//
// See parity/ledger/S2.md for the complete list.
package qag

import (
	"context"

	"agora/internal/app"
	"agora/internal/modules/login"
	"agora/internal/modules/thematique"
)

// Service gathers the S2 repositories and use cases.
type Service struct {
	a *app.App

	// Repositories (usecase/*/repository interfaces and their implementations).
	Info        *InfoRepository
	Supports    *SupportRepository
	Feedbacks   *FeedbackRepository
	Updates     *UpdatesRepository
	DeleteLog   *DeleteLogRepository
	LowPriority *LowPriorityRepository
	Responses   *ResponseRepository

	// Use cases.
	Feedback      *FeedbackUseCase
	Details       *GetQagDetailsUseCase
	PublicDetails *GetPublicQagDetailsUseCase
	AskStatus     *GetAskQagStatusUseCase
	ErrorText     *GetQagErrorTextUseCase
	Insert        *InsertQagUseCase
	Delete        *DeleteQagUseCase
	AdminUpdate   *AdminUpdateQagStatusUseCase
	Keywords      *GetQagByKeywordsUseCase

	q          queues
	suspicious interface {
		IsSuspiciousActivity(ctx context.Context, ipAddressHash, userAgent string) (bool, error)
	}
}

// Get returns the App-wide QaG service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "qag", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	lg := login.Get(a)
	themes := thematique.Get(a)

	info := &InfoRepository{a: a}
	supports := &SupportRepository{a: a, info: info}
	feedbacks := &FeedbackRepository{a: a}
	responses := &ResponseRepository{a: a}

	feedback := &FeedbackUseCase{
		flags:   lg.Flags,
		repo:    feedbacks,
		results: l1FeedbackResults{a},
		users:   l1UserFeedback{a},
	}
	aggregate := &DetailsAggregate{
		info:      info,
		responses: microResponses{a: a, repo: responses},
		feedbacks: feedback,
		themes:    themes,
	}
	askStatus := &GetAskQagStatusUseCase{repo: info, now: a.Now}
	return &Service{
		a:           a,
		Info:        info,
		Supports:    supports,
		Feedbacks:   feedbacks,
		Updates:     &UpdatesRepository{a: a},
		DeleteLog:   &DeleteLogRepository{a: a},
		LowPriority: &LowPriorityRepository{a: a},
		Responses:   responses,

		Feedback:      feedback,
		Details:       &GetQagDetailsUseCase{aggregate: aggregate, supports: supports, feedbacks: feedback},
		PublicDetails: &GetPublicQagDetailsUseCase{aggregate: aggregate},
		AskStatus:     askStatus,
		ErrorText:     &GetQagErrorTextUseCase{messages: lg.ErrorMessages, status: askStatus},
		Insert:        &InsertQagUseCase{sanitize: defaultSanitize, qags: info, supports: supports, log: a.Log},
		Delete:        &DeleteQagUseCase{qags: info, supports: supports, logs: &DeleteLogRepository{a: a}},
		AdminUpdate:   &AdminUpdateQagStatusUseCase{qags: info},
		Keywords:      &GetQagByKeywordsUseCase{qags: info, themes: themes, supported: supports},
		suspicious:    lg,
	}
}

// ---------------------------------------------------------------------------
// Use cases exposed to the other slices
// ---------------------------------------------------------------------------

// AdminUpdateQagStatus is AdminUpdateQagStatusUseCase.updateQagStatus (PUT /admin/qags/{qagId}/status).
func (s *Service) AdminUpdateQagStatus(ctx context.Context, qagID string, newStatus QagStatus) (AdminUpdateQagStatusResult, error) {
	return s.AdminUpdate.UpdateQagStatus(ctx, qagID, newStatus)
}

// GetQagByKeywords is GetQagByKeywordsUseCase.getQagByKeywordsUseCase (GET /qags/search).
func (s *Service) GetQagByKeywords(ctx context.Context, userID string, keywords []string) ([]QagPreview, error) {
	return s.Keywords.GetQagByKeywords(ctx, userID, keywords)
}

// GetQagCount is GetQagCountUseCase.execute (GET /qags/count): the accepted QaGs.
func (s *Service) GetQagCount(ctx context.Context) (int, error) {
	return s.Info.GetQagsCount(ctx, nil)
}

// GetUserSupportedQagIDs is SupportQagUseCase.getUserSupportedQagIds.
func (s *Service) GetUserSupportedQagIDs(ctx context.Context, userID string) ([]string, error) {
	return s.Supports.GetUserSupportedQags(ctx, userID)
}

// GetSupportedQagCount is SupportQagUseCase.getSupportedQagCount.
func (s *Service) GetSupportedQagCount(ctx context.Context, userID string, thematiqueID *string) (int, error) {
	return s.Supports.GetSupportedQagCount(ctx, userID, thematiqueID)
}

// GetQagErrorText is GetQagErrorTextUseCase.getGetQagErrorText (GET /qags/ask_status).
func (s *Service) GetQagErrorText(ctx context.Context, userID string) (*string, error) {
	return s.ErrorText.GetQagErrorText(ctx, userID)
}

// GetFeedbackResults is FeedbackQagUseCase.getFeedbackResults (nil when the feature is off).
func (s *Service) GetFeedbackResults(ctx context.Context, qagID string) (*FeedbackResults, error) {
	return s.Feedback.GetFeedbackResults(ctx, qagID)
}

// EvictQag drops the cached shared aggregate of a QaG (number of supporters,
// status, ...) on every instance: call it after a direct SQL write that this
// module does not do itself. The repositories of this module do it on their own.
func (s *Service) EvictQag(ctx context.Context, qagUUID string) { s.Info.Evict(ctx, qagUUID) }
