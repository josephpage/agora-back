package consultation

import (
	"context"
	"testing"
	"time"
)

func findHistory(list []UpdateHistory, id string) *UpdateHistory {
	for i := range list {
		if list[i].ConsultationUpdateID != nil && *list[i].ConsultationUpdateID == id {
			return &list[i]
		}
	}
	return nil
}

func historyOfSpec(t *testing.T, spec consultationSpec) []UpdateHistory {
	t.Helper()
	spec.id, spec.slug = "c", "s"
	return historyOf(decodeConsultation(t, spec.json()), func() time.Time { return baseTime })
}

// ConsultationUpdateHistoryMapperTest
func TestHistoryMapper(t *testing.T) {
	// only the content before the answer is DONE; the content after the answer is CURRENT when nothing else was published
	got := historyOfSpec(t, consultationSpec{end: pastDate.AddDate(0, 0, -5)})
	if a := findHistory(got, "avant-c"); a == nil || a.Status != HistoryDone || a.Type != HistoryUpdate {
		t.Fatalf("%+v", got)
	}
	if a := findHistory(got, "apres-c"); a == nil || a.Status != HistoryCurrent || a.Type != HistoryResults {
		t.Fatalf("%+v", got)
	}

	// the incoming entry (no id, no date, never filtered out)
	got = historyOfSpec(t, consultationSpec{aVenir: obj{"documentId": "av", "titre_historique": "Prochain contenu"}})
	if got[0].Status != HistoryIncoming || got[0].Title != "Prochain contenu" || got[0].ConsultationUpdateID != nil || got[0].UpdateDate != nil || got[0].Slug != nil || got[0].ActionText != nil {
		t.Fatalf("%+v", got[0])
	}
	for _, h := range historyOfSpec(t, consultationSpec{}) {
		if h.Status == HistoryIncoming {
			t.Fatal("no incoming entry without contenu a venir")
		}
	}

	// two other contents in the past: the newer is CURRENT, the older DONE, newest first after the fixed entries
	got = historyOfSpec(t, consultationSpec{autres: []obj{
		autreContenu("autre-older", pastDate.AddDate(0, 0, -5), nil), autreContenu("autre-newer", pastDate, nil), autreContenu("autre-future", futureDate, nil)},
		end: futureDate})
	if findHistory(got, "autre-older").Status != HistoryDone || findHistory(got, "autre-newer").Status != HistoryCurrent || findHistory(got, "autre-future") != nil {
		t.Fatalf("%+v", got)
	}
	if got[len(got)-1].ConsultationUpdateID == nil || *got[len(got)-1].ConsultationUpdateID != "autre-older" || *got[len(got)-2].ConsultationUpdateID != "autre-newer" {
		t.Fatal("other contents are sorted by decreasing date")
	}
	// the results entry is in the future while the consultation is running
	if findHistory(got, "apres-c") != nil {
		t.Fatal("the results entry is filtered out until the end date")
	}

	// the sponsor's answer and the analysis: the sponsor's answer is the current one when it exists
	got = historyOfSpec(t, consultationSpec{end: pastDate,
		analyse: analyseContenu("an", pastDate, nil), commanditaire: commanditaireContenu("cm", pastDate, nil)})
	if findHistory(got, "cm").Status != HistoryCurrent || findHistory(got, "an").Status != HistoryDone || findHistory(got, "apres-c").Status != HistoryDone {
		t.Fatalf("%+v", got)
	}
	got = historyOfSpec(t, consultationSpec{end: pastDate, analyse: analyseContenu("an", pastDate, nil)})
	if findHistory(got, "an").Status != HistoryCurrent {
		t.Fatal("the analysis is current when there is no sponsor's answer and no other content")
	}
	// an analysis published in the future is still the "current" one (it is only filtered out by its date)
	got = historyOfSpec(t, consultationSpec{end: pastDate, analyse: analyseContenu("an", futureDate, nil)})
	if findHistory(got, "an") != nil || findHistory(got, "apres-c").Status != HistoryDone {
		t.Fatalf("%+v", got)
	}

	// dates are java.util.Date (milliseconds), the end of the consultation is the date of the results
	got = historyOfSpec(t, consultationSpec{end: pastDate})
	if d := findHistory(got, "apres-c").UpdateDate; d == nil || !d.Equal(pastDate) {
		t.Fatalf("%v", d)
	}
}

func TestHistoryDateIsTruncatedToMilliseconds(t *testing.T) {
	spec := consultationSpec{id: "c", slug: "s", autres: []obj{autreContenu("a", pastDate, obj{"datetime_publication": "2026-05-22T12:00:00.123456789"})}}
	got := historyOf(decodeConsultation(t, spec.json()), func() time.Time { return baseTime })
	if d := findHistory(got, "a").UpdateDate; d.Nanosecond() != 123000000 {
		t.Fatalf("%d", d.Nanosecond())
	}
}

// ConsultationUpdateHistoryRepositoryImplTest
func TestHistoryRepositoryCache(t *testing.T) {
	ta := newTestApp(t)
	ta.strapi.set(consultationSpec{id: "c1", slug: "s1", autres: []obj{autreContenu("a1", pastDate, nil)}}.json())
	repo := &HistoryRepository{a: ta.App, strapi: ta.strapiRepo()}
	ctx := context.Background()

	first, err := repo.GetConsultationUpdateHistory(ctx, "c1")
	if err != nil || len(first) == 0 {
		t.Fatalf("%v %v", first, err)
	}
	if ta.strapi.count() != 1 {
		t.Fatalf("one Strapi request: %d", ta.strapi.count())
	}
	second, _ := repo.GetConsultationUpdateHistory(ctx, "c1")
	if ta.strapi.count() != 1 || len(second) != len(first) {
		t.Fatal("the second call comes from the cache")
	}

	// an unknown consultation is an empty history, not cached
	empty, _ := repo.GetConsultationUpdateHistory(ctx, "unknown")
	if len(empty) != 0 || empty == nil {
		t.Fatalf("%v", empty)
	}
	n := ta.strapi.count()
	repo.GetConsultationUpdateHistory(ctx, "unknown")
	if ta.strapi.count() != n {
		// the DTO of an unknown id is not cached by the by-id cache either (a cached null is a miss): Strapi is asked again
		if ta.strapi.count() != n+1 {
			t.Fatalf("%d requests", ta.strapi.count()-n)
		}
	}
}
