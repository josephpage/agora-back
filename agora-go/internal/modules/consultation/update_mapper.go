package consultation

import (
	"strings"

	"agora/internal/strapi"
)

// updateMapper is ConsultationUpdateInfoV2Mapper.
type updateMapper struct{}

const (
	pictoGoalCommanditaire = "\U0001F5E3️" // 🗣️
	pictoGoalObjectif      = "\U0001F3AF"  // 🎯
	pictoGoalAxe           = "\U0001F680"  // 🚀
	pictoFeedback          = "\U0001F4AC"  // 💬
	pictoEnded             = "\U0001F3C1"  // 🏁
	pictoThanks            = "\U0001F64C"  // 🙌
)

// htmlOf is `List<StrapiRichText>.toHtml()`.
func htmlOf(l strapi.RichText) string { return l.ToHTML() }

func ptr[T any](v T) *T { return &v }

func feedbackQuestionOf(documentID, feedbackMessage string) *FeedbackQuestion {
	return &FeedbackQuestion{
		ConsultationUpdateID: documentID,
		Title:                "Donnez votre avis",
		Picto:                pictoFeedback,
		Description:          "<body>" + feedbackMessage + "</body>",
	}
}

func infoHeaderOf(emoji, label *string) *InfoHeader {
	if emoji != nil && label != nil {
		return &InfoHeader{Picto: *emoji, Description: *label}
	}
	return nil
}

// toDomainUnanswered is toDomainUnanswered(consultationDTO): the content shown
// before the user answered.
func (updateMapper) toDomainUnanswered(c *strapiConsultation) *UpdateInfo {
	contentBeforeResponse := &c.ContenuAvantReponse
	sections := toSections(contentBeforeResponse.Sections)

	sectionPourquoi := SectionTitle{Title: "Pourquoi cette consultation ?"}
	presentation := htmlOf(contentBeforeResponse.Presentation)

	body := make([]Section, 0, len(sections)+2)
	body = append(body, sectionPourquoi, SectionRichText{Description: presentation})
	body = append(body, sections...)

	// `split("</p>").firstOrNull()?.let { "$it</p>" }`: the text before the first "</p>"
	// (the whole text when there is none), closed with "</p>"
	firstParagraph := presentation
	if i := strings.Index(presentation, "</p>"); i >= 0 {
		firstParagraph = presentation[:i]
	}

	return &UpdateInfo{
		ID:                   contentBeforeResponse.DocumentID,
		Slug:                 contentBeforeResponse.Slug,
		UpdateDate:           c.DateDeDebut,
		ShareTextTemplate:    contentBeforeResponse.TemplatePartage,
		HasQuestionsInfo:     true,
		HasParticipationInfo: false,
		ResponsesInfo:        nil,
		SectionsHeader:       []Section{},
		Body:                 body,
		BodyPreview:          []Section{sectionPourquoi, SectionRichText{Description: firstParagraph + "</p>"}},
		InfoHeader:           nil,
		DownloadAnalysisURL:  nil,
		FeedbackQuestion:     nil,
		Footer:               nil,
		Goals: []Goal{
			{Picto: pictoGoalCommanditaire, Description: trimParagraph(htmlOf(contentBeforeResponse.Commanditaire))},
			{Picto: pictoGoalObjectif, Description: trimParagraph(htmlOf(contentBeforeResponse.Objectif))},
			{Picto: pictoGoalAxe, Description: trimParagraph(htmlOf(contentBeforeResponse.AxeGouvernemental))},
		},
	}
}

// trimParagraph is `.removePrefix("<p>").removeSuffix("</p>")`.
func trimParagraph(s string) string {
	s = strings.TrimPrefix(s, "<p>")
	return strings.TrimSuffix(s, "</p>")
}

// toDomainContenuAutre is toDomainContenuAutre(consultationDTO, contentDTO).
func (updateMapper) toDomainContenuAutre(c *strapiConsultation, content *strapiContenuAutre) *UpdateInfo {
	htmlSections := toSections(content.Sections)
	previewHTMLSections := toPreviewSections(content.Sections)
	return &UpdateInfo{
		ID:                   content.DocumentID,
		Slug:                 content.Slug,
		UpdateDate:           content.DatetimePublication,
		ShareTextTemplate:    content.TemplatePartage,
		HasQuestionsInfo:     false,
		HasParticipationInfo: false,
		SectionsHeader:       []Section{},
		Body:                 htmlSections,
		BodyPreview:          previewHTMLSections,
		InfoHeader:           infoHeaderOf(content.RecapEmoji, content.RecapLabel),
		FeedbackQuestion:     feedbackQuestionOf(content.DocumentID, content.FeedbackMessage),
	}
}

