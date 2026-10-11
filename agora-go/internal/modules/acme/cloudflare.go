package acme

import (
	"context"
	"fmt"
	"strings"

	"agora/internal/jsonjava"
)

// CloudflareDeployer is infrastructure/acme/repository/CloudflareCertificateDeployerImpl.kt.
type CloudflareDeployer struct {
	Cfg  *Config
	Rest *RestClient
}

// DeployCertificate POSTs {zones}/{zone}/custom_certificates and fails unless
// the answer is a 2xx (after the body has been extracted like RestTemplate does).
func (d *CloudflareDeployer) DeployCertificate(ctx context.Context, certificatePEM, privateKeyPEM string) error {
	target := d.Cfg.CloudflareBaseURL + "/zones/" + d.Cfg.CloudflareZoneID + "/custom_certificates"
	payload := jsonjava.Marshal(jsonjava.OrderedMap{
		{Key: "certificate", Value: certificatePEM},
		{Key: "private_key", Value: privateKeyPEM},
		{Key: "bundle_method", Value: "ubiquitous"},
		{Key: "type", Value: "sni_custom"},
	})
	res, err := d.Rest.Exchange(ctx, "POST", target, [][2]string{
		{"Authorization", "Bearer " + d.Cfg.CloudflareAPIToken},
		{"Content-Type", "application/json"},
	}, payload)
	if err != nil {
		return err
	}
	if res.Is2xx() {
		return nil
	}
	return &CloudflareDeploymentError{fmt.Sprintf("Cloudflare API returned %d for zone %s: %v", res.Status, d.Cfg.CloudflareZoneID, res.Body)}
}

// CloudflareDeploymentError is CloudflareDeploymentException.
type CloudflareDeploymentError struct{ Msg string }

func (e *CloudflareDeploymentError) Error() string { return e.Msg }

// CloudflareZoneInfo is usecase/acme/repository/CloudflareZoneInfo.
type CloudflareZoneInfo struct {
	ZoneID, Name, Status, Plan string
}

// CloudflareZoneCheckError is CloudflareZoneCheckException.
type CloudflareZoneCheckError struct{ Msg string }

func (e *CloudflareZoneCheckError) Error() string { return e.Msg }

// ZoneChecker is infrastructure/acme/repository/CloudflareZoneCheckerImpl.kt.
type ZoneChecker struct {
	Cfg  *Config
	Rest *RestClient
}

// GetZoneInfo is CloudflareZoneCheckerImpl.getZoneInfo.
func (z *ZoneChecker) GetZoneInfo(ctx context.Context) (*CloudflareZoneInfo, error) {
	target := z.Cfg.CloudflareBaseURL + "/zones/" + z.Cfg.CloudflareZoneID
	res, err := z.Rest.Exchange(ctx, "GET", target, [][2]string{
		{"Authorization", "Bearer " + z.Cfg.CloudflareAPIToken},
		{"Content-Type", "application/json"},
	}, nil)
	if err != nil {
		return nil, err
	}
	if res.Body == nil {
		return nil, &CloudflareZoneCheckError{"Cloudflare API returned empty body for zone " + z.Cfg.CloudflareZoneID}
	}
	if !res.Is2xx() {
		return nil, &CloudflareZoneCheckError{fmt.Sprintf("Cloudflare API returned %d for zone %s: %v", res.Status, z.Cfg.CloudflareZoneID, res.Body)}
	}
	result, ok := res.Body["result"].(map[string]any)
	if !ok {
		return nil, &CloudflareZoneCheckError{"Cloudflare API response missing 'result' field"}
	}
	plan := "unknown"
	if p, ok := result["plan"].(map[string]any); ok {
		if n, ok := p["name"].(string); ok {
			plan = n
		}
	}
	str := func(k string) string {
		if s, ok := result[k].(string); ok {
			return s
		}
		return "unknown"
	}
	return &CloudflareZoneInfo{ZoneID: z.Cfg.CloudflareZoneID, Name: str("name"), Status: str("status"), Plan: plan}, nil
}

// ServerDirectoryInfo is usecase/acme/repository/AcmeServerDirectoryInfo.
type ServerDirectoryInfo struct {
	ServerURL, NewAccountURL, NewOrderURL string
	TermsOfServiceURL                     *string
}

// ServerCheckError is AcmeServerCheckException.
type ServerCheckError struct{ Msg string }

func (e *ServerCheckError) Error() string { return e.Msg }

// ServerChecker is infrastructure/acme/repository/AcmeServerCheckerImpl.kt.
type ServerChecker struct {
	Cfg  *Config
	Rest *RestClient
}

// GetDirectoryInfo GETs "<ACME_SERVER_URL without trailing slashes>/directory".
func (s *ServerChecker) GetDirectoryInfo(ctx context.Context) (*ServerDirectoryInfo, error) {
	directoryURL := strings.TrimRight(s.Cfg.ServerURL, "/") + "/directory"
	res, err := s.Rest.Exchange(ctx, "GET", directoryURL, [][2]string{{"Accept", "application/json"}}, nil)
	if err != nil {
		return nil, err
	}
	if res.Body == nil {
		return nil, &ServerCheckError{"ACME server returned empty body from " + directoryURL}
	}
	if !res.Is2xx() {
		return nil, &ServerCheckError{fmt.Sprintf("ACME server returned %d from %s: %v", res.Status, directoryURL, res.Body)}
	}
	newAccount, ok := res.Body["newAccount"].(string)
	if !ok {
		return nil, &ServerCheckError{"ACME directory response missing 'newAccount' field from " + directoryURL}
	}
	newOrder, ok := res.Body["newOrder"].(string)
	if !ok {
		return nil, &ServerCheckError{"ACME directory response missing 'newOrder' field from " + directoryURL}
	}
	info := &ServerDirectoryInfo{ServerURL: s.Cfg.ServerURL, NewAccountURL: newAccount, NewOrderURL: newOrder}
	if meta, ok := res.Body["meta"].(map[string]any); ok {
		if t, ok := meta["termsOfService"].(string); ok {
			info.TermsOfServiceURL = &t
		}
	}
	return info, nil
}
