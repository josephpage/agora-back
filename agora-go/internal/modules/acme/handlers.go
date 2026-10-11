package acme

import (
	"context"
	"time"

	"agora/internal/httpx"
	"agora/internal/modules/content"
)

// AcmeCertificateUploadJSON is AcmeCertificateUploadJson (request body).
type AcmeCertificateUploadJSON struct {
	CertificatePem string                `json:"certificatePem"`
	PrivateKeyPem  string                `json:"privateKeyPem"`
	ExpiresAt      content.LocalDateTime `json:"expiresAt"`
}

// CloudflareZoneInfoJSON is CloudflareZoneInfoJson.
type CloudflareZoneInfoJSON struct {
	ZoneID string `json:"zoneId"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Plan   string `json:"plan"`
}

// JavaName is the XML root element.
func (CloudflareZoneInfoJSON) JavaName() string { return "CloudflareZoneInfoJson" }

// AcmeServerDirectoryJSON is AcmeServerDirectoryJson.
type AcmeServerDirectoryJSON struct {
	ServerURL         string  `json:"serverUrl"`
	NewAccountURL     string  `json:"newAccountUrl"`
	NewOrderURL       string  `json:"newOrderUrl"`
	TermsOfServiceURL *string `json:"termsOfServiceUrl"`
}

// JavaName is the XML root element.
func (AcmeServerDirectoryJSON) JavaName() string { return "AcmeServerDirectoryJson" }

// MessageJSON is `mapOf("message" to text)` returned through ResponseEntity<Any>:
// a java.util.Collections$SingletonMap.
type MessageJSON struct {
	Message string `json:"message"`
}

// JavaName is the XML root element (runtime class of Kotlin's mapOf(pair)).
func (MessageJSON) JavaName() string { return "SingletonMap" }

const (
	renewSuccessMessage  = "Traitement de renouvellement du certificat ACME exécuté avec succès"
	uploadSuccessMessage = "Certificat enregistré avec succès avec le statut TO_DEPLOY"
)

// detached keeps running when the HTTP client goes away: the servlet thread of
// the Kotlin application is not interrupted either.
func detached(c *httpx.Ctx) context.Context { return context.WithoutCancel(c.Context()) }

// challengeHandler is AcmeController.getChallenge.
func (s *Service) challengeHandler(c *httpx.Ctx) *httpx.Response {
	token := c.PathVar("token")
	ka, err := s.Repos.GetChallenge(detached(c), token)
	if err != nil {
		panic(err)
	}
	if ka == nil {
		return httpx.Empty(404)
	}
	return httpx.String(200, *ka)
}

// renewHandler is AcmeAdminController.renewCertificate.
func (s *Service) renewHandler(c *httpx.Ctx) *httpx.Response {
	if err := s.Renewal.RenewIfNeeded(detached(c)); err != nil {
		panic(err)
	}
	return httpx.String(200, renewSuccessMessage)
}

// uploadHandler is AcmeAdminController.uploadCertificate.
func (s *Service) uploadHandler(c *httpx.Ctx) *httpx.Response {
	var body AcmeCertificateUploadJSON
	c.BindBody(&body)
	d := body.ExpiresAt
	expiresAt := time.Date(int(d.Date.Year), time.Month(d.Date.Month), d.Date.Day, d.Hour, d.Minute, d.Second, d.Nano, time.Local)
	if err := s.Upload.UploadCertificate(detached(c), body.CertificatePem, body.PrivateKeyPem, expiresAt); err != nil {
		panic(err)
	}
	return httpx.String(200, uploadSuccessMessage)
}

// zoneInfoHandler is AcmeAdminController.getCloudflareZoneInfo.
func (s *Service) zoneInfoHandler(c *httpx.Ctx) *httpx.Response {
	info, msg, err := s.Cloudflare.GetZoneInfo(detached(c))
	if err != nil {
		panic(err)
	}
	if info == nil {
		return httpx.OK(MessageJSON{Message: msg})
	}
	return httpx.OK(CloudflareZoneInfoJSON{ZoneID: info.ZoneID, Name: info.Name, Status: info.Status, Plan: info.Plan})
}

// directoryHandler is AcmeAdminController.getAcmeServerDirectory.
func (s *Service) directoryHandler(c *httpx.Ctx) *httpx.Response {
	info, msg, err := s.Server.GetDirectoryInfo(detached(c))
	if err != nil {
		panic(err)
	}
	if info == nil {
		return httpx.OK(MessageJSON{Message: msg})
	}
	return httpx.OK(AcmeServerDirectoryJSON{
		ServerURL: info.ServerURL, NewAccountURL: info.NewAccountURL, NewOrderURL: info.NewOrderURL,
		TermsOfServiceURL: info.TermsOfServiceURL,
	})
}
