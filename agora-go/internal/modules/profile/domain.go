// Package profile ports the user profile slice: GET/POST /profile,
// POST /profile/departments, the demographic information ask logic and their
// repositories (users_profile, demographic_info_ask_date).
package profile

import (
	"agora/internal/domain"
)

// The nullable Kotlin enums are string types holding the enum constant name;
// the empty string is null.

// Gender is domain/Gender (MASCULIN, FEMININ, AUTRE).
type Gender string

// CityType is domain/CityType (RURAL, URBAIN, AUTRE).
type CityType string

// JobCategory is domain/JobCategory.
type JobCategory string

// Frequency is domain/Frequency (SOUVENT, PARFOIS, JAMAIS).
type Frequency string

// Enum constants.
const (
	GenderMasculin Gender = "MASCULIN"
	GenderFeminin  Gender = "FEMININ"
	GenderAutre    Gender = "AUTRE"

	CityRural  CityType = "RURAL"
	CityUrbain CityType = "URBAIN"
	CityAutre  CityType = "AUTRE"

	JobAgriculteur             JobCategory = "AGRICULTEUR"
	JobArtisan                 JobCategory = "ARTISAN"
	JobCadre                   JobCategory = "CADRE"
	JobProfessionIntermediaire JobCategory = "PROFESSION_INTERMEDIAIRE"
	JobEmploye                 JobCategory = "EMPLOYE"
	JobOuvrier                 JobCategory = "OUVRIER"
	JobEtudiants               JobCategory = "ETUDIANTS"
	JobRetraites               JobCategory = "RETRAITES"
	JobAutres                  JobCategory = "AUTRESOUSANSACTIVITEPRO"
	JobUnknown                 JobCategory = "UNKNOWN"

	FrequencySouvent Frequency = "SOUVENT"
	FrequencyParfois Frequency = "PARFOIS"
	FrequencyJamais  Frequency = "JAMAIS"
)

// Profile is domain/Profile.
type Profile struct {
	Gender                 Gender
	YearOfBirth            *int
	Department             *domain.Department
	CityType               CityType
	JobCategory            JobCategory
	VoteFrequency          Frequency
	PublicMeetingFrequency Frequency
	ConsultationFrequency  Frequency
	PrimaryDepartment      *domain.Departement
	SecondaryDepartment    *domain.Departement
}

// IsCompleted is Profile.isCompleted (the departments are not considered).
func (p Profile) IsCompleted() bool {
	return p.Gender != "" || p.YearOfBirth != nil || p.Department != nil ||
		p.CityType != "" || p.JobCategory != "" ||
		p.VoteFrequency != "" || p.PublicMeetingFrequency != "" ||
		p.ConsultationFrequency != ""
}

// ProfileInserting is domain/ProfileInserting.
type ProfileInserting struct {
	Gender                 Gender
	YearOfBirth            *int
	Department             *domain.Department
	CityType               CityType
	JobCategory            JobCategory
	VoteFrequency          Frequency
	PublicMeetingFrequency Frequency
	ConsultationFrequency  Frequency
	UserID                 string
}

// ProfileEditResult is usecase/profile/repository/ProfileEditResult.
type ProfileEditResult int

// ProfileEditResult values.
const (
	ProfileEditSuccess ProfileEditResult = iota
	ProfileEditFailure
)

// The code <-> enum conversions below are duplicated, identical, in
// ProfileJsonMapper and ProfileMapper.

func toGender(s *string) Gender {
	if s == nil {
		return ""
	}
	switch *s {
	case "M":
		return GenderMasculin
	case "F":
		return GenderFeminin
	case "A":
		return GenderAutre
	}
	return ""
}

func fromGender(g Gender) *string {
	switch g {
	case GenderMasculin:
		return sp("M")
	case GenderFeminin:
		return sp("F")
	case GenderAutre:
		return sp("A")
	}
	return nil
}

func toCityType(s *string) CityType {
	if s == nil {
		return ""
	}
	switch *s {
	case "R":
		return CityRural
	case "U":
		return CityUrbain
	case "A":
		return CityAutre
	}
	return ""
}

func fromCityType(c CityType) *string {
	switch c {
	case CityRural:
		return sp("R")
	case CityUrbain:
		return sp("U")
	case CityAutre:
		return sp("A")
	}
	return nil
}

var jobCodes = []struct {
	code string
	job  JobCategory
}{
	{"AG", JobAgriculteur}, {"AR", JobArtisan}, {"CA", JobCadre}, {"PI", JobProfessionIntermediaire},
	{"EM", JobEmploye}, {"OU", JobOuvrier}, {"ET", JobEtudiants}, {"RE", JobRetraites},
	{"AU", JobAutres}, {"UN", JobUnknown},
}

func toJobCategory(s *string) JobCategory {
	if s == nil {
		return ""
	}
	for _, j := range jobCodes {
		if j.code == *s {
			return j.job
		}
	}
	return ""
}

func fromJobCategory(j JobCategory) *string {
	for _, e := range jobCodes {
		if e.job == j {
			return sp(e.code)
		}
	}
	return nil
}

func toFrequency(s *string) Frequency {
	if s == nil {
		return ""
	}
	switch *s {
	case "S":
		return FrequencySouvent
	case "P":
		return FrequencyParfois
	case "J":
		return FrequencyJamais
	}
	return ""
}

func fromFrequency(f Frequency) *string {
	switch f {
	case FrequencySouvent:
		return sp("S")
	case FrequencyParfois:
		return sp("P")
	case FrequencyJamais:
		return sp("J")
	}
	return nil
}

func sp(s string) *string { return &s }
