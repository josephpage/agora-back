package qag

import (
	"agora/internal/common"
	"time"

	"agora/internal/modules/thematique"
	"agora/internal/store"
)

// Thematique is domain/Thematique (owned by the thematique module).
type Thematique = thematique.Thematique

// ---------------------------------------------------------------------------
// infrastructure/qag/QagJson.kt
// ---------------------------------------------------------------------------

// QagJSON is QagJson (@JsonInclude(NON_NULL)): GET /qags/{qagId}.
type QagJSON struct {
	ID           string                        `json:"id,omitnull"`
	Thematique   thematique.ThematiqueNoIDJSON `json:"thematique,omitnull"`
	Title        string                        `json:"title,omitnull"`
	Description  string                        `json:"description,omitnull"`
	Date         string                        `json:"date,omitnull"`
	Username     string                        `json:"username,omitnull"`
	CanShare     bool                          `json:"canShare,omitnull"`
	CanSupport   bool                          `json:"canSupport,omitnull"`
	CanDelete    bool                          `json:"canDelete,omitnull"`
	Support      *SupportQagJSON               `json:"support,omitnull"`
	IsAuthor     bool                          `json:"isAuthor,omitnull"`
	Response     *ResponseQagVideoJSON         `json:"response,omitnull"`
	TextResponse *ResponseQagTextJSON          `json:"textResponse,omitnull"`
}

// JavaName is the XML root element.
func (QagJSON) JavaName() string { return "QagJson" }

// SupportQagJSON is SupportQagJson (no @JsonInclude).
type SupportQagJSON struct {
	SupportCount      int  `json:"count"`
	IsSupportedByUser bool `json:"isSupported"`
}

// ResponseQagVideoJSON is ResponseQagVideoJson (@JsonInclude(NON_NULL)).
type ResponseQagVideoJSON struct {
	Author               string               `json:"author,omitnull"`
	AuthorDescription    string               `json:"authorDescription,omitnull"`
	ResponseDate         string               `json:"responseDate,omitnull"`
	VideoURL             string               `json:"videoUrl,omitnull"`
	VideoTitle           string               `json:"videoTitle,omitnull"`
	VideoWidth           int                  `json:"videoWidth,omitnull"`
	VideoHeight          int                  `json:"videoHeight,omitnull"`
	Transcription        string               `json:"transcription,omitnull"`
	FeedbackQuestion     string               `json:"feedbackQuestion,omitnull"`
	FeedbackUserResponse *bool                `json:"feedbackUserResponse,omitnull"`
	FeedbackResults      *FeedbackResultsJSON `json:"feedbackResults,omitnull"`
	AdditionalInfo       *AdditionalInfoJSON  `json:"additionalInfo,omitnull"`
}

// ResponseQagTextJSON is ResponseQagTextJson (@JsonInclude(NON_NULL)).
type ResponseQagTextJSON struct {
	ResponseLabel        string               `json:"responseLabel,omitnull"`
	ResponseText         string               `json:"responseText,omitnull"`
	FeedbackQuestion     string               `json:"feedbackQuestion,omitnull"`
	FeedbackUserResponse *bool                `json:"feedbackUserResponse,omitnull"`
	FeedbackResults      *FeedbackResultsJSON `json:"feedbackResults,omitnull"`
}

// FeedbackResultsJSON is FeedbackResultsJson (also the body of POST /qags/{id}/feedback).
type FeedbackResultsJSON struct {
	PositiveRatio int `json:"positiveRatio"`
	NegativeRatio int `json:"negativeRatio"`
	Count         int `json:"count"`
}

// JavaName is the XML root element.
func (FeedbackResultsJSON) JavaName() string { return "FeedbackResultsJson" }

// AdditionalInfoJSON is AdditionalInfoJson.
type AdditionalInfoJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ---------------------------------------------------------------------------
// infrastructure/qag/QagInsertingJson.kt
// ---------------------------------------------------------------------------

// QagInsertingJSON is QagInsertingJson (request body of POST /qags): four
// required strings (Jackson coerces numbers and booleans to String).
type QagInsertingJSON struct {
	Title        string `json:"title"`
	ThematiqueID string `json:"thematiqueId"`
	Description  string `json:"description"`
	Author       string `json:"author"`
}

// QagInsertionResultJSON is QagInsertionResultJson.
type QagInsertionResultJSON struct {
	QagID string `json:"qagId"`
}

// JavaName is the XML root element.
func (QagInsertionResultJSON) JavaName() string { return "QagInsertionResultJson" }

