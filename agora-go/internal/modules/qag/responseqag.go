package qag

import (
	"context"
	"errors"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/jsonjava"
	"agora/internal/strapi"
)

// This file is the part of the response QaG repository (S3: ResponseQagRepositoryImpl,
// ResponseQagStrapiRepository, ResponseQagMapper, StrapiResponseQagDTO) that the QaG
// details need: the response of ONE QaG.

// strapiResponseQag is StrapiResponseQag. Decoding follows jsonjava (Jackson):
// a missing required field anywhere makes the whole Strapi list empty.
type strapiResponseQag struct {
	Auteur            string               `json:"auteur"`
	AuteurPortraitURL string               `json:"auteurPortraitUrl"`
	AuteurFonction    *string              `json:"auteurFonction"`
	ReponseDate       strapiLocalDate      `json:"reponseDate"`
	FeedbackQuestion  string               `json:"feedbackQuestion"`
	QuestionID        string               `json:"questionId"`
	ReponseType       []strapiResponseType `json:"reponseType"`
	AuteurPortrait    *strapi.MediaPicture `json:"auteurPortrait"`
}

// getAuthorPortraitURL is StrapiResponseQag.getAuthorPortraitUrl.
func (r *strapiResponseQag) getAuthorPortraitURL() string {
	if r.AuteurPortrait != nil {
		return r.AuteurPortrait.MediaURL()
	}
	return r.AuteurPortraitURL
}

