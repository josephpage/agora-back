package main

import (
	"fmt"
	"os"
	"strings"

	"agora/internal/auth"
)

func main() {
	b, _ := os.ReadFile("parity/env/common.env")
	secret := ""
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && k == "JWT_SECRET" {
			secret = v
		}
	}
	tok, _, err := auth.NewJWT(secret, nil).Generate(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Print(tok)
}
