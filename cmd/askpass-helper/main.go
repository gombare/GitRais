// askpass-helper is run by git/ssh as GIT_ASKPASS/SSH_ASKPASS and forwards the
// prompt to the Gitrais process over a Unix socket. On cancel or error it prints
// nothing on stdout and exits non-zero so git aborts cleanly.
package main

import (
	"fmt"
	"os"
	"strings"

	"gitrais/internal/askpass"
)

func main() {
	prompt := strings.Join(os.Args[1:], " ")
	ans, err := askpass.Ask(os.Getenv(askpass.EnvSock), os.Getenv(askpass.EnvToken), prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitrais askpass:", err)
		os.Exit(1)
	}
	fmt.Println(ans)
}
