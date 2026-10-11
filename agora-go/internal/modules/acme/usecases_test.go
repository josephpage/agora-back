package acme

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- AcmeCertificateUploadUseCaseTest ---------------------------------------------------

const (
	uploadCertPEM = "-----BEGIN CERTIFICATE-----\nMIIFAzCC...\n-----END CERTIFICATE-----"
	uploadKeyPEM  = "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----"
)

func TestUploadCertificateSavesWithToDeployStatus(t *testing.T) {
	repos := newFakeRepos()
	uc := &UploadUseCase{Cfg: &Config{Domain: "agora.gouv.fr"}, Certificates: repos, Log: discardLog()}
	expires := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mustNoErr(t, uc.UploadCertificate(context.Background(), uploadCertPEM, uploadKeyPEM, expires))
	want := Certificate{Domain: "agora.gouv.fr", CertificatePEM: uploadCertPEM, PrivateKeyPEM: uploadKeyPEM, ExpiresAt: expires, Status: StatusToDeploy}
	if len(repos.certs) != 1 || !reflect.DeepEqual(repos.certs[0], want) {
		t.Fatalf("saved %+v, want %+v", repos.certs, want)
	}
	if repos.ops() != "saveCertificate:TO_DEPLOY" {
		t.Errorf("ops %s", repos.ops())
	}
}

