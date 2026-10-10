package consultationresponse

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"agora/internal/domain"
)

// tsvMapper is infrastructure/consultationResults/ConsultationResultWithDemographicInfoTsvMapper.
type tsvMapper struct{ nf javaNumberFormat }

// The titles of the eight blocks of the export, in the order of the dim* constants.
var tsvDimTitles = [numDims]string{
	"Genre", "Tranche d'age", "Département ou collectivité d'outre mer", "Habite en milieu", "Catégorie socio-professionnelle",
	"Fréquence de vote", "Fréquence d'engagement sur le terrain", "Fréquence d'engagement en ligne",
}

// prettyOf is the toPretty() of each enum, by dim and ordinal (noKey = null = "N/A").
func prettyOf(dim, key int) string {
	if key == noKey {
		return "N/A"
	}
	switch dim {
	case dimGender:
		return [...]string{"Homme", "Femme", "Autre"}[key]
	case dimAgeRange:
		return [...]string{"< 18 ans", "18 à 25 ans", "26 à 35 ans", "36 à 45 ans", "46 à 55 ans", "56 à 65 ans", "> 65 ans"}[key]
	case dimDepartment:
		return departmentLabels[domain.Departments[key].Name]
	case dimCityType:
		return [...]string{"Rural", "Urbain", "Autre"}[key]
	case dimJobCategory:
		return [...]string{"Agriculteurs", "Artisans, commerçants, chefs d'entreprise", "Cadres", "Professions intermediaires", "Employés",
			"Ouvriers", "Etudiants", "Retraités", "Autres / Sans activité profesionnelle", "Ne sait pas"}[key]
	}
	return [...]string{"Souvent", "Parfois", "Jamais"}[key] // the three frequencies
}

// BuildTsvBody is buildTsvBody.
func (m tsvMapper) BuildTsvBody(r *ConsultationResultWithDemographicInfo) string {
	socioDemogCount, socioDemogRatio := m.buildSocioDemogTotalCountAndRatio(r)
	var sb strings.Builder
	sb.WriteString(m.buildHeader(r))
	sb.WriteString("# Données socio-démographiques\t" + socioDemogCount + "\t" + socioDemogRatio)
	sb.WriteString("\n")
	sb.WriteString(m.buildDemographicInfoBloc(r.Demographic))
	sb.WriteString("\n")
	sb.WriteString("# Réponses aux questions")
	sb.WriteString("\n")
	sb.WriteString(m.buildConsultationResponsesBloc(r))
	sb.WriteString("\n")
	return sb.String()
}

func (m tsvMapper) buildSocioDemogTotalCountAndRatio(r *ConsultationResultWithDemographicInfo) (string, string) {
	var all [numDims]int
	for dim := range all {
		all[dim] = nonNullCountAndRatio(r.Demographic[dim])
	}
	minCount, maxCount := all[0], all[0]
	for _, c := range all {
		minCount, maxCount = min(minCount, c), max(maxCount, c)
	}
	minRatio := ratioOrZeroFloat(float64(minCount) / float64(r.ParticipantCount))
	maxRatio := ratioOrZeroFloat(float64(maxCount) / float64(r.ParticipantCount))
	return strconv.Itoa(minCount) + " ~ " + strconv.Itoa(maxCount),
		m.nf.format(100*minRatio) + "% ~ " + m.nf.format(100*maxRatio) + "%"
}

func ratioOrZeroFloat(r float64) float64 {
	if math.IsNaN(r) {
		return 0.0
	}
	return r
}

// nonNullCountAndRatio is getNonNullDemographicInfoCount (Int sum).
func nonNullCountAndRatio(mp *omap[CountAndRatio]) int {
	sum := int32(0)
	for _, key := range mp.keys {
		if key != noKey {
			sum += int32(mp.vals[key].Count)
		}
	}
	return int(sum)
}

func (m tsvMapper) buildHeader(r *ConsultationResultWithDemographicInfo) string {
	return r.Consultation.Title + "\n" + strconv.Itoa(r.ParticipantCount) + "\tparticipants" + "\n"
}

// sortedKeys is `toList().sortedBy { key?.ordinal ?: Integer.MAX_VALUE }` (stable: the null key last).
func sortedKeys[V any](mp *omap[V]) []int {
	keys := append([]int(nil), mp.keys...)
	sort.SliceStable(keys, func(i, j int) bool { return ordinalOf(keys[i]) < ordinalOf(keys[j]) })
	return keys
}

func ordinalOf(key int) int {
	if key == noKey {
		return math.MaxInt32
	}
	return key
}

func (m tsvMapper) buildDemographicInfoBloc(info *DemographicInfo) string {
	var sb strings.Builder
	for dim := 0; dim < numDims; dim++ {
		sb.WriteString("### " + tsvDimTitles[dim] + "\t#\t%")
		sb.WriteString("\n")
		for _, key := range sortedKeys(info[dim]) {
			sb.WriteString("=\"-- " + prettyOf(dim, key) + "\"\t" + m.countAndRatio(info[dim].vals[key], true))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m tsvMapper) buildConsultationResponsesBloc(r *ConsultationResultWithDemographicInfo) string {
	var sb strings.Builder
	headers := m.buildDemographicHeaders(r.Demographic)
	for _, questionResult := range r.Results {
		sb.WriteString("## " + questionResult.Question.Title + "\t#\t%")
		sb.WriteString("\t" + headers)
		sb.WriteString("\n")
		for _, choiceResult := range questionResult.Responses {
			sb.WriteString("=\"- " + choiceResult.Choix.Label + "\"\t" + m.countAndRatio(choiceResult.CountAndRatio, true))
			sb.WriteString("\t" + m.buildDemographicValues(r.Demographic, choiceResult.Demographic))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m tsvMapper) buildDemographicHeaders(info *DemographicInfo) string {
	var sb strings.Builder
	for dim := 0; dim < numDims; dim++ {
		sb.WriteString("### " + tsvDimTitles[dim])
		sb.WriteString("\t")
		for _, key := range sortedKeys(info[dim]) {
			sb.WriteString("=\"-- " + prettyOf(dim, key) + "\"")
			sb.WriteString("\t=\"-- %\"\t")
		}
	}
	return sb.String()
}

func (m tsvMapper) buildDemographicValues(global, choice *DemographicInfo) string {
	var sb strings.Builder
	for dim := 0; dim < numDims; dim++ {
		sb.WriteString("\t")
		for _, key := range sortedKeys(global[dim]) {
			if cr, ok := choice[dim].get(key); ok {
				sb.WriteString(m.countAndRatio(cr, true))
			} else {
				sb.WriteString("0\t0%") // null.toPretty()
			}
			sb.WriteString("\t")
		}
	}
	return sb.String()
}

// countAndRatio is CountAndRatio?.toPretty(): "<count>\t<100 * ratio>%".
func (m tsvMapper) countAndRatio(cr CountAndRatio, _ bool) string {
	return strconv.Itoa(cr.Count) + "\t" + m.nf.format(100*cr.Ratio) + "%"
}