// FeedbackQagJSON is feedbackQag/FeedbackQagJson (request body): `isHelpful`
// is a JVM primitive, so a missing or null value is false.
type FeedbackQagJSON struct {
	IsHelpful bool `json:"isHelpful"`
}

// QagAskStatusJSON is qagHome/QagAskStatusJson (GET /qags/ask_status).
type QagAskStatusJSON struct {
	AskQagErrorText *string `json:"askQagErrorText"`
}

// JavaName is the XML root element.
func (QagAskStatusJSON) JavaName() string { return "QagAskStatusJson" }

// ---------------------------------------------------------------------------
// infrastructure/qag/PublicQagJson.kt (no @JsonInclude: nulls are written)
// ---------------------------------------------------------------------------

// PublicQagJSON is PublicQagJson: GET /api/public/qags/{qagId}.
type PublicQagJSON struct {
	ID            string                        `json:"id"`
	Status        string                        `json:"status"`
	Thematique    thematique.ThematiqueNoIDJSON `json:"thematique"`
	Title         string                        `json:"title"`
	Description   string                        `json:"description"`
	Date          string                        `json:"date"`
	Username      string                        `json:"username"`
	SupportCount  int                           `json:"supportCount"`
	VideoResponse *PublicQagResponseVideoJSON   `json:"response"`
	TextResponse  *PublicQagResponseTextJSON    `json:"textResponse"`
}

// JavaName is the XML root element.
func (PublicQagJSON) JavaName() string { return "PublicQagJson" }

// PublicQagResponseVideoJSON is PublicQagResponseVideoJson.
type PublicQagResponseVideoJSON struct {
	Author            string                                    `json:"author"`
	AuthorDescription string                                    `json:"authorDescription"`
	AuthorPortraitURL string                                    `json:"authorPortraitUrl"`
	ResponseDate      string                                    `json:"responseDate"`
	VideoURL          string                                    `json:"videoUrl"`
	VideoTitle        string                                    `json:"videoTitle"`
	VideoWidth        int                                       `json:"videoWidth"`
	VideoHeight       int                                       `json:"videoHeight"`
	Transcription     string                                    `json:"transcription"`
	AdditionalInfo    *PublicQagResponseVideoAdditionalInfoJSON `json:"additionalInfo"`
	FeedbackQuestion  string                                    `json:"feedbackQuestion"`
}

// PublicQagResponseVideoAdditionalInfoJSON is PublicQagResponseVideoAdditionalInfoJson.
type PublicQagResponseVideoAdditionalInfoJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// PublicQagResponseTextJSON is PublicQagResponseTextJson.
type PublicQagResponseTextJSON struct {
	ResponseLabel    string `json:"responseLabel"`
	ResponseText     string `json:"responseText"`
	FeedbackQuestion string `json:"feedbackQuestion"`
}

// ---------------------------------------------------------------------------
// Mappers
// ---------------------------------------------------------------------------

// unescapeLineBreaks is StringUtils.unescapeLineBreaks.
func unescapeLineBreaks(s string) string { return replaceAll(s, `\n`, "\n") }

