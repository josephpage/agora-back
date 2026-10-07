package referentiel

import (
	"testing"

	"agora/internal/domain"
	"agora/internal/jsonjava"
	"agora/internal/xmljava"
	"agora/parity/oracle"
)

func TestToJSON(t *testing.T) {
	body := ToJSON(domain.RegionValues, domain.PaysValues)
	if len(body.Regions) != len(domain.RegionValues) || len(body.Regions) == 0 {
		t.Fatalf("regions: %d", len(body.Regions))
	}
	first := body.Regions[0]
	if first.Region != "Auvergne-Rhône-Alpes" || len(first.Departements) != 12 || first.Departements[0] != (DepartementJSON{CodePostal: "01", Label: "Ain"}) {
		t.Fatalf("first region: %+v", first)
	}
	if len(body.Pays) != 2 || body.Pays[0] != "France" || body.Pays[1] != "Français de l'étranger" {
		t.Fatalf("pays: %v", body.Pays)
	}
}

func TestResponseBodyIsBuiltOnce(t *testing.T) {
	b1, raw1 := responseBody()
	b2, raw2 := responseBody()
	if string(raw1) != string(raw2) || len(b1.Regions) != len(b2.Regions) {
		t.Fatal("body changed")
	}
	if string(raw1) != jsonjava.MarshalString(b1) {
		t.Fatal("precomputed JSON differs from the encoded body")
	}
}

// TerritoiresJsonMapper + ReferentielController body against the JVM (JSON and XML bytes).
func TestOracleReferentielBody(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	var want struct {
		JSON string `json:"json"`
		XML  string `json:"xml"`
	}
	oracle.MustCall(t, "referentielBody", nil, &want)
	b, raw := responseBody()
	if string(raw) != want.JSON {
		t.Errorf("json differs:\n go:  %.300s\n jvm: %.300s", raw, want.JSON)
	}
	if got := string(xmljava.Marshal(b)); got != want.XML {
		t.Errorf("xml differs:\n go:  %.300s\n jvm: %.300s", got, want.XML)
	}
}
