package consultationresponse

import (
	"context"
	"strings"
	"testing"

	"agora/internal/modules/consultation"
)

func newControl(qs ...consultation.Question) (*ControlResponseConsultationUseCase, *fakeQuestions) {
	f := &fakeQuestions{list: qs}
	return newControlResponseConsultationUseCase(f, 400), f
}

// ControlResponseConsultationUseCaseTest: duplicated question id.
func TestControlDuplicatedQuestionID(t *testing.T) {
	uc, f := newControl()
	got := uc.IsResponseConsultationValid(context.Background(), "consultId", []ReponseConsultationInserting{resp("question1", ""), resp("question1", "")})
	mustEqual(t, "valid", got, false)
	mustEqual(t, "questions read once", len(f.calls), 1)
}

// QuestionOpenTestCases, QuestionMultipleTestCases, QuestionUniqueChoiceAndConditionalTestCases: the validator of the type decides.
func TestControlUsesTheValidatorOfTheType(t *testing.T) {
	tests := []struct {
		name string
		q    consultation.Question
		r    ReponseConsultationInserting
		want bool
	}{
		{"open ok", openQ("q"), resp("q", "texte"), true},
		{"open too long", openQ("q"), resp("q", strings.Repeat("a", 401)), false},
		{"multiple ok", multiQ("q", 2, "a", "b", "c"), resp("q", "", "a", "b"), true},
		{"multiple too many", multiQ("q", 2, "a", "b", "c"), resp("q", "", "a", "b", "c"), false},
		{"unique ok", uniqueQ("q", "a", "b"), resp("q", "", "a"), true},
		{"unique two", uniqueQ("q", "a", "b"), resp("q", "", "a", "b"), false},
		{"conditional ok", condQ("q", "a", "b"), resp("q", "", "a"), true},
		{"conditional none", condQ("q", "a", "b"), resp("q", ""), false},
		{"chapter has no validator", chapterQ("q"), resp("q", strings.Repeat("a", 5000), "x", "y"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc, _ := newControl(tt.q)
			mustEqual(t, "valid", uc.IsResponseConsultationValid(context.Background(), "c", []ReponseConsultationInserting{tt.r}), tt.want)
		})
	}
}

func TestControlSeveralQuestions(t *testing.T) {
	q1, q2 := uniqueQ("question1", "a", "b"), condQ("question2", "c", "d")
	uc, _ := newControl(q1, q2)
	ctx := context.Background()
	mustEqual(t, "both valid", uc.IsResponseConsultationValid(ctx, "c", []ReponseConsultationInserting{resp("question1", "", "a"), resp("question2", "", "c")}), true)
	mustEqual(t, "second invalid", uc.IsResponseConsultationValid(ctx, "c", []ReponseConsultationInserting{resp("question1", "", "a"), resp("question2", "", "c", "d")}), false)
	mustEqual(t, "unknown question", uc.IsResponseConsultationValid(ctx, "c", []ReponseConsultationInserting{resp("question1", "", "a"), resp("question3", "")}), false)
	mustEqual(t, "no answer is valid", uc.IsResponseConsultationValid(ctx, "c", nil), true)
}

// an unknown consultation has no question: only an empty list of answers is valid
func TestControlUnknownConsultation(t *testing.T) {
	uc, _ := newControl()
	ctx := context.Background()
	mustEqual(t, "empty", uc.IsResponseConsultationValid(ctx, "unknown", nil), true)
	mustEqual(t, "one answer", uc.IsResponseConsultationValid(ctx, "unknown", []ReponseConsultationInserting{resp("1", "", "2")}), false)
}

func TestControlFirstQuestionWithTheID(t *testing.T) {
	uc, _ := newControl(uniqueQ("1", "a"), openQ("1"))
	// find returns the first question: the unique choice validator rejects an answer without choice
	mustEqual(t, "first wins", uc.IsResponseConsultationValid(context.Background(), "c", []ReponseConsultationInserting{resp("1", "texte")}), false)
}

