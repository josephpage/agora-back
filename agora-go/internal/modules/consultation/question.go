package consultation

import (
	"context"

	"agora/internal/app"
)

// ---------------------------------------------------------------------------
// QuestionMapper, QuestionsMapper
// ---------------------------------------------------------------------------

// questionsOf is QuestionsMapper.toDomain: the questions of a consultation, in
// Strapi order. A JSON null element makes Kotlin's `when` throw
// NoWhenBranchMatchedException (HTTP 500).
func questionsOf(c *strapiConsultation) []Question {
	out := make([]Question, 0, len(c.Questions))
	for _, q := range c.Questions {
		switch {
		case q == nil:
			panic("kotlin.NoWhenBranchMatchedException")
		case q.Multiple != nil:
			out = append(out, questionMultiple(q.Multiple, q, c))
		case q.Unique != nil:
			out = append(out, questionUnique(q.Unique, q, c))
		case q.Ouverte != nil:
			out = append(out, questionOuverte(q.Ouverte, q, c))
		case q.Description != nil:
			out = append(out, questionChapter(q.Description, q, c))
		default:
			out = append(out, questionConditional(q.Conditional, c))
		}
	}
	return out
}

func simpleChoices(choix []*strapiChoixSimple, questionID string) []ChoixPossible {
	out := make([]ChoixPossible, len(choix))
	for i, choice := range choix {
		out[i] = ChoixPossible{ID: choice.ID, Label: choice.Label, Ordre: i, QuestionID: questionID, HasOpenTextField: choice.Ouvert}
	}
	return out
}

// toQuestionChoixMultiple
func questionMultiple(s *strapiQuestionMultiple, q *strapiQuestion, c *strapiConsultation) Question {
	choix := simpleChoices(s.Choix, s.ID)
	var popup *string
	if s.PopupExplication != nil {
		popup = ptr(s.PopupExplication.ToHTML())
	}
	return Question{
		Kind: KindMultipleChoices, ID: s.ID, Title: s.Titre, PopupDescription: popup, Order: s.Numero,
		NextQuestionID: c.getNextQuestionID(q), ConsultationID: c.DocumentID, ChoixPossibleList: choix, MaxChoices: s.NombreMaximumDeChoix,
	}
}

// toQuestionChoixUnique
func questionUnique(s *strapiQuestionUnique, q *strapiQuestion, c *strapiConsultation) Question {
	choix := simpleChoices(s.Choix, s.ID)
	var popup *string
	if s.PopupExplication != nil {
		popup = ptr(s.PopupExplication.ToHTML())
	}
	return Question{
		Kind: KindUniqueChoice, ID: s.ID, Title: s.Titre, PopupDescription: popup, Order: s.Numero,
		NextQuestionID: c.getNextQuestionID(q), ConsultationID: c.DocumentID, ChoixPossibleList: choix,
	}
}

// toQuestionOuverte
func questionOuverte(s *strapiQuestionOuverte, q *strapiQuestion, c *strapiConsultation) Question {
	var popup *string
	if s.PopupExplication != nil {
		popup = ptr(s.PopupExplication.ToHTML())
	}
	return Question{
		Kind: KindOpen, ID: s.ID, Title: s.Titre, PopupDescription: popup, Order: s.Numero,
		NextQuestionID: c.getNextQuestionID(q), ConsultationID: c.DocumentID,
	}
}

// toQuestionDescription: a chapter (no popup).
func questionChapter(s *strapiQuestionDescription, q *strapiQuestion, c *strapiConsultation) Question {
	return Question{
		Kind: KindChapter, ID: s.ID, Title: s.Titre, PopupDescription: nil, Order: s.Numero,
		NextQuestionID: c.getNextQuestionID(q), ConsultationID: c.DocumentID,
		URLImage: s.getImageURL(), Description: s.Description.ToHTML(), TranscriptionImage: s.TranscriptionImage,
	}
}

// toQuestionConditionnelle: the next question of each choice is looked up by
// number; none (`first` throws NoSuchElementException, HTTP 500) is an error.
func questionConditional(s *strapiQuestionConditional, c *strapiConsultation) Question {
	choices := make([]ChoixPossible, len(s.Choix))
	for i, choice := range s.Choix {
		var next *strapiQuestion
		for _, other := range c.Questions {
			if other.numero() == choice.NumeroDeLaQuestionSuivante {
				next = other
				break
			}
		}
		if next == nil {
			panic("java.util.NoSuchElementException: Collection contains no element matching the predicate.")
		}
		choices[i] = ChoixPossible{
			ID: choice.ID, Label: choice.Label, Ordre: i, QuestionID: s.ID, HasOpenTextField: choice.Ouvert, NextQuestionID: next.id(),
		}
	}
	var popup *string
	if s.PopupExplication != nil {
		popup = ptr(s.PopupExplication.ToHTML())
	}
	return Question{
		Kind: KindConditional, ID: s.ID, Title: s.Titre, PopupDescription: popup, Order: s.Numero,
		NextQuestionID: nil, ConsultationID: c.DocumentID, ChoixPossibleList: choices,
	}
}

// ---------------------------------------------------------------------------
// QuestionRepository, ListQuestionConsultationUseCase
// ---------------------------------------------------------------------------

// QuestionRepository is QuestionRepositoryImpl.
type QuestionRepository struct {
	a      *app.App
	strapi *StrapiRepository
}

// GetConsultationQuestions is getConsultationQuestions(consultationId): the
// consultation is looked up by document id only (a slug is not resolved);
// unknown = no question.
func (r *QuestionRepository) GetConsultationQuestions(ctx context.Context, consultationID string) Questions {
	c := r.strapi.GetConsultationByIDWithUnpublished(ctx, consultationID)
	if c == nil {
		return Questions{QuestionCount: 0, Questions: []Question{}}
	}
	questions := questionsOf(c)
	return Questions{QuestionCount: c.NombreDeQuestion, Questions: questions}
}