// strapiResponseType is the sealed interface StrapiResponseQagType, read from the
// "__component" property (@JsonTypeInfo NAME / EXISTING_PROPERTY, no default
// implementation: an unknown or missing name fails the decoding).
type strapiResponseType struct {
	Text  *strapiResponseQagText
	Video *strapiResponseQagVideo
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler.
func (t *strapiResponseType) UnmarshalJavaTree(tree any) error {
	obj, ok := tree.(map[string]any)
	if !ok {
		return errors.New("response type: expected object")
	}
	name, _ := obj["__component"].(string)
	switch name {
	case "reponse.reponsetextuelle":
		var v strapiResponseQagText
		if err := jsonjava.UnmarshalTree(tree, &v); err != nil {
			return err
		}
		*t = strapiResponseType{Text: &v}
	case "reponse.reponse-video":
		var v strapiResponseQagVideo
		if err := jsonjava.UnmarshalTree(tree, &v); err != nil {
			return err
		}
		*t = strapiResponseType{Video: &v}
	default:
		return errors.New("response type: unknown or missing __component")
	}
	return nil
}

// strapiResponseQagText is StrapiResponseQagText.
type strapiResponseQagText struct {
	Label string          `json:"label"`
	Text  strapi.RichText `json:"text"`
}

// strapiResponseQagVideo is StrapiResponseQagVideo.
type strapiResponseQagVideo struct {
	AuteurDescription                   string             `json:"auteurDescription"`
	URLVideo                            string             `json:"urlVideo"`
	VideoWidth                          int                `json:"videoWidth"`
	VideoHeight                         int                `json:"videoHeight"`
	Transcription                       string             `json:"transcription"`
	InformationAdditionnelleTitre       *string            `json:"informationAdditionnelleTitre"`
	PageTitle                           string             `json:"page_title"`
	InformationAdditionnelleDescription *strapi.RichText   `json:"informationAdditionnelleDescription"`
	Video                               *strapi.MediaVideo `json:"video"`
}

func (v *strapiResponseQagVideo) hasInformationAdditionnelle() bool {
	return v.InformationAdditionnelleTitre != nil && !javacompat.KotlinIsBlank(*v.InformationAdditionnelleTitre) &&
		v.InformationAdditionnelleDescription != nil && len(*v.InformationAdditionnelleDescription) > 0
}

func (v *strapiResponseQagVideo) getVideoURL() string {
	if v.Video != nil {
		return v.Video.URL
	}
	return v.URLVideo
}

// strapiLocalDate is a java.time.LocalDate read by Jackson's LocalDateDeserializer.
type strapiLocalDate struct{ Year, Month, Day int }

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler: "yyyy-MM-dd" (ISO_LOCAL_DATE,
// strict), or a date-time string whose date is kept ("...Z" read as UTC).
func (d *strapiLocalDate) UnmarshalJavaTree(tree any) error {
	s, ok := tree.(string)
	if !ok {
		return errors.New("local date: expected string")
	}
	if len(s) > 10 && s[10] == 'T' {
		var t time.Time
		var err error
		if strings.HasSuffix(s, "Z") {
			t, err = time.Parse(time.RFC3339Nano, s)
			t = t.UTC()
		} else {
			t, err = time.Parse("2006-01-02T15:04:05.999999999", s)
		}
		if err != nil {
			return err
		}
		*d = strapiLocalDate{t.Year(), int(t.Month()), t.Day()}
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	*d = strapiLocalDate{t.Year(), int(t.Month()), t.Day()}
	return nil
}

// toDate is DateUtils.LocalDate.toDate(): the start of the day with the CURRENT
// time of day (LocalTime.now) in the process zone.
func (d strapiLocalDate) toDate(now time.Time) time.Time {
	n := now.In(time.Local)
	return time.Date(d.Year, time.Month(d.Month), d.Day, n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), time.Local)
}

// responseQagMapper is ResponseQagMapper.toDomain: the first response type decides.
// An empty reponseType makes `first()` throw (HTTP 500).
func responseQagMapper(list []*strapiResponseQag, now time.Time) []ResponseQag {
	out := make([]ResponseQag, 0, len(list))
	for _, r := range list {
		if r == nil {
			panic("java.lang.NullPointerException: null element in Strapi data")
		}
		if len(r.ReponseType) == 0 {
			panic("java.util.NoSuchElementException: List is empty.")
		}
		content := r.ReponseType[0]
		switch {
		case content.Text != nil:
			out = append(out, ResponseQag{Text: &ResponseQagText{
				Author:            r.Auteur,
				AuthorPortraitURL: r.getAuthorPortraitURL(),
				ResponseDate:      r.ReponseDate.toDate(now),
				FeedbackQuestion:  r.FeedbackQuestion,
				QagID:             r.QuestionID,
				AuthorFunction:    r.AuteurFonction,
				ResponseText:      content.Text.Text.ToHTMLBody(),
				ResponseLabel:     content.Text.Label,
			}})
		case content.Video != nil:
			v := content.Video
			fn := v.AuteurDescription
			if r.AuteurFonction != nil {
				fn = *r.AuteurFonction
			}
			video := &ResponseQagVideo{
				Author:            r.Auteur,
				AuthorPortraitURL: r.getAuthorPortraitURL(),
				ResponseDate:      r.ReponseDate.toDate(now),
				FeedbackQuestion:  r.FeedbackQuestion,
				QagID:             r.QuestionID,
				AuthorFunction:    &fn,
				AuthorDescription: fn,
				VideoURL:          v.getVideoURL(),
				VideoTitle:        v.PageTitle,
				VideoWidth:        v.VideoWidth,
				VideoHeight:       v.VideoHeight,
				Transcription:     v.Transcription,
			}
			if v.hasInformationAdditionnelle() {
				video.AdditionalInfo = &ResponseQagAdditionalInfo{
					AdditionalInfoTitle:       *v.InformationAdditionnelleTitre,
					AdditionalInfoDescription: v.InformationAdditionnelleDescription.ToHTMLBody(),
				}
			}
			out = append(out, ResponseQag{Video: video})
		default:
			panic("kotlin.NoWhenBranchMatchedException")
		}
	}
	return out
}

// responsePopulate is ResponseQagStrapiRepository.POPULATE.
var responsePopulate = strings.Join([]string{
	"[auteurPortrait][fields][0]=url",
	"[auteurPortrait][fields][1]=formats",
	"[reponseType][on][reponse.reponse-video][populate]=*",
	"[reponseType][on][reponse.reponsetextuelle][populate]=*",
}, "&populate")

// ResponseRepository is the part of ResponseQagRepositoryImpl used by the QaG details.
type ResponseRepository struct{ a *app.App }

// GetResponseQag is getResponseQag(qagId): the Government response of one QaG, nil
// when the id is not a UUID, when Strapi has none or fails (an empty answer).
// The mapper's exceptions (a response without content) are panics (HTTP 500).
func (r *ResponseRepository) GetResponseQag(ctx context.Context, qagID string) *ResponseQag {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil
	}
	return r.fetch(ctx, uid)
}

func (r *ResponseRepository) fetch(ctx context.Context, uid string) *ResponseQag {
	b := strapi.NewRequest("reponse-du-gouvernements").FilterIn("questionId", []string{uid}).Populate(responsePopulate)
	env := strapi.Collection[*strapiResponseQag](ctx, r.a.Strapi, b)
	list := responseQagMapper(env.Data, r.a.Now())
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}
