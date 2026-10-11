package main

import (
	"os"

	"github.com/Parsa2820/council/internal/cli"
)

func main() {
	st, err := os.Stdin.Stat()
	tty := err == nil && st.Mode()&os.ModeCharDevice != 0
	os.Exit(cli.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, StdinIsTTY: tty}))
}
