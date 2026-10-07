package profile

import (
	"strconv"

	"agora/internal/domain"
	"agora/internal/javacompat"
)

// ProfileJSON is infrastructure/profile/ProfileJson (request and response).
// Kotlin declares every property as String? without default: a missing or
// null property is null, numbers and booleans are coerced to their text.
type ProfileJSON struct {
	Gender                 *string `json:"gender"`
	YearOfBirth            *string `json:"yearOfBirth"`
	Department             *string `json:"department"`
	CityType               *string `json:"cityType"`
	JobCategory            *string `json:"jobCategory"`
	VoteFrequency          *string `json:"voteFrequency"`
	PublicMeetingFrequency *string `json:"publicMeetingFrequency"`
	ConsultationFrequency  *string `json:"consultationFrequency"`
	PrimaryDepartment      *string `json:"primaryDepartment"`
	SecondaryDepartment    *string `json:"secondaryDepartment"`
}

// JavaName is the XML root element.
func (ProfileJSON) JavaName() string { return "ProfileJson" }

// ProfileDepartmentJSON is infrastructure/profile/ProfileDepartmentJson. The
// elements are pointers: Jackson accepts a null element in a List<String>,
// which later fails with a NullPointerException.
type ProfileDepartmentJSON struct {
	Departments []*string `json:"departments"`
}

// ToDomain is ProfileJsonMapper.toDomain.
func ToDomain(json ProfileJSON, userID string) ProfileInserting {
	var year *int
	if json.YearOfBirth != nil {
		if v, ok := javacompat.KotlinToIntOrNull(*json.YearOfBirth); ok {
			year = &v
		}
	}
	return ProfileInserting{
		Gender:                 toGender(json.Gender),
		YearOfBirth:            year,
		Department:             domain.FindDepartmentByCode(json.Department),
		CityType:               toCityType(json.CityType),
		JobCategory:            toJobCategory(json.JobCategory),
		VoteFrequency:          toFrequency(json.VoteFrequency),
		PublicMeetingFrequency: toFrequency(json.PublicMeetingFrequency),
		ConsultationFrequency:  toFrequency(json.ConsultationFrequency),
		UserID:                 userID,
	}
}

// ToJSON is ProfileJsonMapper.toJson.
func ToJSON(p Profile) ProfileJSON {
	var year *string
	if p.YearOfBirth != nil {
		s := strconv.Itoa(*p.YearOfBirth)
		year = &s
	}
	var primary, secondary *string
	if p.PrimaryDepartment != nil {
		v := p.PrimaryDepartment.Value()
		primary = &v
	}
	if p.SecondaryDepartment != nil {
		v := p.SecondaryDepartment.Value()
		secondary = &v
	}
	return ProfileJSON{
		Gender:                 fromGender(p.Gender),
		YearOfBirth:            year,
		Department:             domain.DepartmentCode(p.Department),
		CityType:               fromCityType(p.CityType),
		JobCategory:            fromJobCategory(p.JobCategory),
		VoteFrequency:          fromFrequency(p.VoteFrequency),
		PublicMeetingFrequency: fromFrequency(p.PublicMeetingFrequency),
		ConsultationFrequency:  fromFrequency(p.ConsultationFrequency),
		PrimaryDepartment:      primary,
		SecondaryDepartment:    secondary,
	}
}
