package concertation

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"agora/internal/jsonjava"
	"agora/internal/strapi"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

type oracleResult struct {
	DecodeError bool   `json:"decodeError"`
	MapperError bool   `json:"mapperError"`
	JSON        string `json:"json"`
	XML         string `json:"xml"`
}

type obj = map[string]any

var oracleThematiques = []Thematique{
	{ID: "th1", Label: "Environnement", Picto: "🌳"}, {ID: "th2", Label: "Santé", Picto: "🏥"}, {ID: "th3", Label: "É\"d<u>&", Picto: ""},
	{ID: "th2", Label: "Doublon", Picto: "x"},
}

func randomConcertation(r *rand.Rand, n int, defect bool) obj {
	pick := func(l ...any) any { return l[r.Intn(len(l))] }
	date := pick("2026-03-01T10:00:00.000Z", "2026-03-01T10:00:00", "2026-03-02T09:30", "2026-03-01T10:00:00.123456", []any{2026, 3, 4, 10, 0}, []any{2026, 3, 5, 10, 0, 7, 5000},
		" 2026-03-06T10:00:00 ", "2026-03-01T10:00:00.000Z", "2026-03-03T10:00:00", "+12345-03-13T10:00:00", "0001-01-01T00:00:00")
	if defect && r.Intn(4) == 0 {
		date = pick("2026-03-07t10:00:00z", "2026-03-01", "2026-03-01T10:00:00+01:00", "", "nope", 1772359200000, nil, []any{2026, 13, 1, 0, 0}, true, 1.5)
	}
	var image any
	switch r.Intn(7) {
	case 0:
		image = obj{"url": "https://i/o.jpg", "formats": obj{"medium": obj{"url": "https://i/m.jpg"}}}
	case 1:
		image = obj{"url": "https://i/o.jpg", "formats": obj{"thumbnail": obj{"url": "t"}}}
	case 2:
		image = obj{"url": "https://i/o.jpg", "formats": nil}
	case 3:
		image = obj{"url": "https://i/o.jpg"}
	case 4:
		image = obj{"url": "", "formats": obj{"medium": obj{"url": ""}}}
	}
	if defect && r.Intn(6) == 0 {
		image = pick(obj{"formats": nil}, obj{"url": nil}, "x", 5, obj{"url": "u", "formats": obj{"medium": obj{}}})
	}
	var label any
	switch r.Intn(5) {
	case 0:
		label = "Nouveau"
	case 1:
		label = "🔥 <b>&amp;</b> \"q\" é\\"
	case 2:
		label = ""
	}
	c := obj{
		"id": n, "documentId": fmt.Sprintf("ce%d", r.Intn(1000)), "titre": pick("Titre", "É & <b> \"x\"", "", "日本語 🎉", 5, true, 1.5),
		"url": pick("https://c/1", "", "x y"), "image_url": pick("https://c/i.jpg", ""), "datetime_publication": date,
		"flamme_label": label, "image": image,
		"thematique": obj{"documentId": pick("th1", "th2", "th3", "th9", "TH1", ""), "label": "l", "pictogramme": "p", "id": 1, "createdAt": "x"},
		"createdAt":  "x", "updatedAt": "y", "publishedAt": "z", "locale": nil,
	}
	// defects: a missing or null required property, a wrong type
	if defect && r.Intn(2) == 0 {
		field := pick("documentId", "titre", "url", "image_url", "datetime_publication", "thematique").(string)
		switch r.Intn(3) {
		case 0:
			delete(c, field)
		case 1:
			c[field] = nil
		case 2:
			c[field] = pick([]any{"x"}, obj{"a": 1})
		}
	}
	if defect && r.Intn(4) == 0 {
		c["thematique"] = pick(obj{"documentId": "th1"}, obj{"documentId": "th1", "label": "l", "pictogramme": nil}, 5, "th1")
	}
	if r.Intn(10) == 0 {
		delete(c, "flamme_label")
	}
	return c
}

// ConcertationMapper + GetConcertationsUseCase + ConcertationJsonMapper against the JVM: same decoding outcome, same list
// (filter, order, image, dates), same JSON and XML bytes.
func TestOracleConcertations(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(5))
	counts := map[string]int{}
	for i := 0; i < 600; i++ {
		var data []any
		defective := r.Intn(4) == 0 // a quarter of the lists hold one defective concertation
		for k, n := 0, r.Intn(9); k < n; k++ {
			data = append(data, randomConcertation(r, k, defective && k == 0))
		}
		if r.Intn(30) == 0 {
			data = append(data, nil)
		}
		env, _ := json.Marshal(obj{"data": data, "meta": obj{"pagination": obj{"page": 1, "pageSize": 100, "pageCount": 1, "total": len(data)}}})
		var want oracleResult
		oracle.MustCall(t, "s5Concertations", map[string]any{"json": string(env), "thematiques": oracleThematiques4(), "nowMs": 0}, &want)

		var e strapi.Envelope[*strapiConcertation]
		decErr := jsonjava.Unmarshal(env, &e)
		if want.DecodeError {
			if decErr == nil {
				t.Fatalf("#%d: Kotlin cannot read the payload, Go can: %s", i, env)
			}
			counts["decode error"]++
			continue
		}
		if decErr != nil {
			t.Fatalf("#%d: Go cannot read the payload: %v\n%s", i, decErr, env)
		}
		list, panicked := goList(e.Data)
		if want.MapperError {
			if !panicked {
				t.Fatalf("#%d: Kotlin fails in the mapper, Go does not:\n%s", i, env)
			}
			counts["mapper exception"]++
			continue
		}
		if panicked {
			t.Fatalf("#%d: Go panics, Kotlin does not:\n%s", i, env)
		}
		body := toJSON(list)
		if got := jsonjava.MarshalString(body); got != want.JSON {
			t.Fatalf("#%d JSON:\n go:  %s\n jvm: %s\n%s", i, got, want.JSON, env)
		}
		// the oracle writes the ArrayList value; Spring's converter writes the declared type List<...>
		wantXML := strings.Replace(strings.Replace(want.XML, "<ArrayList", "<List", 1), "</ArrayList>", "</List>", 1)
		if got := string(xmljava.Marshal(body)); got != wantXML {
			t.Fatalf("#%d XML:\n go:  %s\n jvm: %s\n%s", i, got, wantXML, env)
		}
		counts[fmt.Sprintf("%d concertations", min(len(list), 3))]++
	}
	t.Logf("outcomes: %v", counts)
	if counts["decode error"] < 20 || counts["mapper exception"] == 0 || counts["3 concertations"] < 20 {
		t.Errorf("the generator does not cover the cases: %v", counts)
	}
}

func oracleThematiques4() []map[string]string {
	out := make([]map[string]string, len(oracleThematiques))
	for i, th := range oracleThematiques {
		out[i] = map[string]string{"id": th.ID, "label": th.Label, "picto": th.Picto}
	}
	return out
}

// goList is what Service.getAll does after the Strapi call.
func goList(data []*strapiConcertation) (list []Concertation, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	list = toConcertations(data, oracleThematiques)
	sortByUpdateDateDescending(list)
	return list, false
}
