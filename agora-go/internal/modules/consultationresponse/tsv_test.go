package consultationresponse

import (
	"math"
	"strings"
	"testing"

	"agora/internal/domain"
	"agora/internal/modules/consultation"
)

var en = numberFormatFor("en")

func TestNumberFormatEnglish(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"}, {math.Copysign(0, -1), "-0"}, {100, "100"}, {12.5, "12.5"}, {33.33333333, "33.3333"}, {66.66666666, "66.6667"},
		{0.78125, "0.7812"}, {2.34375, "2.3438"}, {0.03125, "0.0312"}, {0.09375, "0.0938"}, {1234567.891234, "1,234,567.8912"}, {1000, "1,000"}, {999.99999, "1,000"},
		{-1234.5, "-1,234.5"}, {-0.00001, "-0"}, {0.00005, "0.0001"}, {0.00015, "0.0001"}, {0.00025, "0.0003"}, {0.00045, "0.0004"}, {0.0001, "0.0001"}, {1e21, "1,000,000,000,000,000,000,000"},
		{math.NaN(), "NaN"}, {math.Inf(1), "∞"}, {math.Inf(-1), "-∞"}, {5e-324, "0"}, {2147483647 * 100, "214,748,364,700"},
	}
	for _, tt := range tests {
		if got := en.format(tt.in); got != tt.want {
			t.Errorf("format(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNumberFormatOtherLocales(t *testing.T) {
	mustEqual(t, "fr", numberFormatFor("fr").format(1234567.891234), "1 234 567,8912")
	mustEqual(t, "de", numberFormatFor("de").format(-1234.5), "-1.234,5")
	mustEqual(t, "unknown language is English", numberFormatFor("xx").format(1234.5), "1,234.5")
}

func TestProcessLanguage(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		env  map[string]string
		want string
	}{
		{nil, "en"}, {map[string]string{"LANG": "fr_FR.UTF-8"}, "fr"}, {map[string]string{"LC_ALL": "de_DE", "LANG": "fr_FR"}, "de"}, {map[string]string{"LC_CTYPE": "C.UTF-8"}, "en"},
		{map[string]string{"LANG": "POSIX"}, "en"}, {map[string]string{"LANG": "C"}, "en"}, {map[string]string{"LANG": "fr_FR", "JAVA_OPTS": "-Xmx1g -Duser.language=de -Duser.country=DE"}, "de"},
		{map[string]string{"JAVA_OPTS": "-Duser.language=de -Duser.language.format=fr"}, "fr"}, {map[string]string{"LANG": "pt_BR@euro"}, "pt"},
	}
	for _, tt := range tests {
		mustEqual(t, "language", processLanguage(env(tt.env)), tt.want)
	}
}

func TestPrettyLabels(t *testing.T) {
	for i, d := range domain.Departments {
		if departmentLabels[d.Name] == "" {
			t.Errorf("no label for the department %d %s", i, d.Name)
		}
	}
	mustEqual(t, "ain", prettyOf(dimDepartment, domain.FindDepartmentByCode(sp("01")).Ordinal), "1 - Ain")
	mustEqual(t, "corse", prettyOf(dimDepartment, domain.FindDepartmentByCode(sp("2A")).Ordinal), "2A - Corse-du-Sud")
	mustEqual(t, "etranger", prettyOf(dimDepartment, domain.FindDepartmentByCode(sp("99")).Ordinal), "Etranger - hors de France")
	for dim := 0; dim < numDims; dim++ {
		mustEqual(t, "null", prettyOf(dim, noKey), "N/A")
	}
	mustEqual(t, "gender", prettyOf(dimGender, genderFeminin), "Femme")
	mustEqual(t, "age", prettyOf(dimAgeRange, ageMoreThan65), "> 65 ans")
	mustEqual(t, "city", prettyOf(dimCityType, cityUrbain), "Urbain")
	mustEqual(t, "job", prettyOf(dimJobCategory, jobAutres), "Autres / Sans activité profesionnelle")
	mustEqual(t, "frequency", prettyOf(dimConsultationFrequency, freqJamais), "Jamais")
}

// the age ranges: 18 is "< 18", the limits of the others
func TestToAgeRange(t *testing.T) {
	year := 2026
	tests := []struct {
		yob  string
		want int
	}{
		{"2027", noKey}, {"2026", ageLessThan18}, {"2008", ageLessThan18}, {"2007", ageBetween18And25}, {"2001", ageBetween18And25}, {"2000", ageBetween26And35}, {"1991", ageBetween26And35},
		{"1990", ageBetween36And45}, {"1981", ageBetween36And45}, {"1980", ageBetween46And55}, {"1971", ageBetween46And55}, {"1970", ageBetween56And65}, {"1961", ageBetween56And65},
		{"1960", ageMoreThan65}, {"1900", ageMoreThan65}, {"abc", noKey}, {"", noKey}, {"+2000", ageBetween26And35}, {" 2000", noKey}, {"2000.5", noKey}, {"99999999999", noKey},
		{"-2147483648", noKey}, // Int overflow of the subtraction: negative age
	}
	for _, tt := range tests {
		y := tt.yob
		mustEqual(t, "yob "+tt.yob, toAgeRange(year, &y), tt.want)
	}
	mustEqual(t, "null", toAgeRange(year, nil), noKey)
}

func TestDemographicMapperCollisions(t *testing.T) {
	// `associate`: a later row with the same enum value (the null key too) replaces the count and keeps the first position
	rows := []demographicKeyed{{key: sp("M"), count: 1}, {key: nil, count: 2}, {key: sp("X"), count: 3}, {key: sp("F"), count: 4}, {key: sp("Z"), count: 5}}
	d := buildDemographicInfoCount(2026, [numDims][]demographicKeyed{rows})
	g := d[dimGender]
	if len(g.keys) != 3 || g.keys[0] != genderMasculin || g.keys[1] != noKey || g.keys[2] != genderFeminin {
		t.Fatalf("keys %v", g.keys)
	}
	mustEqual(t, "last null wins", g.vals[noKey], 5)
	// the age ranges are summed
	ages := []demographicKeyed{{key: sp("2000"), count: 1}, {key: sp("1999"), count: 2}, {key: sp("1500"), count: 4}, {key: sp("x"), count: 8}, {key: nil, count: 16}}
	d = buildDemographicInfoCount(2026, [numDims][]demographicKeyed{nil, ages})
	mustEqual(t, "26-35", d[dimAgeRange].vals[ageBetween26And35], 3)
	mustEqual(t, "more than 65", d[dimAgeRange].vals[ageMoreThan65], 4)
	mustEqual(t, "unknown values add up on the null key", d[dimAgeRange].vals[noKey], 24)
}

func TestDemographicRatios(t *testing.T) {
	count := emptyDemographicInfoCount()
	count[dimGender].put(genderMasculin, 3)
	count[dimGender].put(noKey, 100) // replaced: participants - the others
	count[dimGender].put(genderFeminin, 4)
	info := buildDemographicInfo(count, 10)
	g := info[dimGender]
	mustEqual(t, "man", g.vals[genderMasculin], CountAndRatio{3, 0.3})
	mustEqual(t, "null", g.vals[noKey], CountAndRatio{3, 0.3})
	mustEqual(t, "no participants", buildDemographicInfo(count, 0)[dimGender].vals[genderMasculin].Ratio, math.Inf(1))
	// the ratio of a choice is over the participants who have the value, 0 when nobody: Infinity, and 0 / 0 is 0
	choice := emptyDemographicInfoCount()
	choice[dimGender].put(genderMasculin, 2)
	choice[dimGender].put(genderAutre, 0)
	choice[dimGender].put(noKey, 7)
	cinfo := buildChoicesDemographicInfo(choice, count, 9)
	c := cinfo[dimGender]
	mustEqual(t, "man", c.vals[genderMasculin], CountAndRatio{2, 2.0 / 3})
	mustEqual(t, "other: 0 / 0", c.vals[genderAutre], CountAndRatio{0, 0})
	mustEqual(t, "null: 9 - 2 over the null count of the whole consultation (100)", c.vals[noKey], CountAndRatio{7, 0.07})
}

// a small export, written by hand from the Kotlin mapper
func TestBuildTsvBody(t *testing.T) {
	info := &DemographicInfo{}
	for i := range info {
		info[i] = newOmap[CountAndRatio]()
	}
	info[dimGender].put(noKey, CountAndRatio{1, 0.25})
	info[dimGender].put(genderFeminin, CountAndRatio{2, 0.5})
	info[dimAgeRange].put(ageBetween26And35, CountAndRatio{3, 0.75})
	choiceInfo := &DemographicInfo{}
	for i := range choiceInfo {
		choiceInfo[i] = newOmap[CountAndRatio]()
	}
	choiceInfo[dimGender].put(genderFeminin, CountAndRatio{1, 0.5})
	q := uniqueQ("q1", "a")
	q.Title = "Question <1>"
	r := &ConsultationResultWithDemographicInfo{
		Consultation:     &consultation.ConsultationInfo{Title: "Ma consultation"},
		ParticipantCount: 4,
		Demographic:      info,
		Results: []QuestionResultWithDemographicInfo{{Question: q, Responses: []ChoiceResultWithDemographicInfo{
			{Choix: consultation.ChoixPossible{ID: "a", Label: "Oui"}, CountAndRatio: CountAndRatio{3, 0.75}, Demographic: choiceInfo},
			{Choix: consultation.ChoixPossible{ID: "b", Label: "Non"}, CountAndRatio: CountAndRatio{0, 0}, Demographic: &DemographicInfo{newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio](), newOmap[CountAndRatio]()}},
		}}},
	}
	got := tsvMapper{nf: en}.BuildTsvBody(r)
	lines := strings.Split(got, "\n")
	want := []string{
		"Ma consultation",
		"4\tparticipants",
		"# Données socio-démographiques\t0 ~ 3\t0% ~ 75%",
		"### Genre\t#\t%",
		"=\"-- Femme\"\t2\t50%",
		"=\"-- N/A\"\t1\t25%", // the null key is last
		"### Tranche d'age\t#\t%",
		"=\"-- 26 à 35 ans\"\t3\t75%",
		"### Département ou collectivité d'outre mer\t#\t%",
		"### Habite en milieu\t#\t%",
		"### Catégorie socio-professionnelle\t#\t%",
		"### Fréquence de vote\t#\t%",
		"### Fréquence d'engagement sur le terrain\t#\t%",
		"### Fréquence d'engagement en ligne\t#\t%",
		"",
		"# Réponses aux questions",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d: %q, want %q\n%s", i, lines[i], w, got)
		}
	}
	// the question header repeats the keys of the whole consultation, the choice lines their counts (0 / 0% for a missing key)
	header := "## Question <1>\t#\t%\t### Genre\t=\"-- Femme\"\t=\"-- %\"\t=\"-- N/A\"\t=\"-- %\"\t### Tranche d'age\t=\"-- 26 à 35 ans\"\t=\"-- %\"\t### Département ou collectivité d'outre mer\t" +
		"### Habite en milieu\t### Catégorie socio-professionnelle\t### Fréquence de vote\t### Fréquence d'engagement sur le terrain\t### Fréquence d'engagement en ligne\t"
	mustEqual(t, "header", lines[16], header)
	mustEqual(t, "choice a", lines[17], "=\"- Oui\"\t3\t75%\t\t1\t50%\t0\t0%\t\t0\t0%\t\t\t\t\t\t\t")
	mustEqual(t, "choice b", lines[18], "=\"- Non\"\t0\t0%\t\t0\t0%\t0\t0%\t\t0\t0%\t\t\t\t\t\t\t")
	mustEqual(t, "ends with a line feed", strings.HasSuffix(got, "\n"), true)
}
