package thematique

// ThematiquesJSON is ThematiquesJson (GET /thematiques body).
type ThematiquesJSON struct {
	Thematiques []ThematiqueJSON `json:"thematiques"`
}

// JavaName is the XML root element.
func (ThematiquesJSON) JavaName() string { return "ThematiquesJson" }

// ThematiqueJSON is ThematiqueJson.
type ThematiqueJSON struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Picto string `json:"picto"`
}

// JavaName is the XML root element.
func (ThematiqueJSON) JavaName() string { return "ThematiqueJson" }

// ThematiqueNoIDJSON is ThematiqueNoIdJson, embedded by the consultation,
// concertation, fiche inventaire, QaG and response QaG DTOs.
type ThematiqueNoIDJSON struct {
	Label string `json:"label"`
	Picto string `json:"picto"`
}

// JavaName is the XML root element.
func (ThematiqueNoIDJSON) JavaName() string { return "ThematiqueNoIdJson" }

// ToJSON is ThematiqueJsonMapper.toJson(domain).
func ToJSON(t Thematique) ThematiqueJSON {
	return ThematiqueJSON{ID: t.ID, Label: t.Label, Picto: t.Picto}
}

// ToListJSON is ThematiqueJsonMapper.toJson(List<Thematique>).
func ToListJSON(list []Thematique) ThematiquesJSON {
	out := make([]ThematiqueJSON, len(list))
	for i, t := range list {
		out[i] = ToJSON(t)
	}
	return ThematiquesJSON{Thematiques: out}
}

// ToNoIDJSON is ThematiqueJsonMapper.toNoIdJson(domain).
func ToNoIDJSON(t Thematique) ThematiqueNoIDJSON {
	return ThematiqueNoIDJSON{Label: t.Label, Picto: t.Picto}
}

// strapiThematique is ThematiqueStrapiDTO (a missing/null field makes the
// whole Strapi list undecodable, i.e. empty, like in Kotlin).
type strapiThematique struct {
	DocumentID  string `json:"documentId"`
	Label       string `json:"label"`
	Pictogramme string `json:"pictogramme"`
}

// toDomain is ThematiqueMapper.toDomain(dto).
func toDomain(dto strapiThematique) Thematique {
	return Thematique{ID: dto.DocumentID, Label: dto.Label, Picto: dto.Pictogramme}
}

// toDomainList is ThematiqueMapper.toDomain(StrapiDTO<ThematiqueStrapiDTO>).
// A JSON null element is accepted by Jackson (type erasure) but the Kotlin
// mapper then throws on its non-null parameter (→ 500): panic.
func toDomainList(data []*strapiThematique) []Thematique {
	out := make([]Thematique, len(data))
	for i, d := range data {
		if d == nil {
			panic("NullPointerException: Parameter specified as non-null is null: ThematiqueMapper.toDomain, parameter dto")
		}
		out[i] = toDomain(*d)
	}
	return out
}
