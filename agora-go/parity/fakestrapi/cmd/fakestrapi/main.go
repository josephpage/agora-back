// Command fakestrapi serves the parity fixtures as a fake Strapi v5 API.
//
//	go run ./parity/fakestrapi/cmd/fakestrapi -addr :1337 -fixtures parity/fixtures/strapi
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agora/parity/fakestrapi"
)

func main() {
	addr := flag.String("addr", ":1337", "listen address")
	fixtures := flag.String("fixtures", "../fixtures/strapi",
		"fixtures directory (absolute, or relative to the cwd, the module root or parity/fakestrapi)")
	nowFlag := flag.String("now", "", "reference time for {{now...}} templates, RFC3339 (default: current UTC time truncated to the second)")
	flag.Parse()

	now := time.Now().UTC().Truncate(time.Second)
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			log.Fatalf("invalid -now %q: %v", *nowFlag, err)
		}
		now = t.UTC()
	}

	dir, err := resolveFixturesDir(*fixtures)
	if err != nil {
		log.Fatal(err)
	}

	srv, err := fakestrapi.New(dir, now)
	if err != nil {
		log.Fatalf("load fixtures: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		srv.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.RequestURI, time.Since(start).Round(time.Millisecond))
	})
	httpSrv := &http.Server{Addr: *addr, Handler: handler}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("fakestrapi listening on %s | fixtures=%s (%d models) | now=%s",
		*addr, dir, len(srv.Models()), now.Format(time.RFC3339))
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// resolveFixturesDir accepts an absolute path, or a relative one resolved
// against the cwd, the Go module root and the parity/fakestrapi package dir.
func resolveFixturesDir(p string) (string, error) {
	if filepath.IsAbs(p) {
		return p, statDir(p)
	}
	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, p))
		if root := moduleRoot(cwd); root != "" {
			candidates = append(candidates,
				filepath.Join(root, p),
				filepath.Join(root, "parity", "fakestrapi", p),
			)
		}
	}
	if exe, err := os.Executable(); err == nil {
		if root := moduleRoot(filepath.Dir(exe)); root != "" {
			candidates = append(candidates, filepath.Join(root, "parity", "fakestrapi", p))
		}
	}
	for _, c := range candidates {
		if statDir(c) == nil {
			return filepath.Clean(c), nil
		}
	}
	return "", fmt.Errorf("fixtures directory %q not found (tried %v)", p, candidates)
}

func statDir(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", p)
	}
	return nil
}

func moduleRoot(from string) string {
	dir := from
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
