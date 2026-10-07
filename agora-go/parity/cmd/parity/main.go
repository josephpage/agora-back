// Command parity runs the differential harness (Kotlin reference vs Go).
//
//	go run ./parity/cmd/parity -scenarios parity/scenarios [-tags S0,S1] [-run regexp] [-report out.json]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"agora/internal/auth"
	"agora/parity/runner"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadEnvFile(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(k, "#") {
			m[k] = v
		}
	}
	return m
}

func main() {
	dir := flag.String("scenarios", "parity/scenarios", "scenario directory")
	tags := flag.String("tags", "", "comma separated tags")
	run := flag.String("run", "", "regexp on scenario names")
	report := flag.String("report", "", "write JSON report to this path")
	verbose := flag.Bool("v", false, "verbose")
	strapiCheck := flag.Bool("strapi", true, "compare outgoing Strapi URIs")
	maxDiffs := flag.Int("max-diffs", 15, "diffs printed per scenario")
	flag.Parse()

	common := loadEnvFile(env("PARITY_COMMON_ENV", "parity/env/common.env"))
	now := time.Now().UTC().Truncate(time.Second)
	if v := os.Getenv("PARITY_NOW"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			now = t
		}
	}
	seedVars, err := runner.LoadSeedVars("parity/seed/ids.go")
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed ids:", err)
		os.Exit(2)
	}
	r := &runner.Runner{
		KotlinLocksFile: os.Getenv("PARITY_KOTLIN_LOCKS"),
		Ref: &runner.Side{
			Name: "ref", BaseURL: env("PARITY_REF_URL", "http://localhost:8081"),
			DBURL:     env("PARITY_REF_DB", "postgres://backend:agora_password@localhost:5432/agora_ref"),
			RedisAddr: env("PARITY_REF_REDIS", "localhost:6380"), RedisPassword: env("PARITY_REF_REDIS_PASS", "refpass"),
			StrapiControl: env("PARITY_REF_STRAPI", "http://localhost:1337"),
			Command:       strings.Fields(env("PARITY_REF_CMD", "/home/user/run/start-ref.sh")),
		},
		Go: &runner.Side{
			Name: "go", BaseURL: env("PARITY_GO_URL", "http://localhost:8082"),
			DBURL:     env("PARITY_GO_DB", "postgres://backend:agora_password@localhost:5432/agora_go"),
			RedisAddr: env("PARITY_GO_REDIS", "localhost:6381"), RedisPassword: env("PARITY_GO_REDIS_PASS", "gopass"),
			StrapiControl: env("PARITY_GO_STRAPI", "http://localhost:1338"),
			Command:       strings.Fields(env("PARITY_GO_CMD", "/home/user/run/start-go.sh")),
			GoCache:       true,
		},
		JWT: auth.NewJWT(common["JWT_SECRET"], nil),
		Tokens: auth.NewLoginTokens(auth.LoginTokenConfig{
			EncodeSecret: common["LOGIN_TOKEN_ENCODE_SECRET"], EncodeTransformation: common["LOGIN_TOKEN_ENCODE_TRANSFORMATION"], EncodeAlgorithm: common["LOGIN_TOKEN_ENCODE_ALGORITHM"],
			DecodeSecret: common["LOGIN_TOKEN_DECODE_SECRET"], DecodeTransformation: common["LOGIN_TOKEN_DECODE_TRANSFORMATION"], DecodeAlgorithm: common["LOGIN_TOKEN_DECODE_ALGORITHM"],
		}),
		SeedVars:    seedVars,
		Now:         now,
		Verbose:     *verbose,
		StrapiCheck: *strapiCheck,
	}
	scenarios, err := runner.LoadScenarios(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var tagList []string
	if *tags != "" {
		tagList = strings.Split(*tags, ",")
	}
	var re *regexp.Regexp
	if *run != "" {
		re = regexp.MustCompile(*run)
	}
	sum := &runner.Summary{}
	ctx := context.Background()
	for _, sc := range scenarios {
		if !sc.HasTag(tagList) || (re != nil && !re.MatchString(sc.Name)) {
			continue
		}
		res := r.Run(ctx, sc)
		sum.Total++
		sum.Results = append(sum.Results, res)
		switch {
		case res.Skipped != "":
			sum.Skipped++
			fmt.Printf("SKIP %s (%s)\n", sc.Name, res.Skipped)
		case res.Failed():
			sum.Failed++
			fmt.Printf("FAIL %s\n", sc.Name)
			printed := 0
			if res.Error != "" {
				fmt.Printf("     error: %s\n", res.Error)
			}
			for _, st := range res.Steps {
				for _, d := range st.Diffs {
					if printed < *maxDiffs {
						fmt.Printf("     [%s %s %s] %s\n        ref: %s\n        go:  %s %s\n", st.ID, st.Method, st.Path, d.Where, d.Ref, d.Go, d.Note)
					}
					printed++
				}
			}
			for _, d := range res.Diffs {
				if printed < *maxDiffs {
					fmt.Printf("     %s\n        ref: %s\n        go:  %s %s\n", d.Where, d.Ref, d.Go, d.Note)
				}
				printed++
			}
			if printed > *maxDiffs {
				fmt.Printf("     … %d more\n", printed-*maxDiffs)
			}
		default:
			sum.Passed++
			fmt.Printf("ok   %s\n", sc.Name)
		}
	}
	fmt.Printf("\n%d scenarios: %d passed, %d failed, %d skipped\n", sum.Total, sum.Passed, sum.Failed, sum.Skipped)
	if *report != "" {
		_ = sum.WriteJSON(*report)
	}
	if sum.Failed > 0 {
		os.Exit(1)
	}
}
