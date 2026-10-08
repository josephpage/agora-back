package consultation

import (
	"log/slog"
)

// infoMapper is ConsultationInfoMapper (with ThematiqueMapper.toDomain).
type infoMapper struct{ log *slog.Logger }

// thematiqueOf is ThematiqueMapper.toDomain(dto).
func thematiqueOf(t strapiThematique) Thematique {
	return Thematique{ID: t.DocumentID, Label: t.Label, Picto: t.Pictogramme}
}

// toConsultationPreview is ConsultationInfoMapper.toConsultationPreview.
func (m infoMapper) toConsultationPreview(list []*strapiConsultation) []ConsultationPreview {
	out := make([]ConsultationPreview, 0, len(list))
	for _, c := range list {
		out = append(out, ConsultationPreview{
			ID:         c.DocumentID,
			Slug:       c.Slug,
			Title:      c.Titre,
			CoverURL:   c.getImageCouverture(),
			Thematique: thematiqueOf(c.Thematique),
			EndDate:    c.DateDeFin,
			Territory:  c.Territoire,
		})
	}
	return out
}

// toDomainFinished is ConsultationInfoMapper.toDomainFinished: a consultation
// without any update date before now is dropped (and logged).
func (m infoMapper) toDomainFinished(list []*strapiConsultation, now LocalDateTime) []ConsultationPreviewFinished {
	out := make([]ConsultationPreviewFinished, 0, len(list))
	for _, c := range list {
		thematique := thematiqueOf(c.Thematique)
		updateDate := c.getLatestUpdateDate(now)
		if updateDate == nil {
			m.log.Error("ConsultationPreviewFinished - Impossible de générer une updateDate pour la consultation id '" + c.DocumentID + "'")
			continue
		}
		out = append(out, ConsultationPreviewFinished{
			ID:             c.DocumentID,
			Slug:           c.Slug,
			Title:          c.Titre,
			CoverURL:       c.getImageCouverture(),
			Thematique:     thematique,
			UpdateLabel:    c.getFlammeLabel(now),
			LastUpdateDate: *updateDate,
			EndDate:        c.DateDeFin,
			Territory:      c.Territoire,
		})
	}
	return out
}

// toConsultationsWithUpdateInfo is ConsultationInfoMapper.toConsultationsWithUpdateInfo.
func (m infoMapper) toConsultationsWithUpdateInfo(list []*strapiConsultation, now LocalDateTime) []ConsultationWithUpdateInfo {
	out := make([]ConsultationWithUpdateInfo, 0, len(list))
	for _, c := range list {
		updateDate := c.getLatestUpdateDate(now)
		if updateDate == nil {
			m.log.Error("toConsultationWithUpdateInfo - Impossible de générer une updateDate pour la consultation id '" + c.DocumentID + "'")
			continue
		}
		out = append(out, ConsultationWithUpdateInfo{
			ID:           c.DocumentID,
			Slug:         c.Slug,
			Title:        c.Titre,
			CoverURL:     c.getImageCouverture(),
			ThematiqueID: c.Thematique.DocumentID,
			EndDate:      c.DateDeFin,
			UpdateDate:   *updateDate,
			UpdateLabel:  c.getFlammeLabel(now),
			Territory:    c.Territoire,
		})
	}
	return out
}

// toConsultationInfo is ConsultationInfoMapper.toConsultationInfo.
func (m infoMapper) toConsultationInfo(c *strapiConsultation) *ConsultationInfo {
	return &ConsultationInfo{
		ID:                   c.DocumentID,
		Title:                c.Titre,
		Slug:                 c.Slug,
		CoverURL:             c.getImageCouverture(),
		DetailsCoverURL:      c.getImagePageContenu(),
		StartDate:            c.DateDeDebut,
		EndDate:              c.DateDeFin,
		QuestionCount:        c.EstimationNombreDeQuestions,
		EstimatedTime:        c.EstimationTemps,
		ParticipantCountGoal: c.NombreParticipantsCible,
		Thematique:           thematiqueOf(c.Thematique),
		Territory:            c.Territoire,
		TitreWeb:             c.TitrePageWeb,
		SousTitreWeb:         c.SousTitrePageWeb,
	}
}
