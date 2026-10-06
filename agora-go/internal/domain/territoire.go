package domain

import (
	"strings"

	"agora/internal/javacompat"
)

// Territoire is the Kotlin interface fr.gouv.agora.domain.Territoire.
type Territoire interface {
	Value() string
	EnumName() string
}

// Value returns the display value.
func (p *Pays) Value() string { return p.value }

// EnumName returns the Kotlin enum constant name.
func (p *Pays) EnumName() string { return p.Name }

// Value returns the display value.
func (r *Region) Value() string { return r.value }

// EnumName returns the Kotlin enum constant name.
func (r *Region) EnumName() string { return r.Name }

// Value returns the display value.
func (d *Departement) Value() string { return d.value }

// EnumName returns the Kotlin enum constant name.
func (d *Departement) EnumName() string { return d.Name }

// InvalidTerritoryError is InvalidTerritoryException (advice → 400 {"title"}).
type InvalidTerritoryError struct{ Territoire string }

func (e *InvalidTerritoryError) Error() string {
	return "Le territoire " + e.Territoire + " n'existe pas."
}

// ErrInvalidNumberOfDepartments is InvalidNumberOfDepartmentsException.
const InvalidNumberOfDepartmentsMessage = "Vous devez renseigner entre 0 et 2 départements."

func eqLower(a, b string) bool { return javacompat.KotlinLowercase(a) == javacompat.KotlinLowercase(b) }

// TerritoireFrom is Territoire.from(territoire): Pays, then Region, then
// Departement, first value matching case-insensitively (lowercase()).
func TerritoireFrom(t string) (Territoire, error) {
	for _, p := range PaysValues {
		if eqLower(p.value, t) {
			return p, nil
		}
	}
	for _, r := range RegionValues {
		if eqLower(r.value, t) {
			return r, nil
		}
	}
	for _, d := range DepartementValues {
		if eqLower(d.value, t) {
			return d, nil
		}
	}
	return nil, &InvalidTerritoryError{Territoire: t}
}

// DepartementFromOrThrow is Territoire.Departement.fromOrThrow.
func DepartementFromOrThrow(v string) (*Departement, error) {
	if d := DepartementFrom(v); d != nil {
		return d, nil
	}
	return nil, &InvalidTerritoryError{Territoire: v}
}

// DepartementFrom is Territoire.Departement.from (nil when unknown).
func DepartementFrom(v string) *Departement {
	for _, d := range DepartementValues {
		if eqLower(d.value, v) {
			return d
		}
	}
	return nil
}

// DepartementFromCodePostal is Territoire.Departement.fromCodePostal.
func DepartementFromCodePostal(code string) *Departement {
	for _, d := range DepartementValues {
		if d.CodePostal == code {
			return d
		}
	}
	return nil
}

// RegionByDepartment is Territoire.Region.getByDepartment.
func RegionByDepartment(d *Departement) *Region {
	if d == nil {
		return nil
	}
	for _, r := range RegionValues {
		for _, rd := range r.Departements {
			if rd == d {
				return r
			}
		}
	}
	return nil
}

// FindDepartmentByCode is Department.findByCode (name ends with "_<code>").
func FindDepartmentByCode(code *string) *Department {
	if code == nil {
		return nil
	}
	for _, d := range Departments {
		if strings.HasSuffix(d.Name, "_"+*code) {
			return d
		}
	}
	return nil
}

// DepartmentCode is Department.getDepartmentCode (substringAfterLast("_")).
func DepartmentCode(d *Department) *string {
	if d == nil {
		return nil
	}
	i := strings.LastIndex(d.Name, "_")
	c := d.Name[i+1:]
	return &c
}

// DepartmentByName is Department.valueOf(name) (nil when unknown).
func DepartmentByName(name string) *Department {
	for _, d := range Departments {
		if d.Name == name {
			return d
		}
	}
	return nil
}