func replaceAll(s, old, repl string) string {
	// strings.ReplaceAll semantics (Kotlin String.replace with a non-empty target)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if len(s)-i >= len(old) && s[i:i+len(old)] == old {
			out = append(out, repl...)
			i += len(old)
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

func feedbackResultsJSON(f *FeedbackResults) *FeedbackResultsJSON {
	if f == nil {
		return nil
	}
	return &FeedbackResultsJSON{PositiveRatio: f.PositiveRatio, NegativeRatio: f.NegativeRatio, Count: f.Count}
}

// ToQagJSON is QagJsonMapper.toJson(QagWithUserData).
func ToQagJSON(q QagWithUserData) QagJSON {
	d := q.QagDetails
	j := QagJSON{
		ID:          d.ID,
		Thematique:  thematique.ToNoIDJSON(d.Thematique),
		Title:       d.Title,
		Description: d.Description,
		Date:        common.FormatDate(d.Date),
		Username:    d.Username,
		CanShare:    q.CanShare,
		CanSupport:  q.CanSupport,
		CanDelete:   q.CanDelete,
		Support:     &SupportQagJSON{SupportCount: d.SupportCount, IsSupportedByUser: q.IsSupportedByUser},
		IsAuthor:    q.IsAuthor,
	}
	if r := d.Response; r != nil {
		if v := r.Video; v != nil {
			vj := &ResponseQagVideoJSON{
				Author:               v.Author,
				AuthorDescription:    v.AuthorDescription,
				ResponseDate:         common.FormatDate(v.ResponseDate),
				VideoURL:             v.VideoURL,
				VideoTitle:           v.VideoTitle,
				VideoWidth:           v.VideoWidth,
				VideoHeight:          v.VideoHeight,
				Transcription:        unescapeLineBreaks(v.Transcription),
				FeedbackQuestion:     v.FeedbackQuestion,
				FeedbackUserResponse: q.IsHelpful,
				FeedbackResults:      feedbackResultsJSON(d.FeedbackResults),
			}
			if v.AdditionalInfo != nil {
				vj.AdditionalInfo = &AdditionalInfoJSON{
					Title:       v.AdditionalInfo.AdditionalInfoTitle,
					Description: v.AdditionalInfo.AdditionalInfoDescription,
				}
			}
			j.Response = vj
		}
		if t := r.Text; t != nil {
			j.TextResponse = &ResponseQagTextJSON{
				ResponseLabel:        t.ResponseLabel,
				ResponseText:         t.ResponseText,
				FeedbackQuestion:     t.FeedbackQuestion,
				FeedbackUserResponse: q.IsHelpful,
				FeedbackResults:      feedbackResultsJSON(d.FeedbackResults),
			}
		}
	}
	return j
}

// ToPublicQagJSON is PublicQagJsonMapper.toJson.
func ToPublicQagJSON(d QagDetails) PublicQagJSON {
	j := PublicQagJSON{
		ID:           d.ID,
		Status:       publicStatus(d),
		Thematique:   thematique.ToNoIDJSON(d.Thematique),
		Title:        d.Title,
		Description:  d.Description,
		Date:         common.FormatDate(d.Date),
		Username:     d.Username,
		SupportCount: d.SupportCount,
	}
	if r := d.Response; r != nil {
		if v := r.Video; v != nil {
			vj := &PublicQagResponseVideoJSON{
				Author:            v.Author,
				AuthorDescription: v.AuthorDescription,
				AuthorPortraitURL: v.AuthorPortraitURL,
				ResponseDate:      common.FormatDate(v.ResponseDate),
				VideoURL:          v.VideoURL,
				VideoTitle:        v.VideoTitle,
				VideoWidth:        v.VideoWidth,
				VideoHeight:       v.VideoHeight,
				Transcription:     v.Transcription,
				FeedbackQuestion:  v.FeedbackQuestion,
			}
			if v.AdditionalInfo != nil {
				vj.AdditionalInfo = &PublicQagResponseVideoAdditionalInfoJSON{
					Title:       v.AdditionalInfo.AdditionalInfoTitle,
					Description: v.AdditionalInfo.AdditionalInfoDescription,
				}
			}
			j.VideoResponse = vj
		}
		if t := r.Text; t != nil {
			j.TextResponse = &PublicQagResponseTextJSON{
				ResponseLabel:    t.ResponseLabel,
				ResponseText:     t.ResponseText,
				FeedbackQuestion: t.FeedbackQuestion,
			}
		}
	}
	return j
}

// publicStatus is PublicQagJsonMapper.toResponseStatusJson.
func publicStatus(d QagDetails) string {
	if d.Response != nil {
		return "responseAvailable"
	}
	if d.Status == StatusSelectedForResponse {
		return "selectedForResponse"
	}
	return "openForSupport"
}

// ToFeedbackResultsJSON is FeedbackJsonMapper.toJson.
func ToFeedbackResultsJSON(f FeedbackResults) FeedbackResultsJSON {
	return FeedbackResultsJSON{PositiveRatio: f.PositiveRatio, NegativeRatio: f.NegativeRatio, Count: f.Count}
}

// ToSupportQagJSON builds the SupportQagJson written by the QaG previews.
func ToSupportQagJSON(supportCount int, isSupportedByUser bool) SupportQagJSON {
	return SupportQagJSON{SupportCount: supportCount, IsSupportedByUser: isSupportedByUser}
}

// toDomain is QagJsonMapper.toDomain(json, userId): the date is Calendar.getInstance().time.
func (j QagInsertingJSON) toDomain(userID string, now func() time.Time) QagInserting {
	return QagInserting{
		ThematiqueID: j.ThematiqueID,
		Title:        j.Title,
		Description:  j.Description,
		Date:         store.Millis(now()),
		Status:       StatusOpen,
		Username:     j.Author,
		UserID:       userID,
	}
}
