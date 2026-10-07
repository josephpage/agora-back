package qag

import (
	"fmt"
	"time"
)

// QagStatus is domain/QagStatus (declaration order of the Kotlin enum).
type QagStatus int

// The QagStatus constants.
const (
	StatusOpen QagStatus = iota
	StatusArchived
	StatusModeratedAccepted
	StatusModeratedRejected
	StatusSelectedForResponse
)

// String is the Kotlin constant name (QagStatus.name).
func (s QagStatus) String() string {
	switch s {
	case StatusOpen:
		return "OPEN"
	case StatusArchived:
		return "ARCHIVED"
	case StatusModeratedAccepted:
		return "MODERATED_ACCEPTED"
	case StatusModeratedRejected:
		return "MODERATED_REJECTED"
	case StatusSelectedForResponse:
		return "SELECTED_FOR_RESPONSE"
	}
	return fmt.Sprintf("QagStatus(%d)", int(s))
}

// ParseStatus is QagStatus.valueOf(name): exact match only.
func ParseStatus(name string) (QagStatus, bool) {
	for _, s := range []QagStatus{StatusOpen, StatusArchived, StatusModeratedAccepted, StatusModeratedRejected, StatusSelectedForResponse} {
		if s.String() == name {
			return s, true
		}
	}
	return 0, false
}

// Database values of qags.status / qag_updates.status (QagInfoMapper, QagUpdatesMapper).
const (
	dbStatusOpen              = 0
	dbStatusArchived          = 2
	dbStatusModeratedRejected = -1
	dbStatusModeratedAccepted = 1
	dbStatusSelected          = 7
)

// invalidStatusError is the IllegalArgumentException("Invalid QaG status : n") of the mappers.
type invalidStatusError struct{ status int }

func (e *invalidStatusError) Error() string {
	return fmt.Sprintf("java.lang.IllegalArgumentException: Invalid QaG status : %d", e.status)
}

func statusFromDB(status int) (QagStatus, error) {
	switch status {
	case dbStatusOpen:
		return StatusOpen, nil
	case dbStatusArchived:
		return StatusArchived, nil
	case dbStatusModeratedAccepted:
		return StatusModeratedAccepted, nil
	case dbStatusModeratedRejected:
		return StatusModeratedRejected, nil
	case dbStatusSelected:
		return StatusSelectedForResponse, nil
	}
	return 0, &invalidStatusError{status}
}

// StatusToDB is QagInfoMapper.toIntStatus.
func StatusToDB(s QagStatus) int {
	switch s {
	case StatusOpen:
		return dbStatusOpen
	case StatusArchived:
		return dbStatusArchived
	case StatusModeratedAccepted:
		return dbStatusModeratedAccepted
	case StatusModeratedRejected:
		return dbStatusModeratedRejected
	case StatusSelectedForResponse:
		return dbStatusSelected
	}
	panic(fmt.Sprintf("unknown status %d", int(s)))
}

// QagInfo is usecase/qag/repository/QagInfo.
type QagInfo struct {
	ID           string
	ThematiqueID string
	Title        string
	Description  string
	Date         time.Time
	Status       QagStatus
	Username     string
	UserID       string
}

// QagInfoWithSupportCount is usecase/qag/repository/QagInfoWithSupportCount.
// ModeratedDate is only set by getTrendingQagsV3.
type QagInfoWithSupportCount struct {
	ID            string
	ThematiqueID  string
	Title         string
	Description   string
	Date          time.Time
	Status        QagStatus
	Username      string
	UserID        string
	SupportCount  int
	ModeratedDate *time.Time
}

// equal is the data class equals (java.util.Date equality = same instant).
func (q QagInfoWithSupportCount) equal(o QagInfoWithSupportCount) bool {
	if q.ID != o.ID || q.ThematiqueID != o.ThematiqueID || q.Title != o.Title || q.Description != o.Description ||
		!q.Date.Equal(o.Date) || q.Status != o.Status || q.Username != o.Username || q.UserID != o.UserID ||
		q.SupportCount != o.SupportCount {
		return false
	}
	if (q.ModeratedDate == nil) != (o.ModeratedDate == nil) {
		return false
	}
	return q.ModeratedDate == nil || q.ModeratedDate.Equal(*o.ModeratedDate)
}

// QagInserting is domain/QagInserting.
type QagInserting struct {
	ThematiqueID string
	Title        string
	Description  string
	Date         time.Time
	Status       QagStatus
	Username     string
	UserID       string
}

// QagInsertionResult is the sealed class of the same name: Info is nil on Failure.
type QagInsertionResult struct{ Info *QagInfo }

// Success reports QagInsertionResult.Success.
func (r QagInsertionResult) Success() bool { return r.Info != nil }

