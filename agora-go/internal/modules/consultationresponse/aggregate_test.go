package consultationresponse

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"agora/internal/modules/consultation"
)

type fakeToAggregate struct{ list []consultation.ConsultationPreview }

func (f fakeToAggregate) GetConsultationsToAggregate(context.Context) []consultation.ConsultationPreview {
	return f.list
}

type fakeAggregatedIDs struct {
	ids   []string
	calls int
}

func (f *fakeAggregatedIDs) GetAggregatedResultsConsultationIDs(context.Context) ([]string, error) {
	f.calls++
	return f.ids, nil
}

type fakeAggregate struct {
	done []string
	err  error
}

func (f *fakeAggregate) AggregateConsultationResults(_ context.Context, id string) error {
	f.done = append(f.done, id)
	return f.err
}

func newPick(list []consultation.ConsultationPreview, aggregatedIDs []string) (*PickConsultationsToAggregateUseCase, *fakeAggregatedIDs, *fakeAggregate) {
	ids, agg := &fakeAggregatedIDs{ids: aggregatedIDs}, &fakeAggregate{}
	return &PickConsultationsToAggregateUseCase{a: newApp(false), info: fakeToAggregate{list}, aggregated: ids, aggregate: agg}, ids, agg
}

// PickConsultationsToAggregateUseCaseTest: no consultation - should do nothing
func TestPickNoConsultation(t *testing.T) {
	uc, ids, agg := newPick(nil, nil)
	if err := uc.AggregateConsultations(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "aggregated ids not read", ids.calls, 0)
	mustEqual(t, "nothing aggregated", len(agg.done), 0)
}

// consultations that are not already aggregated - should aggregate them
func TestPickAggregatesTheOthers(t *testing.T) {
	uc, ids, agg := newPick([]consultation.ConsultationPreview{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}}, []string{"b"})
	if err := uc.AggregateConsultations(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "aggregated ids read once", ids.calls, 1)
	if !reflect.DeepEqual(agg.done, []string{"a", "c"}) {
		t.Fatalf("aggregated %v", agg.done)
	}
}

// consultations that already aggregated - should do nothing
func TestPickAlreadyAggregated(t *testing.T) {
	uc, ids, agg := newPick([]consultation.ConsultationPreview{{ID: "consultationId"}}, []string{"consultationId"})
	uc.AggregateConsultations(context.Background())
	mustEqual(t, "aggregated ids read", ids.calls, 1)
	mustEqual(t, "nothing aggregated", len(agg.done), 0)
}

// an exception stops the loop (the daily task logs it)
func TestPickStopsAtTheFirstError(t *testing.T) {
	uc, _, agg := newPick([]consultation.ConsultationPreview{{ID: "a"}, {ID: "b"}}, nil)
	agg.err = errors.New("boom")
	if err := uc.AggregateConsultations(context.Background()); err == nil {
		t.Fatal("expected the error")
	}
	mustEqual(t, "stopped", len(agg.done), 1)
}
