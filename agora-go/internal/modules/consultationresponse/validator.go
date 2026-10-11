package consultationresponse

import (
	"context"

	"agora/internal/javacompat"
	"agora/internal/modules/consultation"
)

// QuestionSource is QuestionRepository.getConsultationQuestions (the consultation is
// looked up by document id, unpublished included; unknown = no question).
type QuestionSource interface {
	GetConsultationQuestions(ctx context.Context, consultationID string) consultation.Questions
}

// openFieldTextMaxLength is ResponseQuestionWithChoicesValidator.OPEN_FIELD_TEXT_MAX_LENGTH.
const openFieldTextMaxLength = 200

// responseValidator is the abstract class ResponseQuestionValidator.
type responseValidator interface {
	isValid(question consultation.Question, response ReponseConsultationInserting) bool
}

// isValidOpenTextFieldLength is ResponseQuestionWithChoicesValidator.isValidOpenTextFieldLength:
// String.length is a count of UTF-16 units.
func isValidOpenTextFieldLength(response ReponseConsultationInserting) bool {
	return javacompat.Len16(response.ResponseText) <= openFieldTextMaxLength
}

// multipleChoicesValidator is QuestionMultipleChoicesValidator.
type multipleChoicesValidator struct{}

func (multipleChoicesValidator) isValid(question consultation.Question, response ReponseConsultationInserting) bool {
	if question.Kind != consultation.KindMultipleChoices || response.ChoiceIDsNil {
		return false
	}
	isChoiceIDListUnderMaxChoices := len(response.ChoiceIDs) <= question.MaxChoices
	isChoiceIDListUnique := distinctChoiceIDs(response.ChoiceIDs) == len(response.ChoiceIDs)
	return isChoiceIDListUnique && isChoiceIDListUnderMaxChoices && isValidOpenTextFieldLength(response)
}

// distinctChoiceIDs is choiceIds.toSet().size: a null element counts as one value.
func distinctChoiceIDs(ids []*string) int {
	seen := make(map[string]struct{}, len(ids))
	nulls := 0
	for _, id := range ids {
		if id == nil {
			nulls = 1
			continue
		}
		seen[*id] = struct{}{}
	}
	return len(seen) + nulls
}

// uniqueChoiceAndConditionalValidator is QuestionUniqueChoiceAndConditionalValidator.
type uniqueChoiceAndConditionalValidator struct{}

func (uniqueChoiceAndConditionalValidator) isValid(question consultation.Question, response ReponseConsultationInserting) bool {
	if question.Kind != consultation.KindUniqueChoice && question.Kind != consultation.KindConditional {
		return false
	}
	if response.ChoiceIDsNil {
		return false
	}
	hasOneChoiceID := len(response.ChoiceIDs) == 1
	return hasOneChoiceID && isValidOpenTextFieldLength(response)
}

// openValidator is QuestionOpenValidator (OPEN_QUESTION_MAX_TEXT_LENGTH, 400 by default).
type openValidator struct{ openQuestionMaxTextLength int }

func (v openValidator) isValid(question consultation.Question, response ReponseConsultationInserting) bool {
	if question.Kind != consultation.KindOpen {
		return false
	}
	return javacompat.Len16(response.ResponseText) <= v.openQuestionMaxTextLength
}

// ControlResponseConsultationUseCase is usecase/consultationResponse/ControlResponseConsultationUseCase.
type ControlResponseConsultationUseCase struct {
	questions QuestionSource
	multiple  responseValidator
	unique    responseValidator
	open      responseValidator
}

func newControlResponseConsultationUseCase(questions QuestionSource, openQuestionMaxTextLength int) *ControlResponseConsultationUseCase {
	return &ControlResponseConsultationUseCase{
		questions: questions,
		multiple:  multipleChoicesValidator{},
		unique:    uniqueChoiceAndConditionalValidator{},
		open:      openValidator{openQuestionMaxTextLength: openQuestionMaxTextLength},
	}
}

// IsResponseConsultationValid is isResponseConsultationValid: no question answered twice, and
// every answer matches a question of the consultation and is accepted by the validator of
// its type. A chapter has no validator (any answer is accepted); an unknown consultation has
// no question, so only an empty list of answers is valid.
func (u *ControlResponseConsultationUseCase) IsResponseConsultationValid(ctx context.Context, consultationID string, responses []ReponseConsultationInserting) bool {
	questionList := u.questions.GetConsultationQuestions(ctx, consultationID).Questions
	unique := make(map[string]struct{}, len(responses))
	for _, r := range responses {
		unique[r.QuestionID] = struct{}{}
	}
	if len(unique) < len(responses) {
		return false
	}
	byID := make(map[string]int, len(questionList))
	for i, q := range questionList {
		if _, ok := byID[q.ID]; !ok {
			byID[q.ID] = i // find returns the first question with that id
		}
	}
	for _, response := range responses {
		i, ok := byID[response.QuestionID]
		if !ok {
			return false
		}
		question := questionList[i]
		if v := u.validatorOf(question); v != nil && !v.isValid(question, response) {
			return false
		}
	}
	return true
}

// validatorOf is getResponseQuestionValidator: nil for a chapter.
func (u *ControlResponseConsultationUseCase) validatorOf(question consultation.Question) responseValidator {
	switch question.Kind {
	case consultation.KindMultipleChoices:
		return u.multiple
	case consultation.KindUniqueChoice, consultation.KindConditional:
		return u.unique
	case consultation.KindOpen:
		return u.open
	}
	return nil
}