// QagUpdateResult is QagUpdateResult: Info is nil on Failure.
type QagUpdateResult struct{ Info *QagInfo }

// Success reports QagUpdateResult.Success.
func (r QagUpdateResult) Success() bool { return r.Info != nil }

// QagDeleteResult is QagDeleteResult: Info is nil on Failure.
type QagDeleteResult struct{ Info *QagInfo }

// Success reports QagDeleteResult.Success.
func (r QagDeleteResult) Success() bool { return r.Info != nil }

// SupportQagResult is usecase/supportQag/repository/SupportQagResult.
type SupportQagResult int

// The SupportQagResult constants.
const (
	SupportSuccess SupportQagResult = iota
	SupportFailure
)

// SupportQagInserting / SupportQagDeleting are domain/SupportQag.
type (
	SupportQagInserting struct{ QagID, UserID string }
	SupportQagDeleting  struct{ QagID, UserID string }
)

// FeedbackQagInserting is domain/FeedbackQag.
type FeedbackQagInserting struct {
	QagID     string
	UserID    string
	IsHelpful bool
}

// FeedbackQag is domain/FeedbackQag (a stored feedback).
type FeedbackQag struct {
	QagID     string
	UserID    string
	IsHelpful bool
}

// FeedbackResults is domain/FeedbackResults.
type FeedbackResults struct {
	PositiveRatio int
	NegativeRatio int
	Count         int
}

// FeedbackQagResult is usecase/feedbackQag/repository/FeedbackQagResult.
type FeedbackQagResult int

// The FeedbackQagResult constants.
const (
	FeedbackSuccess FeedbackQagResult = iota
	FeedbackFailure
)

// InsertFeedbackQagResult is the sealed class of the same name.
type InsertFeedbackQagResult int

// The InsertFeedbackQagResult variants.
const (
	InsertFeedbackFailure InsertFeedbackQagResult = iota
	InsertFeedbackSuccess
	InsertFeedbackSuccessDisabled
)

// QagDeleteLog is domain/QagDeleteLog.
type QagDeleteLog struct{ UserID, QagID string }

// QagInsertingUpdates is domain/QagInsertingUpdates.
type QagInsertingUpdates struct {
	QagID        string
	NewQagStatus QagStatus
	UserID       string
	Reason       *string
	ShouldDelete bool
	MotifID      *string
}

// QagUpdates is domain/QagUpdates.
type QagUpdates struct {
	QagID         string
	QagStatus     QagStatus
	UserID        string
	ModeratedDate time.Time
}

// ResponseQag is the sealed class domain/ResponseQag: exactly one of Video / Text is set.
type ResponseQag struct {
	Video *ResponseQagVideo
	Text  *ResponseQagText
}

// ResponseQagVideo is domain/ResponseQagVideo.
type ResponseQagVideo struct {
	Author            string
	AuthorPortraitURL string
	ResponseDate      time.Time
	FeedbackQuestion  string
	QagID             string
	AuthorFunction    *string
	AuthorDescription string
	VideoURL          string
	VideoTitle        string
	VideoWidth        int
	VideoHeight       int
	Transcription     string
	AdditionalInfo    *ResponseQagAdditionalInfo
}

// ResponseQagText is domain/ResponseQagText.
type ResponseQagText struct {
	Author            string
	AuthorPortraitURL string
	ResponseDate      time.Time
	FeedbackQuestion  string
	QagID             string
	AuthorFunction    *string
	ResponseLabel     string
	ResponseText      string
}

// ResponseQagAdditionalInfo is domain/ResponseQagAdditionalInfo.
type ResponseQagAdditionalInfo struct {
	AdditionalInfoTitle       string
	AdditionalInfoDescription string
}

// QagDetails is domain/QagDetails. Thematique is the thematique of the QaG.
type QagDetails struct {
	ID              string
	Thematique      Thematique
	Title           string
	Description     string
	Date            time.Time
	Status          QagStatus
	Username        string
	UserID          string
	SupportCount    int
	Response        *ResponseQag
	FeedbackResults *FeedbackResults
}

// QagWithUserData is domain/QagWithUserData.
type QagWithUserData struct {
	QagDetails        QagDetails
	CanShare          bool
	CanSupport        bool
	CanDelete         bool
	IsAuthor          bool
	IsSupportedByUser bool
	IsHelpful         *bool
}

// QagPreview is domain/QagPreview.
type QagPreview struct {
	ID                string
	Thematique        Thematique
	Title             string
	Description       string
	Username          string
	Date              time.Time
	SupportCount      int
	IsSupportedByUser bool
	IsAuthor          bool
	CanShare          bool
}

// QagWithSupportCount is domain/QagWithSupportCount.
type QagWithSupportCount struct {
	QagInfo    QagInfoWithSupportCount
	Thematique Thematique
}
