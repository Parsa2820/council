// Package cli implements the council command-line interface: argument and
// flag parsing for the ask, init, doctor (alias status), login, agents and
// version subcommands, the live --verbose printer, and the final result
// output.
//
// [Main] is the entry point. It takes the arguments without the program name
// and an [Env] holding the process streams, so the whole CLI can be driven
// in-process by tests. It returns the exit status: 0 on success (and for
// -h/--help), 2 for usage errors, and 1 for every other failure, including
// "council doctor" finding a harness that is missing or signed out.
//
// Flags may appear before or after positional arguments (see
// parseInterspersed); everything after a literal "--" is positional. For
// "council login", arguments after "--" are passed through to the harness
// CLI's own login command.
//
// [Version] is stamped at build time with
//
//	-ldflags "-X github.com/Parsa2820/council/internal/cli.Version=v0.1.0"
package cli
