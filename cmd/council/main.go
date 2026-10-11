package main

import (
	"io"
	"os"

	"github.com/Parsa2820/council/internal/cli"
)

// exit is os.Exit, replaced in tests.
var exit = os.Exit

func main() {
	exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the CLI with the given arguments and streams and returns the
// process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return cli.Main(args, cli.Env{Stdin: stdin, Stdout: stdout, Stderr: stderr, StdinIsTTY: isTerminal(stdin)})
}

// isTerminal reports whether r is a character device such as a terminal.
// Pipes, regular files and in-memory readers are not.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok || f == nil {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
