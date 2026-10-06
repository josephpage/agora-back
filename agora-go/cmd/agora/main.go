// Command agora is the Go rewrite of the agora-back Kotlin backend.
//
//	agora                                   → HTTP server on AGORA_PORT (else PORT)
//	agora --run-custom-command=dailyTasks   → run a cron task and exit
//	agora --run-custom-command=weeklyTasks --force_question_selection=true
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/auth"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/httpx"
	"agora/internal/modules/all"
	"agora/internal/modules/users"
	"agora/internal/store"
	"agora/internal/strapi"
	"agora/internal/tasks"
)

const customCommandPrefix = "--run-custom-command="

// parseCustomCommand reproduces AgoraCustomCommandHelper: the command name
// and the following "--key=value" arguments.
func parseCustomCommand(args []string) (string, map[string]string, bool) {
	for i, a := range args {
		if !strings.HasPrefix(a, customCommandPrefix) {
			continue
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(a, customCommandPrefix))
		if cmd == "" {
			return "", nil, false
		}
		params := map[string]string{}
		for _, arg := range args[i+1:] {
			if strings.TrimSpace(arg) == "" {
				continue
			}
			if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
				k := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)[0]
				v := arg[strings.Index(arg, "=")+1:]
				params[k] = v
			}
		}
		return cmd, params, true
	}
	return "", nil, false
}

func main() {
	logger := newLogger()
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, cleanup, err := build(ctx, cfg, logger)
	if err != nil {
		logger.Error("startup error", "err", err)
		os.Exit(1)
	}
	defer cleanup()

	if cmd, args, ok := parseCustomCommand(os.Args[1:]); ok {
		os.Exit(runCustomCommand(ctx, a, cmd, args))
	}
	if err := serve(ctx, a); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG", "TRACE":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func build(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*app.App, func(), error) {
	db, err := store.Open(ctx, cfg.DatabaseURL, cfg.DatabaseMaxPoolSize, cfg.DBAcquireTimeout)
	if err != nil {
		return nil, nil, err
	}
	if cfg.BootstrapSchema {
		if err := db.BootstrapSchema(ctx); err != nil {
			return nil, nil, fmt.Errorf("bootstrap schema: %w", err)
		}
	}
	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddr,
			Username:     cfg.RedisUser,
			Password:     cfg.RedisPassword,
			PoolSize:     cfg.RedisPoolSize,
			DialTimeout:  2 * time.Second,
			ReadTimeout:  2 * time.Second,
			WriteTimeout: 2 * time.Second,
			PoolTimeout:  2 * time.Second,
		})
	}
	c := cache.New(rdb, logger, cfg.Coexistence)
	a := &app.App{
		Cfg:   cfg,
		DB:    db,
		Cache: c,
		Strapi: strapi.NewClient(strapi.Options{
			BaseURL: cfg.CMSAPIURL, Token: cfg.CMSAuthToken, Suspended: cfg.StrapiSuspended, Logger: logger,
		}),
		JWT: auth.NewJWT(cfg.JWTSecret, nil),
		LoginTokens: auth.NewLoginTokens(auth.LoginTokenConfig{
			EncodeSecret: cfg.LoginToken.EncodeSecret, EncodeTransformation: cfg.LoginToken.EncodeTransformation, EncodeAlgorithm: cfg.LoginToken.EncodeAlgorithm,
			DecodeSecret: cfg.LoginToken.DecodeSecret, DecodeTransformation: cfg.LoginToken.DecodeTransformation, DecodeAlgorithm: cfg.LoginToken.DecodeAlgorithm,
		}),
		IPHasher: auth.NewIPHasher(cfg.RemoteAddressHash.Algorithm, cfg.RemoteAddressHash.Iterations, cfg.RemoteAddressHash.KeyLength, cfg.RemoteAddressHash.Salt),
		Log:      logger,
		Clock:    time.Now,
		Background: ctx,
	}
	cleanup := func() {
		db.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
	}
	return a, cleanup, nil
}

func runCustomCommand(ctx context.Context, a *app.App, cmd string, args map[string]string) int {
	a.Log.Info(fmt.Sprintf("⚙️ Run custom command = %s / argument = %v", cmd, args))
	if a.Cfg.CronDisabled {
		a.Log.Info("AGORA_CRON_DISABLED=true: custom command skipped (parallel app)")
		return 0
	}
	if h, ok := tasks.Handlers[cmd]; ok {
		if err := h(ctx, a, args); err != nil {
			a.Log.Error("custom command failed", "cmd", cmd, "err", err)
			return 1
		}
	}
	a.Log.Info("⚙️ Run custom command finished")
	return 0
}

func serve(ctx context.Context, a *app.App) error {
	a.Cache.Run(ctx)
	u := users.Get(a)
	a.Server = httpx.NewServer(httpx.Options{
		AllowedOrigins: a.Cfg.AllowedOrigins,
		JWT:            a.JWT,
		IPHasher:       a.IPHasher,
		Users:          u.Lookup,
		ETagPaths:      []string{"/thematiques", "/participation_charter"},
		Now:            a.Clock,
		Logger:         a.Log,
		OnPanic: func(r *http.Request, v any, stack []byte) {
			reportPanic(r, v, stack)
		},
	})
	all.Routes(a)

	srv := &http.Server{
		Addr:              net.JoinHostPort("", a.Cfg.Port),
		Handler:           a.Server,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errCh := make(chan error, 1)
	go func() {
		a.Log.Info("Started agora (Go) on port " + a.Cfg.Port)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func reportPanic(r *http.Request, v any, stack []byte) {
	_ = r
	_ = v
	if len(stack) == 0 {
		stack = debug.Stack()
	}
}
