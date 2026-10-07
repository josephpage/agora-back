package consultation

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation/answered"
	"agora/internal/modules/login"
)

// Kotlin caches of ConsultationDetailsV2CacheRepositoryImpl.
const (
	// latestConsultationDetailsV2: default cache manager (1 hour), key = consultation id.
	latestDetailsCacheName = "latestConsultationDetailsV2"
	// consultationDetailsV2: default cache manager (1 hour); the unanswered users'
	// details live under "unanswered//<consultation id>".
	detailsCacheName = "consultationDetailsV2"
	detailsCacheTTL  = time.Hour
	// participantCountConsultationDetailsV2: shortTermCacheManager (5 minutes).
	participantCountCacheName = "participantCountConsultationDetailsV2"
	participantCountTTL       = 5 * time.Minute
	// the statistics of a feedback question, shared by every user (no Kotlin cache
	// outside the details above): micro-cache, evicted by every feedback.
	feedbackStatsCacheName = "consultationFeedbackStats"
)

// ConsultationNotFoundError is ConsultationNotFoundException (DefaultControllerAdvice: 404).
type ConsultationNotFoundError struct{ ConsultationID string }

func (e *ConsultationNotFoundError) Error() string {
	return "Consultation with id '" + e.ConsultationID + "' was not found"
}

// ConsultationUpdateNotFoundError is ConsultationUpdateNotFoundException (404).
type ConsultationUpdateNotFoundError struct{ ConsultationID, ConsultationUpdateID string }

func (e *ConsultationUpdateNotFoundError) Error() string {
	return "Consultation update with id '" + e.ConsultationUpdateID + "' for the consultation '" + e.ConsultationID + "' was not found"
}

type featureFlags interface {
	IsFeatureEnabled(ctx context.Context, feature login.Feature) (bool, error)
}

// DetailsUseCase is ConsultationDetailsV2UseCase + ConsultationDetailsUpdateV2UseCase.
type DetailsUseCase struct {
	a        *app.App
	flags    featureFlags
	info     *InfoRepository
	updates  *UpdateRepository
	answered *answered.Repository
	feedback *FeedbackRepository
	history  *HistoryRepository
}

// GetFeedbackStats is getFeedbackStats of ConsultationDetailsV2UseCase: the flag
// is read first, then the question is checked.
func (u *DetailsUseCase) feedbackStatsFlagFirst(ctx context.Context, update *UpdateInfo) (*FeedbackStats, error) {
	enabled, err := u.flags.IsFeatureEnabled(ctx, login.FeatureFeedbackConsultationUpdate)
	if err != nil {
		return nil, err
	}
	if !enabled || update.FeedbackQuestion == nil {
		return nil, nil
	}
	return u.feedback.GetFeedbackStats(ctx, update.ID)
}

// feedbackStatsQuestionFirst is getFeedbackStats of ConsultationDetailsUpdateV2UseCase:
// the question is checked first, then the flag. The statistics are shared by
// every reader for AGORA_MICROCACHE_TTL and dropped by every feedback.
func (u *DetailsUseCase) feedbackStatsQuestionFirst(ctx context.Context, update *UpdateInfo) (*FeedbackStats, error) {
	if update.FeedbackQuestion == nil {
		return nil, nil
	}
	enabled, err := u.flags.IsFeatureEnabled(ctx, login.FeatureFeedbackConsultationUpdate)
	if err != nil || !enabled {
		return nil, err
	}
	return loadCached(u.a, feedbackStatsCacheName, update.ID, u.a.Cfg.MicroCacheTTL, func() (*FeedbackStats, bool, error) {
		stats, err := u.feedback.GetFeedbackStats(context.WithoutCancel(ctx), update.ID)
		return stats, err == nil, err
	})
}

// participantCount is getParticipantCount: the count of distinct participants,
// shared for 5 minutes. Kotlin writes the cached value back at every call, which
// restarts its 5 minutes: the count never changed while the page was visited at
// least every 5 minutes (class B, S4-B3). Here it is recomputed every 5 minutes.
func (u *DetailsUseCase) participantCount(ctx context.Context, consultationID string) (int, error) {
	return loadCached(u.a, participantCountCacheName, consultationID, participantCountTTL, func() (int, bool, error) {
		n, err := u.answered.GetParticipantCount(context.WithoutCancel(ctx), consultationID)
		return n, err == nil, err
	})
}

