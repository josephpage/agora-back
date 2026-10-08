// Command mint prints a JWT for a user id, signed with the parity JWT_SECRET
// (parity/env/common.env), to call the reference / Go servers by hand:
//
//	curl -H "Authorization: Bearer $(go run ./parity/cmd/mint 00000000-0000-4000-9000-000000000001)" ...
package main

import (
	"fmt"
	"os"
	"strings"

	"agora/internal/auth"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mint <user id>")
		os.Exit(2)
	}
	b, err := os.ReadFile("parity/env/common.env")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	secret := ""
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && k == "JWT_SECRET" {
			secret = v
		}
	}
	tok, _, err := auth.NewJWT(secret, nil).Generate(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(tok)
}