func TestUploadCertificateUsesTheConfiguredDomain(t *testing.T) {
	repos := newFakeRepos()
	uc := &UploadUseCase{Cfg: &Config{Domain: "custom.example.com"}, Certificates: repos, Log: discardLog()}
	mustNoErr(t, uc.UploadCertificate(context.Background(), uploadCertPEM, uploadKeyPEM, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
	if repos.certs[0].Domain != "custom.example.com" {
		t.Errorf("domain %q", repos.certs[0].Domain)
	}
}

func TestUploadCertificatePropagatesTheRepositoryFailure(t *testing.T) {
	repos := newFakeRepos()
	boom := errors.New("db down")
	repos.failures["saveCertificate"] = boom
	uc := &UploadUseCase{Cfg: &Config{Domain: "agora.gouv.fr"}, Certificates: repos, Log: discardLog()}
	if err := uc.UploadCertificate(context.Background(), "c", "k", time.Now()); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
}

// PostgreSQL rounds a timestamp(6) parameter that has nanoseconds half up.
func TestRoundMicros(t *testing.T) {
	base := time.Date(2030, 6, 1, 10, 20, 30, 0, time.Local)
	for _, c := range []struct{ nanos, want int }{
		{0, 0}, {499, 0}, {500, 1000}, {999, 1000}, {1000, 1000}, {1499, 1000}, {1500, 2000},
		{123456499, 123456000}, {123456500, 123457000}, {999999499, 999999000}, {999999500, 1000000000},
	} {
		got := roundMicros(base.Add(time.Duration(c.nanos)))
		if want := base.Add(time.Duration(c.want)); !got.Equal(want) {
			t.Errorf("%d ns: %v, want %v", c.nanos, got, want)
		}
	}
}

// --- CloudflareHealthCheckUseCaseTest ----------------------------------------------------

type fakeZoneChecker struct {
	info  *CloudflareZoneInfo
	err   error
	calls int
}

func (f *fakeZoneChecker) GetZoneInfo(context.Context) (*CloudflareZoneInfo, error) {
	f.calls++
	return f.info, f.err
}

func TestZoneInfoDisabledDoesNotCallTheChecker(t *testing.T) {
	checker := &fakeZoneChecker{}
	uc := &CloudflareHealthCheckUseCase{Cfg: &Config{CloudflareInteractionEnabled: false}, Checker: checker, Log: discardLog()}
	info, msg, err := uc.GetZoneInfo(context.Background())
	mustNoErr(t, err)
	if info != nil || !strings.Contains(msg, "ACME_CLOUDFLARE_INTERACTION_ENABLED=false") || checker.calls != 0 {
		t.Errorf("info=%v msg=%q calls=%d", info, msg, checker.calls)
	}
	if msg != "Cloudflare interaction is disabled (ACME_CLOUDFLARE_INTERACTION_ENABLED=false)" {
		t.Errorf("message %q", msg)
	}
}

func TestZoneInfoReturnsTheZone(t *testing.T) {
	want := &CloudflareZoneInfo{ZoneID: "zone-abc123", Name: "example.gouv.fr", Status: "active", Plan: "Enterprise"}
	checker := &fakeZoneChecker{info: want}
	uc := &CloudflareHealthCheckUseCase{Cfg: &Config{CloudflareInteractionEnabled: true, CloudflareZoneID: "zone-abc123"}, Checker: checker, Log: discardLog()}
	info, msg, err := uc.GetZoneInfo(context.Background())
	mustNoErr(t, err)
	if msg != "" || info != want || *info != *want {
		t.Errorf("info=%+v msg=%q", info, msg)
	}
}

func TestZoneInfoPropagatesTheCheckerFailure(t *testing.T) {
	boom := errors.New("Cloudflare API error")
	uc := &CloudflareHealthCheckUseCase{Cfg: &Config{CloudflareInteractionEnabled: true}, Checker: &fakeZoneChecker{err: boom}, Log: discardLog()}
	if _, _, err := uc.GetZoneInfo(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
}

// --- AcmeServerHealthCheckUseCaseTest ----------------------------------------------------

type fakeDirChecker struct {
	info  *ServerDirectoryInfo
	err   error
	calls int
}

func (f *fakeDirChecker) GetDirectoryInfo(context.Context) (*ServerDirectoryInfo, error) {
	f.calls++
	return f.info, f.err
}

func TestDirectoryInfoDisabledWhenAcmeIsDisabled(t *testing.T) {
	checker := &fakeDirChecker{}
	uc := &ServerHealthCheckUseCase{Cfg: &Config{Enabled: false}, Checker: checker, Log: discardLog()}
	info, msg, err := uc.GetDirectoryInfo(context.Background())
	mustNoErr(t, err)
	if info != nil || msg != "ACME is disabled (ACME_ENABLED=false)" || checker.calls != 0 {
		t.Errorf("info=%v msg=%q calls=%d", info, msg, checker.calls)
	}
}

func TestDirectoryInfoDisabledWhenServerInteractionIsDisabled(t *testing.T) {
	checker := &fakeDirChecker{}
	uc := &ServerHealthCheckUseCase{Cfg: &Config{Enabled: true, ServerInteractionEnabled: false}, Checker: checker, Log: discardLog()}
	info, msg, err := uc.GetDirectoryInfo(context.Background())
	mustNoErr(t, err)
	if info != nil || msg != "ACME server interaction is disabled (ACME_SERVER_INTERACTION_ENABLED=false)" || checker.calls != 0 {
		t.Errorf("info=%v msg=%q calls=%d", info, msg, checker.calls)
	}
}

func TestDirectoryInfoReturnsTheDirectory(t *testing.T) {
	tos := "https://zerossl.com/terms"
	want := &ServerDirectoryInfo{ServerURL: "https://acme.zerossl.com/v2/DV90", NewAccountURL: "https://acme.zerossl.com/v2/DV90/newAccount",
		NewOrderURL: "https://acme.zerossl.com/v2/DV90/newOrder", TermsOfServiceURL: &tos}
	uc := &ServerHealthCheckUseCase{Cfg: &Config{Enabled: true, ServerInteractionEnabled: true, ServerURL: want.ServerURL}, Checker: &fakeDirChecker{info: want}, Log: discardLog()}
	info, msg, err := uc.GetDirectoryInfo(context.Background())
	mustNoErr(t, err)
	if msg != "" || info != want {
		t.Errorf("info=%+v msg=%q", info, msg)
	}
}

func TestDirectoryInfoWithoutTermsOfService(t *testing.T) {
	want := &ServerDirectoryInfo{ServerURL: "https://acme.example.com", NewAccountURL: "https://acme.example.com/newAccount", NewOrderURL: "https://acme.example.com/newOrder"}
	uc := &ServerHealthCheckUseCase{Cfg: &Config{Enabled: true, ServerInteractionEnabled: true}, Checker: &fakeDirChecker{info: want}, Log: discardLog()}
	info, _, err := uc.GetDirectoryInfo(context.Background())
	mustNoErr(t, err)
	if info.TermsOfServiceURL != nil {
		t.Errorf("termsOfServiceUrl %v", info.TermsOfServiceURL)
	}
}

func TestDirectoryInfoPropagatesTheCheckerFailure(t *testing.T) {
	boom := errors.New("ACME server unreachable")
	uc := &ServerHealthCheckUseCase{Cfg: &Config{Enabled: true, ServerInteractionEnabled: true}, Checker: &fakeDirChecker{err: boom}, Log: discardLog()}
	if _, _, err := uc.GetDirectoryInfo(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
}

// --- AcmeCertificateRenewalTasksHandlerTest ----------------------------------------------

func taskService(cron bool, repos *fakeRepos) *Service {
	cfg := &Config{Enabled: true, CronEnabled: cron, Domain: "agora.gouv.fr"}
	return &Service{
		Cfg: cfg,
		a:   &app.App{Log: discardLog()},
		Renewal: &RenewalUseCase{
			Cfg: cfg, Certificates: repos, Accounts: repos, Challenges: repos, Orders: repos, Deployer: repos,
			Now: func() time.Time { return testNow }, Log: discardLog(),
		},
	}
}

func TestRenewalTaskDoesNothingWhenTheCronIsDisabled(t *testing.T) {
	repos := newFakeRepos()
	mustNoErr(t, taskService(false, repos).RunRenewalTask(context.Background()))
	if repos.ops() != "" {
		t.Errorf("the use case ran: %s", repos.ops())
	}
}

func TestRenewalTaskRunsTheRenewalWhenTheCronIsEnabled(t *testing.T) {
	repos := newFakeRepos()
	mustNoErr(t, taskService(true, repos).RunRenewalTask(context.Background()))
	if !repos.has("loadCertificate") {
		t.Errorf("the use case did not run: %q", repos.ops())
	}
}

func TestRenewalTaskPropagatesTheFailure(t *testing.T) {
	repos := newFakeRepos()
	boom := errors.New("db down")
	repos.failures["loadCertificate"] = boom
	if err := taskService(true, repos).RunRenewalTask(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
}
