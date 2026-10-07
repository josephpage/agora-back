package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"agora/parity/seed"
)

// reset reseeds the database, flushes Redis and resets the fake Strapi.
func (s *Side) reset(ctx context.Context, setup Setup, now time.Time) error {
	if setup.Reseed == nil || *setup.Reseed {
		conn, err := pgx.Connect(ctx, s.DBURL)
		if err != nil {
			return fmt.Errorf("%s db: %w", s.Name, err)
		}
		defer conn.Close(ctx)
		if err := seed.Seed(ctx, conn, now, 1); err != nil {
			return fmt.Errorf("%s seed: %w", s.Name, err)
		}
		for _, q := range setup.SQL {
			if _, err := conn.Exec(ctx, q); err != nil {
				return fmt.Errorf("%s setup sql %q: %w", s.Name, q, err)
			}
		}
	}
	rdb := redis.NewClient(&redis.Options{Addr: s.RedisAddr, Password: s.RedisPassword})
	defer rdb.Close()
	// keep the Go epoch sentinel: otherwise the instance's epoch watcher would
	// notice the FLUSHALL up to 2 s later and clear its L1 in the middle of the
	// scenario (the explicit flush below already cleared it)
	epoch, _ := rdb.Get(ctx, "agora:go:epoch").Result()
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		return fmt.Errorf("%s redis: %w", s.Name, err)
	}
	if epoch != "" {
		if err := rdb.Set(ctx, "agora:go:epoch", epoch, 0).Err(); err != nil {
			return fmt.Errorf("%s redis: %w", s.Name, err)
		}
	}
	if s.GoCache {
		// flush every Go L1 and wait for the instance's acknowledgement: the
		// pub/sub delivery is asynchronous, and without it the first request of
		// the scenario could still be served from the previous scenario's L1
		nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := rdb.Publish(ctx, "agora:go:inval", "*\x00ack:"+nonce).Err(); err != nil {
			return fmt.Errorf("%s L1 flush: %w", s.Name, err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			if n, _ := rdb.Exists(ctx, "agora:go:inval:ack:"+nonce).Result(); n == 1 {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s: no L1 flush acknowledgement from the Go instance", s.Name)
			}
			time.Sleep(2 * time.Millisecond)
		}
		_ = rdb.Del(ctx, "agora:go:inval:ack:"+nonce).Err()
	}
	for k, v := range setup.Redis {
		if err := rdb.Set(ctx, k, v, 0).Err(); err != nil {
			return err
		}
	}
	if s.StrapiControl != "" {
		if err := postJSON(ctx, s.StrapiControl+"/__control/reset", map[string]any{}); err != nil {
			return fmt.Errorf("%s strapi reset: %w", s.Name, err)
		}
		for _, f := range setup.StrapiFaults {
			if err := postJSON(ctx, s.StrapiControl+"/__control/fault", f); err != nil {
				return err
			}
		}
		for _, o := range setup.StrapiOverrides {
			if err := postJSON(ctx, s.StrapiControl+"/__control/override", o); err != nil {
				return err
			}
		}
	}
	return nil
}

func postJSON(ctx context.Context, url string, body any) error {
	b, _ := json.Marshal(normalizeYAML(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return nil
}

// strapiRequests returns the request URIs recorded by the fake Strapi.
func (s *Side) strapiRequests(ctx context.Context) ([]string, error) {
	if s.StrapiControl == "" {
		return nil, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.StrapiControl+"/__control/requests", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var entries []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		for _, k := range []string{"uri", "rawURI", "requestURI", "path"} {
			if v, ok := e[k].(string); ok {
				out = append(out, v)
				break
			}
		}
	}
	return out, nil
}
