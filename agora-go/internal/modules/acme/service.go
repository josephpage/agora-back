package acme

import (
	"context"
	"log/slog"

	"agora/internal/app"
)

// Service gathers the ACME beans (AcmeConfig, the repositories, the use cases).
type Service struct {
	Cfg     *Config
	Crypto  *CryptoHelper
	Repos   *pgRepos
	Renewal *RenewalUseCase
	Upload  *UploadUseCase

	Cloudflare *CloudflareHealthCheckUseCase
	Server     *ServerHealthCheckUseCase
	Deployer   *CloudflareDeployer

	a    *app.App
	stub *stubState
}

type built struct {
	svc *Service
	err error
}

// Get returns the App-wide ACME service. It fails when the ACME_* properties
// are invalid (the Kotlin application does not start in that case).
func Get(a *app.App) (*Service, error) {
	b := app.Singleton(a, "acme", func() built {
		svc, err := newService(a)
		return built{svc, err}
	})
	return b.svc, b.err
}

func newService(a *app.App) (*Service, error) {
	cfg, err := LoadConfig(a.Cfg)
	if err != nil {
		return nil, err
	}
	return NewService(a, cfg, nil), nil
}

// NewService wires the beans. A nil ca selects the golang.org/x/crypto/acme client.
func NewService(a *app.App, cfg *Config, ca CAProvider) *Service {
	log := a.Log
	if log == nil {
		log = slog.Default()
	}
	if ca == nil {
		ca = &LibProvider{}
	}
	crypto := &CryptoHelper{cfg: cfg}
	repos := &pgRepos{db: a.DB.Pool, crypto: crypto, now: a.Now}
	rest := NewRestClient()
	deployer := &CloudflareDeployer{Cfg: cfg, Rest: rest}
	return &Service{
		Cfg:    cfg,
		Crypto: crypto,
		Repos:  repos,
		Renewal: &RenewalUseCase{
			Cfg: cfg, Certificates: repos, Accounts: repos, Challenges: repos, Orders: repos,
			Deployer: deployer, CA: ca, Now: a.Now, Log: log,
		},
		Upload:     &UploadUseCase{Cfg: cfg, Certificates: repos, Log: log},
		Cloudflare: &CloudflareHealthCheckUseCase{Cfg: cfg, Checker: &ZoneChecker{Cfg: cfg, Rest: rest}, Log: log},
		Server:     &ServerHealthCheckUseCase{Cfg: cfg, Checker: &ServerChecker{Cfg: cfg, Rest: rest}, Log: log},
		Deployer:   deployer,
		a:          a,
		stub: &stubState{
			orders: map[string]*stubOrder{}, authzs: map[string]*stubAuthz{},
			challengeToOrder: map[string]string{}, certs: map[string]string{},
		},
	}
}

// RunRenewalTask is AcmeCertificateRenewalTasksHandler.handleTask: nothing
// happens unless ACME_CRON_ENABLED is true.
func (s *Service) RunRenewalTask(ctx context.Context) error {
	if !s.Cfg.CronEnabled {
		s.a.Log.Info("ACME cron renewal is disabled (ACME_CRON_ENABLED=false). Skipping.")
		return nil
	}
	return s.Renewal.RenewIfNeeded(ctx)
}