// toDomainAnsweredOrEnded is toDomainAnsweredOrEnded(consultation, now).
func (updateMapper) toDomainAnsweredOrEnded(c *strapiConsultation, now LocalDateTime) *UpdateInfo {
	contenu := &c.ContenuApresReponseOuTerminee
	htmlSections := toSections(contenu.Sections)

	consultationIsEnded := c.DateDeFin.Before(now)
	var responsesInfo *ResponsesInfo
	if consultationIsEnded {
		responsesInfo = &ResponsesInfo{
			Picto:       pictoEnded,
			Description: "<body><b>Cette consultation est maintenant terminée.</b> Les résultats sont en cours d'analyse. Vous serez prévenu(e) dès que la synthèse sera disponible.</body>",
			ActionText:  "Voir tous les résultats",
		}
	} else {
		responsesInfo = &ResponsesInfo{
			Picto:       pictoThanks,
			Description: "<body><b>Merci pour votre participation</b> à cette consultation !</body>",
			ActionText:  "Voir les premiers résultats",
		}
	}

	previewHTMLSections := toPreviewSections(contenu.Sections)

	return &UpdateInfo{
		ID:                   contenu.DocumentID,
		Slug:                 contenu.Slug,
		UpdateDate:           c.DateDeDebut,
		ShareTextTemplate:    contenu.TemplatePartage,
		HasQuestionsInfo:     false,
		HasParticipationInfo: false,
		ResponsesInfo:        responsesInfo,
		SectionsHeader:       []Section{},
		Body:                 htmlSections,
		BodyPreview:          previewHTMLSections,
		FeedbackQuestion:     feedbackQuestionOf(contenu.DocumentID, contenu.FeedbackMessage),
	}
}

// toDomainReponseDuCommanditaire is toDomainReponseDuCommanditaire(consultation).
func (updateMapper) toDomainReponseDuCommanditaire(c *strapiConsultation) *UpdateInfo {
	contenu := c.ReponseDuCommanditaire
	if contenu == nil {
		return nil
	}
	return &UpdateInfo{
		ID:                   contenu.DocumentID,
		Slug:                 contenu.Slug,
		UpdateDate:           contenu.DatetimePublication,
		ShareTextTemplate:    contenu.TemplatePartage,
		HasQuestionsInfo:     false,
		HasParticipationInfo: false,
		SectionsHeader:       []Section{},
		Body:                 toSections(contenu.Sections),
		BodyPreview:          toPreviewSections(contenu.Sections),
		InfoHeader:           infoHeaderOf(contenu.RecapEmoji, contenu.RecapLabel),
		FeedbackQuestion:     feedbackQuestionOf(contenu.DocumentID, contenu.FeedbackMessage),
	}
}

// toDomainAnalyseDesReponses is toDomainAnalyseDesReponses(consultation).
func (updateMapper) toDomainAnalyseDesReponses(c *strapiConsultation) *UpdateInfo {
	contenu := c.AnalyseDesReponses
	if contenu == nil {
		return nil
	}
	url := contenu.getAnalysePdfURL()
	return &UpdateInfo{
		ID:                   contenu.DocumentID,
		Slug:                 contenu.Slug,
		UpdateDate:           contenu.DatetimePublication,
		ShareTextTemplate:    contenu.TemplatePartage,
		HasQuestionsInfo:     false,
		HasParticipationInfo: false,
		SectionsHeader:       []Section{},
		Body:                 toSections(contenu.Sections),
		BodyPreview:          toPreviewSections(contenu.Sections),
		InfoHeader:           infoHeaderOf(contenu.RecapEmoji, contenu.RecapLabel),
		DownloadAnalysisURL:  &url,
		FeedbackQuestion:     feedbackQuestionOf(contenu.DocumentID, contenu.FeedbackMessage),
	}
}

// toSections is toSections(sections): a JSON null element makes Kotlin's
// `when` throw NoWhenBranchMatchedException (HTTP 500).
func toSections(sections []*strapiSection) []Section {
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		switch {
		case s == nil:
			panic("kotlin.NoWhenBranchMatchedException")
		case s.Titre != nil:
			out = append(out, SectionTitle{Title: s.Titre.Titre})
		case s.RichText != nil:
			out = append(out, SectionRichText{Description: htmlOf(s.RichText.Description)})
		case s.Citation != nil:
			out = append(out, SectionQuote{Description: htmlOf(s.Citation.Description)})
		case s.Image != nil:
			out = append(out, SectionImage{URL: s.Image.getImageURL(), ContentDescription: ptr(s.Image.DescriptionImage)})
		case s.Video != nil:
			v := s.Video
			out = append(out, SectionVideo{
				URL:           v.getVideoURL(),
				Width:         v.Largeur,
				Height:        v.Hauteur,
				AuthorInfo:    &VideoAuthorInfo{Name: v.NomAuteur, Message: v.PosteAuteur, Date: v.DateTournage},
				Transcription: v.Transcription,
			})
		case s.Chiffre != nil:
			out = append(out, SectionFocusNumber{Title: s.Chiffre.Titre, Description: htmlOf(s.Chiffre.Description)})
		case s.Accordeon != nil:
			out = append(out, SectionAccordion{Title: s.Accordeon.Titre, Sections: []Section{SectionRichText{Description: htmlOf(s.Accordeon.Description)}}})
		}
	}
	return out
}

// toPreviewSections is toPreviewSections(sections): no preview under 8
// sections, else the first five.
func toPreviewSections(sections []*strapiSection) []Section {
	html := toSections(sections)
	if len(html) < 8 {
		return []Section{}
	}
	return html[:5]
}
