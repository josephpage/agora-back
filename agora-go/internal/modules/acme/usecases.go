package acme

import (
	"context"
	"log/slog"
	"time"
)

// UploadUseCase is usecase/acme/AcmeCertificateUploadUseCase.kt.
type UploadUseCase struct {
	Cfg          *Config
	Certificates CertificateRepository
	Log          *slog.Logger
}

// UploadCertificate stores the certificate of the configured domain with the
// TO_DEPLOY status.
func (u *UploadUseCase) UploadCertificate(ctx context.Context, certificatePEM, privateKeyPEM string, expiresAt time.Time) error {
	domain := u.Cfg.Domain
	u.Log.Info("Uploading manually provided certificate for domain " + domain + ", expires at " + expiresAt.Format("2006-01-02T15:04:05"))
	err := u.Certificates.SaveCertificate(ctx, Certificate{
		Domain: domain, CertificatePEM: certificatePEM, PrivateKeyPEM: privateKeyPEM,
		ExpiresAt: expiresAt, Status: StatusToDeploy,
	})
	if err != nil {
		return err
	}
	u.Log.Info("Certificate for domain " + domain + " saved with status TO_DEPLOY. It will be deployed to Cloudflare on the next renewIfNeeded() call.")
	return nil
}

// ZoneInfoChecker is usecase/acme/repository/CloudflareZoneChecker.
type ZoneInfoChecker interface {
	GetZoneInfo(ctx context.Context) (*CloudflareZoneInfo, error)
}

// DirectoryInfoChecker is usecase/acme/repository/AcmeServerChecker.
type DirectoryInfoChecker interface {
	GetDirectoryInfo(ctx context.Context) (*ServerDirectoryInfo, error)
}

// CloudflareHealthCheckUseCase is usecase/acme/CloudflareHealthCheckUseCase.kt.
type CloudflareHealthCheckUseCase struct {
	Cfg     *Config
	Checker ZoneInfoChecker
	Log     *slog.Logger
}

// GetZoneInfo returns the zone info, or the "disabled" message (info == nil).
func (u *CloudflareHealthCheckUseCase) GetZoneInfo(ctx context.Context) (info *CloudflareZoneInfo, disabledMessage string, err error) {
	if !u.Cfg.CloudflareInteractionEnabled {
		msg := "Cloudflare interaction is disabled (ACME_CLOUDFLARE_INTERACTION_ENABLED=false)"
		u.Log.Info(msg)
		return nil, msg, nil
	}
	u.Log.Info("Fetching Cloudflare zone info for zoneId=" + u.Cfg.CloudflareZoneID)
	info, err = u.Checker.GetZoneInfo(ctx)
	if err != nil {
		return nil, "", err
	}
	u.Log.Info("Cloudflare zone info retrieved: name=" + info.Name + ", status=" + info.Status + ", plan=" + info.Plan)
	return info, "", nil
}

// ServerHealthCheckUseCase is usecase/acme/AcmeServerHealthCheckUseCase.kt.
type ServerHealthCheckUseCase struct {
	Cfg     *Config
	Checker DirectoryInfoChecker
	Log     *slog.Logger
}

// GetDirectoryInfo returns the directory info, or the "disabled" message (info == nil).
func (u *ServerHealthCheckUseCase) GetDirectoryInfo(ctx context.Context) (info *ServerDirectoryInfo, disabledMessage string, err error) {
	if !u.Cfg.Enabled {
		msg := "ACME is disabled (ACME_ENABLED=false)"
		u.Log.Info(msg)
		return nil, msg, nil
	}
	if !u.Cfg.ServerInteractionEnabled {
		msg := "ACME server interaction is disabled (ACME_SERVER_INTERACTION_ENABLED=false)"
		u.Log.Info(msg)
		return nil, msg, nil
	}
	u.Log.Info("Fetching ACME server directory info from " + u.Cfg.ServerURL)
	info, err = u.Checker.GetDirectoryInfo(ctx)
	if err != nil {
		return nil, "", err
	}
	u.Log.Info("ACME server directory retrieved: newAccountUrl=" + info.NewAccountURL + ", newOrderUrl=" + info.NewOrderURL)
	return info, "", nil
}
