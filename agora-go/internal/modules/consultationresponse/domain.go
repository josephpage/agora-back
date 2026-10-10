package consultationresponse

import (
	"sync"

	"agora/internal/domain"
	"agora/internal/modules/consultation"
)

// ---------------------------------------------------------------------------
// domain/ReponseConsultation.kt
// ---------------------------------------------------------------------------

// ResponseConsultationCount is domain.ResponseConsultationCount.
type ResponseConsultationCount struct {
	QuestionID    string
	ChoiceID      string
	ResponseCount int
}

// ReponseConsultationInserting is domain.ReponseConsultationInserting. ChoiceIDs
// is a Kotlin List<String>? whose elements may still be null at run time (Jackson
// keeps a JSON null inside a List<String>, the Kotlin module does not check list
// elements): a nil pointer is such a null.
type ReponseConsultationInserting struct {
	QuestionID   string
	ChoiceIDs    []*string
	ChoiceIDsNil bool // choiceIds == null (never produced by the controller: the JSON field is required)
	ResponseText string
}

// ---------------------------------------------------------------------------
// Enumerations of domain/Profile.kt. The keys of the demographic maps are
// ordinals, noKey being the null key of a Map<Enum?, V>.
// ---------------------------------------------------------------------------

// noKey is the null key of the demographic maps.
const noKey = -1

// The ordinals of the enums, in declaration order.
const (
	genderMasculin = iota
	genderFeminin
	genderAutre
)

// AgeRange ordinals.
const (
	ageLessThan18 = iota
	ageBetween18And25
	ageBetween26And35
	ageBetween36And45
	ageBetween46And55
	ageBetween56And65
	ageMoreThan65
)

// CityType ordinals.
const (
	cityRural = iota
	cityUrbain
	cityAutre
)

// JobCategory ordinals.
const (
	jobAgriculteur = iota
	jobArtisan
	jobCadre
	jobProfessionIntermediaire
	jobEmploye
	jobOuvrier
	jobEtudiants
	jobRetraites
	jobAutres
	jobUnknown
)

// Frequency ordinals.
const (
	freqSouvent = iota
	freqParfois
	freqJamais
)

// omap is a Kotlin LinkedHashMap<K?, V> with an enum (or null) key: iteration in
// insertion order, and putting an existing key keeps its first position.
type omap[V any] struct {
	keys []int
	vals map[int]V
}

func newOmap[V any]() *omap[V] { return &omap[V]{vals: map[int]V{}} }

func (m *omap[V]) put(k int, v V) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *omap[V]) get(k int) (V, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func (m *omap[V]) len() int { return len(m.keys) }

// The eight demographic attributes, in the order of the fields of DemographicInfoCount /
// DemographicInfo (genderCount, ageRangeCount, departmentCount, cityTypeCount,
// jobCategoryCount, voteFrequencyCount, publicMeetingFrequencyCount, consultationFrequencyCount).
const (
	dimGender = iota
	dimAgeRange
	dimDepartment
	dimCityType
	dimJobCategory
	dimVoteFrequency
	dimPublicMeetingFrequency
	dimConsultationFrequency
	numDims
)

// DemographicInfoCount is domain.DemographicInfoCount: the number of participants
// per value of each demographic attribute (one map per dim* constant).
type DemographicInfoCount [numDims]*omap[int]

func emptyDemographicInfoCount() *DemographicInfoCount {
	var d DemographicInfoCount
	for i := range d {
		d[i] = newOmap[int]()
	}
	return &d
}

// DemographicInfoCountByChoices is domain.DemographicInfoCountByChoices.
type DemographicInfoCountByChoices struct {
	Choices map[string]*DemographicInfoCount
}

// CountAndRatio is domain.CountAndRatio.
type CountAndRatio struct {
	Count int
	Ratio float64
}

// DemographicInfo is domain.DemographicInfo (one map per dim* constant).
type DemographicInfo [numDims]*omap[CountAndRatio]

// ChoiceResultWithDemographicInfo is domain.ChoiceResultWithDemographicInfo.
type ChoiceResultWithDemographicInfo struct {
	Choix         consultation.ChoixPossible
	CountAndRatio CountAndRatio
	Demographic   *DemographicInfo
}

// QuestionResultWithDemographicInfo is domain.QuestionResultWithDemographicInfo.
type QuestionResultWithDemographicInfo struct {
	Question  consultation.Question
	Responses []ChoiceResultWithDemographicInfo
}

// ConsultationResultWithDemographicInfo is domain.ConsultationResultWithDemographicInfo.
type ConsultationResultWithDemographicInfo struct {
	Consultation     *consultation.ConsultationInfo
	ParticipantCount int
	Results          []QuestionResultWithDemographicInfo
	Demographic      *DemographicInfo
}

// ---------------------------------------------------------------------------
// domain/ConsultationResults.kt, ConsultationResultAggregated.kt
// ---------------------------------------------------------------------------

// ChoiceResults is domain.ChoiceResults.
type ChoiceResults struct {
	Choix consultation.ChoixPossible
	Ratio float64
}

// QuestionResults is domain.QuestionResults.
type QuestionResults struct {
	Question  consultation.Question
	Responses []ChoiceResults
	SeenRatio float64
}

// ConsultationResults is domain.ConsultationResults. A value held by the cache is
// shared between requests and must never be modified.
type ConsultationResults struct {
	Consultation       *consultation.ConsultationInfo
	ParticipantCount   int
	ResultsWithChoices []QuestionResults
	OpenQuestions      []consultation.Question

	// the response body, built once per cached value
	once sync.Once
	dto  *ConsultationResultsJSON
	json []byte
}

// ConsultationResultAggregated is domain.ConsultationResultAggregated (the
// consultation_results table).
type ConsultationResultAggregated struct {
	ConsultationID string
	QuestionID     string
	ChoiceID       string
	ResponseCount  int
}

// ---------------------------------------------------------------------------
// The enum <-> database code conversions of ReponseConsultationMapper.
// ---------------------------------------------------------------------------

func toGender(s *string) int {
	if s == nil {
		return noKey
	}
	switch *s {
	case "M":
		return genderMasculin
	case "F":
		return genderFeminin
	case "A":
		return genderAutre
	}
	return noKey
}

func toCityType(s *string) int {
	if s == nil {
		return noKey
	}
	switch *s {
	case "R":
		return cityRural
	case "U":
		return cityUrbain
	case "A":
		return cityAutre
	}
	return noKey
}

func toJobCategory(s *string) int {
	if s == nil {
		return noKey
	}
	switch *s {
	case "AG":
		return jobAgriculteur
	case "AR":
		return jobArtisan
	case "CA":
		return jobCadre
	case "PI":
		return jobProfessionIntermediaire
	case "EM":
		return jobEmploye
	case "OU":
		return jobOuvrier
	case "ET":
		return jobEtudiants
	case "RE":
		return jobRetraites
	case "AU":
		return jobAutres
	case "UN":
		return jobUnknown
	}
	return noKey
}

func toFrequency(s *string) int {
	if s == nil {
		return noKey
	}
	switch *s {
	case "S":
		return freqSouvent
	case "P":
		return freqParfois
	case "J":
		return freqJamais
	}
	return noKey
}

// toDepartment is Department.findByCode(code).
func toDepartment(s *string) int {
	if d := domain.FindDepartmentByCode(s); d != nil {
		return d.Ordinal
	}
	return noKey
}
