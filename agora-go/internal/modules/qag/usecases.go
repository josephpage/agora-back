package qag

import (
	"context"
	"log/slog"
	"time"

	"agora/internal/sanitize"
)

// ---------------------------------------------------------------------------
// GetAskQagStatusUseCase, GetQagErrorTextUseCase, GetQagCountUseCase
// ---------------------------------------------------------------------------

// AskQagStatus is usecase/qag/AskQagStatus.
type AskQagStatus int

// The AskQagStatus constants (FeatureDisabled is never returned by Kotlin either).
const (
	AskFeatureDisabled AskQagStatus = iota
	AskWeeklyLimitReached
	AskEnabled
)

type lastQagReader interface {
	GetUserLastQagInfo(ctx context.Context, userID string) (*QagInfo, error)
}

// GetAskQagStatusUseCase is GetAskQagStatusUseCase.
type GetAskQagStatusUseCase struct {
	repo lastQagReader
	now  func() time.Time
}

// GetAskQagStatus is getAskQagStatus: one QaG per week, the week restarting on
// Monday at 10:00 (local time).
func (u *GetAskQagStatusUseCase) GetAskQagStatus(ctx context.Context, userID string) (AskQagStatus, error) {
	latest, err := u.repo.GetUserLastQagInfo(ctx, userID)
	if err != nil {
		return AskEnabled, err
	}
	switch {
	case latest == nil:
		return AskEnabled, nil
	case isDateWithinTheWeek(latest.Date, u.now()):
		return AskWeeklyLimitReached, nil
	}
	return AskEnabled, nil
}

// naive is the LocalDateTime of an instant: the wall clock of the process zone
// as a zone-less value (comparisons are by field, like LocalDateTime).
func naive(t time.Time) time.Time {
	l := t.In(time.Local)
	return time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), time.UTC)
}

// isDateWithinTheWeek is GetAskQagStatusUseCase.isDateWithinTheWeek. As in Kotlin,
// `withSecond(0)` keeps the nanoseconds of the clock, so the Monday 10:00 bound
// carries the sub-second part of "now" (LocalDateTime.now has microsecond
// precision on Linux).
func isDateWithinTheWeek(postDate, now time.Time) bool {
	post := naive(postDate)
	current := naive(now.Truncate(time.Microsecond))
	daysSinceMonday := (int(current.Weekday()) + 6) % 7
	monday := time.Date(current.Year(), current.Month(), current.Day()-daysSinceMonday, 10, 0, 0, current.Nanosecond(), time.UTC)
	var previous, next time.Time
	if current.Before(monday) {
		previous, next = monday.AddDate(0, 0, -7), monday
	} else { // after, or equal
		previous, next = monday, monday.AddDate(0, 0, 7)
	}
	return !post.Before(previous) && post.Before(next)
}

// ErrorMessages is the part of ErrorMessagesRepository used by GetQagErrorTextUseCase.
type ErrorMessages interface {
	QagDisabledErrorMessage() string
	QagErrorMessageOneByWeek() string
}

// GetQagErrorTextUseCase is GetQagErrorTextUseCase.
type GetQagErrorTextUseCase struct {
	messages ErrorMessages
	status   *GetAskQagStatusUseCase
}

