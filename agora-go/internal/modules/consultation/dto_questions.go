package consultation

import "strconv"

// QuestionsJSON is QuestionsJson (GET /consultations/{consultationId}/questions).
type QuestionsJSON struct {
	QuestionCount            int                           `json:"questionCount"`
	QuestionsUniqueChoice    []QuestionUniqueChoiceJSON    `json:"questionsUniqueChoice"`
	QuestionsOpened          []QuestionOpenedJSON          `json:"questionsOpened"`
	QuestionsMultipleChoices []QuestionMultipleChoicesJSON `json:"questionsMultipleChoices"`
	Chapters                 []QuestionChapterJSON         `json:"chapters"`
	QuestionsWithCondition   []QuestionConditionalJSON     `json:"questionsWithCondition"`
}

// JavaName is the XML root element.
func (QuestionsJSON) JavaName() string { return "QuestionsJson" }

// QuestionUniqueChoiceJSON is QuestionUniqueChoiceJson.
type QuestionUniqueChoiceJSON struct {
	ID                   string              `json:"id"`
	Title                string              `json:"title"`
	PopupDescription     *string             `json:"popupDescription"`
	Order                int                 `json:"order"`
	QuestionProgress     string              `json:"questionProgress"`
	QuestionProgressA11y string              `json:"questionProgressA11y"`
	NextQuestionID       *string             `json:"nextQuestionId"`
	PossibleChoices      []ChoixPossibleJSON `json:"possibleChoices"`
}

// QuestionMultipleChoicesJSON is QuestionMultipleChoicesJson.
type QuestionMultipleChoicesJSON struct {
	ID                   string              `json:"id"`
	Title                string              `json:"title"`
	PopupDescription     *string             `json:"popupDescription"`
	Order                int                 `json:"order"`
	QuestionProgress     string              `json:"questionProgress"`
	QuestionProgressA11y string              `json:"questionProgressA11y"`
	MaxChoices           int                 `json:"maxChoices"`
	NextQuestionID       *string             `json:"nextQuestionId"`
	PossibleChoices      []ChoixPossibleJSON `json:"possibleChoices"`
}

// QuestionOpenedJSON is QuestionOpenedJson.
type QuestionOpenedJSON struct {
	ID                   string  `json:"id"`
	Title                string  `json:"title"`
	PopupDescription     *string `json:"popupDescription"`
	Order                int     `json:"order"`
	QuestionProgress     string  `json:"questionProgress"`
	QuestionProgressA11y string  `json:"questionProgressA11y"`
	NextQuestionID       *string `json:"nextQuestionId"`
}

// QuestionChapterJSON is QuestionChapterJson.
type QuestionChapterJSON struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	PopupDescription   *string `json:"popupDescription"`
	Order              int     `json:"order"`
	Description        string  `json:"description"`
	NextQuestionID     *string `json:"nextQuestionId"`
	ImageURL           *string `json:"imageUrl"`
	ImageTranscription *string `json:"imageTranscription"`
}

// QuestionConditionalJSON is QuestionConditionalJson.
type QuestionConditionalJSON struct {
	ID                   string              `json:"id"`
	Title                string              `json:"title"`
	PopupDescription     *string             `json:"popupDescription"`
	Order                int                 `json:"order"`
	QuestionProgress     string              `json:"questionProgress"`
	QuestionProgressA11y string              `json:"questionProgressA11y"`
	PossibleChoices      []ChoixPossibleJSON `json:"possibleChoices"`
}

// ChoixPossibleJSON is ChoixPossibleJson (@JsonInclude(NON_NULL)).
type ChoixPossibleJSON struct {
	ID               string  `json:"id,omitnull"`
	Label            string  `json:"label,omitnull"`
	Order            int     `json:"order,omitnull"`
	HasOpenTextField bool    `json:"hasOpenTextField,omitnull"`
	NextQuestionID   *string `json:"nextQuestionId,omitnull"`
}

// JavaName of each class (XML root element).
func (QuestionUniqueChoiceJSON) JavaName() string    { return "QuestionUniqueChoiceJson" }
func (QuestionMultipleChoicesJSON) JavaName() string { return "QuestionMultipleChoicesJson" }
func (QuestionOpenedJSON) JavaName() string          { return "QuestionOpenedJson" }
func (QuestionChapterJSON) JavaName() string         { return "QuestionChapterJson" }
func (QuestionConditionalJSON) JavaName() string     { return "QuestionConditionalJson" }
func (ChoixPossibleJSON) JavaName() string           { return "ChoixPossibleJson" }

