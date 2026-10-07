// Package referentiel ports ReferentielController (GET /referentiels/regions-et-departements)
// and TerritoiresJsonMapper: the regions with their departments, and the
// countries, straight from the Territoire enums. The body never changes, so it
// is built (and serialized) once.
package referentiel

import (
	"sync"

	"agora/internal/domain"
	"agora/internal/jsonjava"
)

// DepartementJSON is DepartementJson.
type DepartementJSON struct {
	CodePostal string `json:"codePostal"`
	Label      string `json:"label"`
}

// JavaName is the XML root element.
func (DepartementJSON) JavaName() string { return "DepartementJson" }

// RegionJSON is RegionJson.
type RegionJSON struct {
	Region       string            `json:"region"`
	Departements []DepartementJSON `json:"departements"`
}

// JavaName is the XML root element.
func (RegionJSON) JavaName() string { return "RegionJson" }

// TerritoiresJSON is TerritoiresJson.
type TerritoiresJSON struct {
	Regions []RegionJSON `json:"regions"`
	Pays    []string     `json:"pays"`
}

// JavaName is the XML root element.
func (TerritoiresJSON) JavaName() string { return "TerritoiresJson" }

// ToJSON is TerritoiresJsonMapper.toJson(Region.values(), Pays.values()).
func ToJSON(regions []*domain.Region, pays []*domain.Pays) TerritoiresJSON {
	out := TerritoiresJSON{Regions: make([]RegionJSON, len(regions)), Pays: make([]string, len(pays))}
	for i, region := range regions {
		deps := make([]DepartementJSON, len(region.Departements))
		for j, d := range region.Departements {
			deps[j] = DepartementJSON{CodePostal: d.CodePostal, Label: d.Value()}
		}
		out.Regions[i] = RegionJSON{Region: region.Value(), Departements: deps}
	}
	for i, p := range pays {
		out.Pays[i] = p.Value()
	}
	return out
}

var (
	once    sync.Once
	body    TerritoiresJSON
	bodyRaw []byte
)

// responseBody builds the (immutable) response body once.
func responseBody() (TerritoiresJSON, []byte) {
	once.Do(func() {
		body = ToJSON(domain.RegionValues, domain.PaysValues)
		bodyRaw = jsonjava.Marshal(body)
	})
	return body, bodyRaw
}
