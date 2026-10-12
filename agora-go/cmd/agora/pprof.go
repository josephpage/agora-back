package main

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

// startPprof serves the runtime profiles on AGORA_PPROF_ADDR (e.g. 127.0.0.1:6060), on a
// mux of its own: off by default, never on the public port.
func startPprof(log *slog.Logger) {
	addr := os.Getenv("AGORA_PPROF_ADDR")
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Warn("pprof server stopped", "err", err)
		}
	}()
	log.Info("pprof listening", "addr", addr)
}