func TestValidatorsTextLengthsAreUTF16Units(t *testing.T) {
	emoji := "\U0001F600"
	tests := []struct {
		name string
		q    consultation.Question
		r    ReponseConsultationInserting
		want bool
	}{
		{"unique 200", uniqueQ("q", "a"), resp("q", strings.Repeat("é", 200), "a"), true},
		{"unique 201", uniqueQ("q", "a"), resp("q", strings.Repeat("é", 201), "a"), false},
		{"unique 100 emoji", uniqueQ("q", "a"), resp("q", strings.Repeat(emoji, 100), "a"), true},
		{"unique 101 emoji", uniqueQ("q", "a"), resp("q", strings.Repeat(emoji, 101), "a"), false},
		{"multiple 201", multiQ("q", 2, "a"), resp("q", strings.Repeat("a", 201), "a"), false},
		{"multiple 200", multiQ("q", 2, "a"), resp("q", strings.Repeat("a", 200), "a"), true},
		{"open 400", openQ("q"), resp("q", strings.Repeat("é", 400)), true},
		{"open 200 emoji", openQ("q"), resp("q", strings.Repeat(emoji, 200)), true},
		{"open 201 emoji", openQ("q"), resp("q", strings.Repeat(emoji, 201)), false},
		{"open 399+emoji", openQ("q"), resp("q", strings.Repeat("a", 399)+emoji), false},
		{"open 398+emoji", openQ("q"), resp("q", strings.Repeat("a", 398)+emoji), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc, _ := newControl(tt.q)
			mustEqual(t, "valid", uc.IsResponseConsultationValid(context.Background(), "c", []ReponseConsultationInserting{tt.r}), tt.want)
		})
	}
}

func TestMultipleChoicesValidator(t *testing.T) {
	q := multiQ("q", 2, "a", "b", "c")
	v := multipleChoicesValidator{}
	tests := []struct {
		name string
		r    ReponseConsultationInserting
		want bool
	}{
		{"none", resp("q", ""), true},
		{"one", resp("q", "", "a"), true},
		{"two", resp("q", "", "a", "b"), true},
		{"three", resp("q", "", "a", "b", "c"), false},
		{"duplicate", resp("q", "", "a", "a"), false},
		{"unknown ids are not checked", resp("q", "", "x", "y"), true},
		{"null id", ReponseConsultationInserting{QuestionID: "q", ChoiceIDs: []*string{nil}}, true},
		{"two nulls are a duplicate", ReponseConsultationInserting{QuestionID: "q", ChoiceIDs: []*string{nil, nil}}, false},
		{"null and an id", ReponseConsultationInserting{QuestionID: "q", ChoiceIDs: []*string{nil, sp("a")}}, true},
		{"null list", ReponseConsultationInserting{QuestionID: "q", ChoiceIDsNil: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { mustEqual(t, "valid", v.isValid(q, tt.r), tt.want) })
	}
	mustEqual(t, "not a multiple choices question", v.isValid(uniqueQ("q", "a"), resp("q", "", "a")), false)
	zero := multiQ("q", 0, "a")
	mustEqual(t, "max 0, none", v.isValid(zero, resp("q", "")), true)
	mustEqual(t, "max 0, one", v.isValid(zero, resp("q", "", "a")), false)
	neg := multiQ("q", -1, "a")
	mustEqual(t, "max -1, none", v.isValid(neg, resp("q", "")), false)
}

func TestUniqueChoiceAndConditionalValidator(t *testing.T) {
	v := uniqueChoiceAndConditionalValidator{}
	for _, q := range []consultation.Question{uniqueQ("q", "a", "b"), condQ("q", "a", "b")} {
		mustEqual(t, "one", v.isValid(q, resp("q", "", "a")), true)
		mustEqual(t, "one null", v.isValid(q, ReponseConsultationInserting{QuestionID: "q", ChoiceIDs: []*string{nil}}), true)
		mustEqual(t, "none", v.isValid(q, resp("q", "")), false)
		mustEqual(t, "two", v.isValid(q, resp("q", "", "a", "b")), false)
		mustEqual(t, "null list", v.isValid(q, ReponseConsultationInserting{QuestionID: "q", ChoiceIDsNil: true}), false)
	}
	mustEqual(t, "an open question", v.isValid(openQ("q"), resp("q", "", "a")), false)
	mustEqual(t, "a multiple choices question", v.isValid(multiQ("q", 3, "a"), resp("q", "", "a")), false)
}

func TestOpenValidatorLimit(t *testing.T) {
	v := openValidator{openQuestionMaxTextLength: 2000}
	mustEqual(t, "2000", v.isValid(openQ("q"), resp("q", strings.Repeat("a", 2000))), true)
	mustEqual(t, "2001", v.isValid(openQ("q"), resp("q", strings.Repeat("a", 2001))), false)
	mustEqual(t, "not an open question", v.isValid(uniqueQ("q", "a"), resp("q", "")), false)
}
