// Command hashpw prints a bcrypt hash for an admin password.
//
//	echo -n 'the-password' | go run ./cmd/hashpw
//
// The password is read from stdin so it never lands in shell history or ps output.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	in := bufio.NewReader(os.Stdin)
	pw, err := in.ReadString('\n')
	if err != nil && pw == "" {
		fmt.Fprintln(os.Stderr, "read password from stdin")
		os.Exit(1)
	}
	pw = strings.TrimRight(pw, "\r\n")
	if len(pw) < 8 {
		fmt.Fprintln(os.Stderr, "password must be at least 8 bytes")
		os.Exit(1)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(h))
}
