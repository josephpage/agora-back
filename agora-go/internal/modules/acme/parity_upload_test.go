package acme

// Runner-free parity tests of POST /admin/acme/upload-certificate (see the header of
// parity_it_test.go for the environment).

import (
	"encoding/json"
	"fmt"
	"testing"
)

// The stored row (random IV, so the scenario cannot compare it) holds the posted PEMs after
// decryption, the status TO_DEPLOY and the same expires_at on both implementations.
func TestParityUploadRow(t *testing.T) {
	ref, gov := paritySides(t)
	type tc struct{ name, cert, key, expires string }
	for _, c := range []tc{
		{"plain", "CERTPEM", "KEYPEM", `"2030-06-01T10:20:30"`},
		{"no seconds", "CERTPEM", "KEYPEM", `"2030-06-01T10:20"`},
		{"millis", "a", "b", `"2030-06-01T10:20:30.123"`},
		{"micros", "a", "b", `"2030-06-01T10:20:30.123456"`},
		{"array", "a", "b", `[2030,6,1,10,20,30]`},
		{"multi-line unicode", "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----\n", "é😀\n", `"2030-06-01T10:20:30"`},
		{"empty strings", "", "", `"2030-06-01T10:20:30"`},
	} {
		certJSON, _ := json.Marshal(c.cert)
		keyJSON, _ := json.Marshal(c.key)
		body := fmt.Sprintf(`{"certificatePem":%s,"privateKeyPem":%s,"expiresAt":%s}`, certJSON, keyJSON, c.expires)
		both(ref, gov, func(s *pside) {
			s.reset(t)
			if st, _, b := s.do(t, "POST", "/admin/acme/upload-certificate", map[string]string{"Content-Type": "application/json"}, []byte(body)); st != 200 {
				t.Errorf("%s/%s: upload %d %s", c.name, s.name, st, b)
			}
		})
		rs, gs := ref.state(t), gov.state(t)
		if len(rs.Certs) != 1 || len(gs.Certs) != 1 {
			t.Fatalf("%s: rows ref=%d go=%d", c.name, len(rs.Certs), len(gs.Certs))
		}
		for _, p := range []struct {
			who string
			r   certRowT
		}{{"ref", rs.Certs[0]}, {"go", gs.Certs[0]}} {
			if p.r.Cert != c.cert || p.r.KeyPEM != c.key || p.r.Status != "TO_DEPLOY" || p.r.DeployedAt != nil || p.r.Domain != "local.test" {
				t.Errorf("%s/%s: row %+v", c.name, p.who, p.r)
			}
		}
		if !rs.Certs[0].ExpiresAt.Equal(gs.Certs[0].ExpiresAt) {
			t.Errorf("%s: expires_at ref=%v go=%v", c.name, rs.Certs[0].ExpiresAt, gs.Certs[0].ExpiresAt)
		}
	}
}

// More than 6 fraction digits: PostgreSQL rounds the parameter to the microsecond (half up), Go does the same.
func TestParityUploadNanos(t *testing.T) {
	ref, gov := paritySides(t)
	for _, frac := range []string{"1234567", "1234564", "1234565", "9999999", "123456789", "0000005", "0000004"} {
		body := `{"certificatePem":"a","privateKeyPem":"b","expiresAt":"2030-06-01T10:20:30.` + frac + `"}`
		both(ref, gov, func(s *pside) {
			s.reset(t)
			s.do(t, "POST", "/admin/acme/upload-certificate", map[string]string{"Content-Type": "application/json"}, []byte(body))
		})
		rs, gs := ref.state(t), gov.state(t)
		if len(rs.Certs) != 1 || len(gs.Certs) != 1 {
			t.Fatalf("%s: rows ref=%d go=%d", frac, len(rs.Certs), len(gs.Certs))
		}
		if !rs.Certs[0].ExpiresAt.Equal(gs.Certs[0].ExpiresAt) {
			t.Errorf("fraction %s: ref=%s go=%s", frac, rs.Certs[0].ExpiresAt.Format("15:04:05.000000"), gs.Certs[0].ExpiresAt.Format("15:04:05.000000"))
		}
	}
}