// ToQuestionsJSON is QuestionJsonMapper.toJson(domain).
func ToQuestionsJSON(q Questions) QuestionsJSON {
	var chapters []Question
	for _, question := range q.Questions {
		if question.Kind == KindChapter {
			chapters = append(chapters, question)
		}
	}
	questionsNumber := len(q.Questions) - len(chapters)

	out := QuestionsJSON{
		QuestionCount:            q.QuestionCount,
		QuestionsUniqueChoice:    []QuestionUniqueChoiceJSON{},
		QuestionsOpened:          []QuestionOpenedJSON{},
		QuestionsMultipleChoices: []QuestionMultipleChoicesJSON{},
		Chapters:                 []QuestionChapterJSON{},
		QuestionsWithCondition:   []QuestionConditionalJSON{},
	}
	for _, question := range q.Questions {
		switch question.Kind {
		case KindUniqueChoice:
			progress, a11y := buildQuestionProgress(question.Order, chapters, questionsNumber)
			out.QuestionsUniqueChoice = append(out.QuestionsUniqueChoice, QuestionUniqueChoiceJSON{
				ID: question.ID, Title: question.Title, PopupDescription: question.PopupDescription, Order: question.Order,
				QuestionProgress: progress, QuestionProgressA11y: a11y, NextQuestionID: question.NextQuestionID,
				PossibleChoices: choicesJSON(question.ChoixPossibleList, false),
			})
		case KindMultipleChoices:
			progress, a11y := buildQuestionProgress(question.Order, chapters, questionsNumber)
			out.QuestionsMultipleChoices = append(out.QuestionsMultipleChoices, QuestionMultipleChoicesJSON{
				ID: question.ID, Title: question.Title, PopupDescription: question.PopupDescription, Order: question.Order,
				QuestionProgress: progress, QuestionProgressA11y: a11y, MaxChoices: question.MaxChoices, NextQuestionID: question.NextQuestionID,
				PossibleChoices: choicesJSON(question.ChoixPossibleList, false),
			})
		case KindOpen:
			progress, a11y := buildQuestionProgress(question.Order, chapters, questionsNumber)
			out.QuestionsOpened = append(out.QuestionsOpened, QuestionOpenedJSON{
				ID: question.ID, Title: question.Title, PopupDescription: question.PopupDescription, Order: question.Order,
				QuestionProgress: progress, QuestionProgressA11y: a11y, NextQuestionID: question.NextQuestionID,
			})
		case KindChapter:
			out.Chapters = append(out.Chapters, QuestionChapterJSON{
				ID: question.ID, Title: question.Title, PopupDescription: question.PopupDescription, Order: question.Order,
				Description: question.Description, NextQuestionID: question.NextQuestionID,
				ImageURL: question.URLImage, ImageTranscription: question.TranscriptionImage,
			})
		case KindConditional:
			progress, a11y := buildQuestionProgress(question.Order, chapters, questionsNumber)
			out.QuestionsWithCondition = append(out.QuestionsWithCondition, QuestionConditionalJSON{
				ID: question.ID, Title: question.Title, PopupDescription: question.PopupDescription, Order: question.Order,
				QuestionProgress: progress, QuestionProgressA11y: a11y,
				PossibleChoices: choicesJSON(question.ChoixPossibleList, true),
			})
		}
	}
	return out
}

// choicesJSON maps the possible choices; only the conditional ones carry the
// next question (the others are written with nextQuestionId = null, i.e. omitted).
func choicesJSON(choices []ChoixPossible, conditional bool) []ChoixPossibleJSON {
	out := make([]ChoixPossibleJSON, len(choices))
	for i, c := range choices {
		out[i] = ChoixPossibleJSON{ID: c.ID, Label: c.Label, Order: c.Ordre, HasOpenTextField: c.HasOpenTextField}
		if conditional {
			next := c.NextQuestionID
			out[i].NextQuestionID = &next
		}
	}
	return out
}

// buildQuestionProgress is QuestionJsonMapper.buildQuestionProgress.
func buildQuestionProgress(order int, chapters []Question, questionsNumber int) (progress, a11y string) {
	p := order
	for _, ch := range chapters {
		if ch.Order < order {
			p--
		}
	}
	n := strconv.Itoa(questionsNumber)
	return "Question " + strconv.Itoa(p) + "/" + n, "Question " + strconv.Itoa(p) + " sur " + n
}
