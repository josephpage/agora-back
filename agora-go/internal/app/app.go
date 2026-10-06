// Package app holds the process-wide dependencies shared by every module.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"agora/internal/auth"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/httpx"
	"agora/internal/store"
	"agora/internal/strapi"
)

// App is passed to every module constructor.
type App struct {
	Cfg         *config.Config
	DB          *store.DB
	Cache       *cache.Cache
	Strapi      *strapi.Client
	JWT         *auth.JWT
	LoginTokens *auth.LoginTokens
	IPHasher    *auth.IPHasher
	Log         *slog.Logger
	// Clock is Clock.systemDefaultZone(): always use it instead of time.Now so
	// tests can freeze time.
	Clock func() time.Time
	// Server is nil in custom-command (cron) mode.
	Server *httpx.Server
	// Background runs fire-and-forget work bound to the process lifetime
	// (e.g. the in-memory FCM batch scheduler).
	Background context.Context
}

// Now returns the current time in the process zone.
func (a *App) Now() time.Time { return a.Clock().In(time.Local) }

// singletons holds lazily built module services, keyed by name.
type singletons struct {
	mu   sync.Mutex
	vals map[string]any
}

var registry sync.Map // *App -> *singletons

// Singleton returns the service registered under key, building it once per
// App. Modules expose `func Get(a *app.App) *Service { return app.Singleton(a, "name", func() *Service {...}) }`
// so that cross-module dependencies need no central wiring.
func Singleton[T any](a *App, key string, build func() T) T {
	v, _ := registry.LoadOrStore(a, &singletons{vals: map[string]any{}})
	s := v.(*singletons)
	s.mu.Lock()
	if existing, ok := s.vals[key]; ok {
		s.mu.Unlock()
		return existing.(T)
	}
	s.mu.Unlock()
	built := build() // may recursively call Singleton for dependencies
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.vals[key]; ok {
		return existing.(T)
	}
	s.vals[key] = built
	return built
}