// userState reads, in one query, whether the user answered the consultation and
// the feedback of the user on an update (updateID nil: none asked). A user id
// that is not a UUID is neither (no query). A feedback row with a NULL date
// crashes the Kotlin entity mapping.
func (u *DetailsUseCase) userState(ctx context.Context, consultationID, userID string, updateID *string) (answered bool, feedback *bool, err error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return false, nil, nil
	}
	var positive *int32
	var broken *bool
	err = u.a.DB.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_answered_consultation
            WHERE consultation_id = $1
            AND user_id = $2), f.is_positive, (f.created_date IS NULL OR f.updated_date IS NULL)
        FROM (SELECT 1) x LEFT JOIN LATERAL (SELECT is_positive, created_date, updated_date FROM feedbacks_consultation_update
            WHERE consultation_update_id = $3
            AND user_id = $2
            LIMIT 1) f ON true`, consultationID, uid, updateID).Scan(&answered, &positive, &broken)
	if err != nil {
		return false, nil, err
	}
	if positive == nil {
		return answered, nil, nil
	}
	if broken != nil && *broken {
		return false, nil, errNullColumn("feedbacks_consultation_update", "created_date/updated_date")
	}
	b := int(*positive) == isPositiveTrueValue
	return answered, &b, nil
}

// GetConsultation is ConsultationDetailsV2UseCase.getConsultation(consultationIdOrSlug).
// userID is the authenticated user, nil when anonymous. A ConsultationNotFoundError
// is returned for an unknown consultation.
func (u *DetailsUseCase) GetConsultation(ctx context.Context, idOrSlug string, userID *string) (*DetailsWithInfo, error) {
	info := u.info.GetConsultationByIDOrSlugWithUnpublished(ctx, idOrSlug)
	if info == nil {
		return nil, &ConsultationNotFoundError{ConsultationID: idOrSlug}
	}
	history, err := u.history.GetConsultationUpdateHistory(ctx, info.ID)
	if err != nil {
		return nil, err
	}

	// has the user answered (and, when the latest update asks for it, the feedback)?
	// The latest update is looked up in the cache to ask for both at once.
	var answered bool
	var feedback *bool
	var feedbackRead bool
	if userID != nil {
		var guess *string
		if v, ok := u.a.Cache.Get(latestDetailsCacheName, info.ID); ok {
			if d := v.(*Details); d.Update.FeedbackQuestion != nil {
				guess = &d.Update.ID
			}
		}
		answered, feedback, err = u.userState(ctx, info.ID, *userID, guess)
		if err != nil {
			return nil, err
		}
		feedbackRead = guess != nil
		if guess == nil {
			feedback = nil
		}
		_ = feedbackRead
		details, err := u.consultationDetails(ctx, info, answered)
		if err != nil {
			return nil, err
		}
		participants, err := u.participantCount(ctx, details.Consultation.ID)
		if err != nil {
			return nil, err
		}
		// getUserFeedback
		var userFeedback *bool
		if details.Update.FeedbackQuestion != nil {
			if guess != nil && *guess == details.Update.ID {
				userFeedback = feedback
			} else {
				id := details.Update.ID
				_, userFeedback, err = u.userState(ctx, info.ID, *userID, &id)
				if err != nil {
					return nil, err
				}
			}
		}
		return &DetailsWithInfo{
			Consultation:           details.Consultation,
			Update:                 details.Update,
			FeedbackStats:          details.FeedbackStats,
			History:                history,
			ParticipantCount:       participants,
			IsUserFeedbackPositive: userFeedback,
			IsAnsweredByUser:       answered,
		}, nil
	}

	details, err := u.consultationDetails(ctx, info, false)
	if err != nil {
		return nil, err
	}
	participants, err := u.participantCount(ctx, details.Consultation.ID)
	if err != nil {
		return nil, err
	}
	return &DetailsWithInfo{
		Consultation:     details.Consultation,
		Update:           details.Update,
		FeedbackStats:    details.FeedbackStats,
		History:          history,
		ParticipantCount: participants,
	}, nil
}

// consultationDetails is getConsultationDetails: the unanswered users' view of an
// ongoing consultation, else the latest update.
func (u *DetailsUseCase) consultationDetails(ctx context.Context, info *ConsultationInfo, userHasAnswered bool) (*Details, error) {
	now := FromTime(u.a.Now())
	var d *Details
	var err error
	if info.IsOngoing(now) && !userHasAnswered {
		d, err = u.unansweredDetails(ctx, info)
	} else {
		d, err = u.lastDetails(ctx, info)
	}
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, &ConsultationNotFoundError{ConsultationID: info.ID}
	}
	return d, nil
}

func (u *DetailsUseCase) unansweredDetails(ctx context.Context, info *ConsultationInfo) (*Details, error) {
	return loadCached(u.a, detailsCacheName, "unanswered//"+info.ID, coexistenceCap(u.a, detailsCacheTTL), func() (*Details, bool, error) {
		lctx := context.WithoutCancel(ctx)
		update := u.updates.GetUnansweredUsersConsultationUpdateWithUnpublished(lctx, info.ID)
		if update == nil {
			return nil, false, nil
		}
		stats, err := u.feedbackStatsFlagFirst(lctx, update)
		if err != nil {
			return nil, false, err
		}
		return &Details{Consultation: info, Update: update, FeedbackStats: stats}, true, nil
	})
}

func (u *DetailsUseCase) lastDetails(ctx context.Context, info *ConsultationInfo) (*Details, error) {
	return loadCached(u.a, latestDetailsCacheName, info.ID, coexistenceCap(u.a, detailsCacheTTL), func() (*Details, bool, error) {
		lctx := context.WithoutCancel(ctx)
		update := u.updates.GetLatestConsultationUpdate(lctx, info.ID)
		if update == nil {
			return nil, false, nil
		}
		stats, err := u.feedbackStatsFlagFirst(lctx, update)
		if err != nil {
			return nil, false, err
		}
		return &Details{Consultation: info, Update: update, FeedbackStats: stats}, true, nil
	})
}

// GetConsultationUpdate is ConsultationDetailsUpdateV2UseCase.getConsultationUnpublishedDetailsUpdate:
// one update of a consultation (by id or slug), nothing of it is cached.
func (u *DetailsUseCase) GetConsultationUpdate(ctx context.Context, idOrSlug, updateIDOrSlug string, userID *string) (*DetailsWithInfo, error) {
	info := u.info.GetConsultationByIDOrSlugWithUnpublished(ctx, idOrSlug)
	if info == nil {
		return nil, &ConsultationUpdateNotFoundError{ConsultationID: idOrSlug, ConsultationUpdateID: updateIDOrSlug}
	}
	update := u.updates.GetConsultationUpdateBySlugOrIDWithUnpublished(ctx, info.ID, updateIDOrSlug)
	if update == nil {
		return nil, &ConsultationUpdateNotFoundError{ConsultationID: idOrSlug, ConsultationUpdateID: updateIDOrSlug}
	}

	var answered bool
	var userFeedback *bool
	if userID != nil {
		var updateID *string
		if update.FeedbackQuestion != nil {
			updateID = &update.ID
		}
		var err error
		answered, userFeedback, err = u.userState(ctx, info.ID, *userID, updateID)
		if err != nil {
			return nil, err
		}
	}

	history, err := u.history.GetConsultationUpdateHistory(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	stats, err := u.feedbackStatsQuestionFirst(ctx, update)
	if err != nil {
		return nil, err
	}
	participants, err := u.participantCount(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	return &DetailsWithInfo{
		Consultation:           info,
		Update:                 update,
		FeedbackStats:          stats,
		History:                history,
		ParticipantCount:       participants,
		IsUserFeedbackPositive: userFeedback,
		IsAnsweredByUser:       answered,
	}, nil
}