// GetQagErrorText is getGetQagErrorText: nil when the user can ask a QaG.
func (u *GetQagErrorTextUseCase) GetQagErrorText(ctx context.Context, userID string) (*string, error) {
	status, err := u.status.GetAskQagStatus(ctx, userID)
	if err != nil {
		return nil, err
	}
	switch status {
	case AskFeatureDisabled:
		m := u.messages.QagDisabledErrorMessage()
		return &m, nil
	case AskWeeklyLimitReached:
		m := u.messages.QagErrorMessageOneByWeek()
		return &m, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// InsertQagUseCase, DeleteQagUseCase, AdminUpdateQagStatusUseCase
// ---------------------------------------------------------------------------

const (
	titleMaxLength       = 200
	descriptionMaxLength = 400
	usernameMaxLength    = 50
)

type qagInserter interface {
	InsertQagInfo(ctx context.Context, q QagInserting) (QagInsertionResult, error)
}

type supportInserter interface {
	InsertSupportQagUnchecked(ctx context.Context, s SupportQagInserting) (SupportQagResult, error)
}

// InsertQagUseCase is InsertQagUseCase. The sanitizer is ContentSanitizer.sanitize.
type InsertQagUseCase struct {
	sanitize func(content string, maxLength int) string
	qags     qagInserter
	supports supportInserter
	log      *slog.Logger
}

// InsertQag is insertQag: sanitize, insert, then add the author's own support.
func (u *InsertQagUseCase) InsertQag(ctx context.Context, q QagInserting) (QagInsertionResult, error) {
	q.Title = u.sanitize(q.Title, titleMaxLength)
	q.Description = u.sanitize(q.Description, descriptionMaxLength)
	q.Username = u.sanitize(q.Username, usernameMaxLength)
	result, err := u.qags.InsertQagInfo(ctx, q)
	if err != nil {
		return result, err
	}
	if !result.Success() {
		u.log.Error("⚠️ Insert QaG error")
		return result, nil
	}
	if _, err := u.supports.InsertSupportQagUnchecked(ctx, SupportQagInserting{QagID: result.Info.ID, UserID: q.UserID}); err != nil {
		return result, err
	}
	return result, nil
}

// defaultSanitize is ContentSanitizer.sanitize.
func defaultSanitize(content string, maxLength int) string {
	return sanitize.Sanitize(content, maxLength)
}

type qagReaderDeleter interface {
	GetQagInfo(ctx context.Context, qagID string) (*QagInfo, error)
	// deleteFound is deleteQag for a QaG that was just read: the second read of
	// the repository is skipped, the DELETE must still delete a row.
	deleteFound(ctx context.Context, qagUUID string, info *QagInfo) (QagDeleteResult, error)
}

type supportListDeleter interface {
	DeleteSupportListByQagID(ctx context.Context, qagID string) (SupportQagResult, error)
}

type deleteLogInserter interface {
	InsertQagDeleteLog(ctx context.Context, l QagDeleteLog) error
}

// DeleteQagUseCase is DeleteQagUseCase.
type DeleteQagUseCase struct {
	qags     qagReaderDeleter
	supports supportListDeleter
	logs     deleteLogInserter
}

// DeleteQagByID is deleteQagById: the author may delete their QaG unless it was
// selected for a response; its supports are deleted and the deletion is logged.
func (u *DeleteQagUseCase) DeleteQagByID(ctx context.Context, userID, qagID string) (QagDeleteResult, error) {
	info, err := u.qags.GetQagInfo(ctx, qagID)
	if err != nil {
		return QagDeleteResult{}, err
	}
	if info == nil || info.UserID != userID || info.Status == StatusSelectedForResponse {
		return QagDeleteResult{}, nil
	}
	result, err := u.qags.deleteFound(ctx, info.ID, info)
	if err != nil || !result.Success() {
		return result, err
	}
	if _, err := u.supports.DeleteSupportListByQagID(ctx, qagID); err != nil {
		return result, err
	}
	return result, u.logs.InsertQagDeleteLog(ctx, QagDeleteLog{UserID: userID, QagID: qagID})
}

// AdminUpdateQagStatusResult is the sealed class of AdminUpdateQagStatusUseCase.
type AdminUpdateQagStatusResult int

// The AdminUpdateQagStatusResult variants.
const (
	AdminUpdateSuccess AdminUpdateQagStatusResult = iota
	AdminUpdateNotFound
	AdminUpdateFailure
)

type qagStatusUpdater interface {
	GetQagInfo(ctx context.Context, qagID string) (*QagInfo, error)
	UpdateQagStatus(ctx context.Context, qagID string, newStatus QagStatus) (QagUpdateResult, error)
}

// AdminUpdateQagStatusUseCase is AdminUpdateQagStatusUseCase.
type AdminUpdateQagStatusUseCase struct{ qags qagStatusUpdater }

// UpdateQagStatus is updateQagStatus(qagId, newStatus).
func (u *AdminUpdateQagStatusUseCase) UpdateQagStatus(ctx context.Context, qagID string, newStatus QagStatus) (AdminUpdateQagStatusResult, error) {
	info, err := u.qags.GetQagInfo(ctx, qagID)
	if err != nil {
		return AdminUpdateFailure, err
	}
	if info == nil {
		return AdminUpdateNotFound, nil
	}
	result, err := u.qags.UpdateQagStatus(ctx, info.ID, newStatus)
	if err != nil {
		return AdminUpdateFailure, err
	}
	if result.Success() {
		return AdminUpdateSuccess, nil
	}
	return AdminUpdateFailure, nil
}

// ---------------------------------------------------------------------------
// QagPreviewMapper, GetQagByKeywordsUseCase
// ---------------------------------------------------------------------------

// ToPreview is QagPreviewMapper.toPreview(qag, thematique, isSupportedByUser, isAuthor).
func ToPreview(qag QagInfoWithSupportCount, th Thematique, isSupportedByUser, isAuthor bool) QagPreview {
	return QagPreview{
		ID:                qag.ID,
		Thematique:        th,
		Title:             qag.Title,
		Description:       qag.Description,
		Username:          qag.Username,
		Date:              qag.Date,
		SupportCount:      qag.SupportCount,
		IsSupportedByUser: isSupportedByUser,
		IsAuthor:          isAuthor,
		CanShare:          qag.Status == StatusModeratedAccepted || qag.Status == StatusSelectedForResponse,
	}
}

// ToPreviewWithThematique is QagPreviewMapper.toPreview(QagWithSupportCount, isSupportedByUser, isAuthor).
func ToPreviewWithThematique(qag QagWithSupportCount, isSupportedByUser, isAuthor bool) QagPreview {
	return ToPreview(qag.QagInfo, qag.Thematique, isSupportedByUser, isAuthor)
}

type keywordSearcher interface {
	GetQagByKeywordsList(ctx context.Context, keywords []string) ([]QagInfoWithSupportCount, error)
}

type supportedIDsReader interface {
	GetUserSupportedQagIDs(ctx context.Context, userID string) ([]string, error)
}

// GetQagByKeywordsUseCase is GetQagByKeywordsUseCase.
type GetQagByKeywordsUseCase struct {
	qags      keywordSearcher
	themes    thematiqueReader
	supported supportedIDsReader
}

// GetQagByKeywords is getQagByKeywordsUseCase(userId, keywords): the accepted QaGs
// matching every keyword, whose thematique exists. Kotlin read the user's
// supported QaG ids once per result; here it is read once, and only when a
// result has a thematique.
func (u *GetQagByKeywordsUseCase) GetQagByKeywords(ctx context.Context, userID string, keywords []string) ([]QagPreview, error) {
	list, err := u.qags.GetQagByKeywordsList(ctx, keywords)
	if err != nil {
		return nil, err
	}
	out := []QagPreview{}
	var supported map[string]bool
	for _, q := range list {
		th := u.themes.ByID(ctx, q.ThematiqueID)
		if th == nil {
			continue
		}
		if supported == nil {
			ids, err := u.supported.GetUserSupportedQagIDs(ctx, userID)
			if err != nil {
				return nil, err
			}
			supported = make(map[string]bool, len(ids))
			for _, id := range ids {
				supported[id] = true
			}
		}
		out = append(out, ToPreview(q, *th, supported[q.ID], userID == q.UserID))
	}
	return out, nil
}
