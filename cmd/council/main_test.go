package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	cases := []struct {
		name       string
		args       []string
		stdin      string
		wantCode   int
		wantOut    string
		wantErrOut string
	}{
		{"version", []string{"version"}, "", 0, "council dev\n", ""},
		{"help", []string{"--help"}, "", 0, "Usage:", ""},
		{"no args", nil, "", 2, "", "council: no command given"},
		{"unknown", []string{"nope"}, "", 2, "", `unknown command "nope"`},
		{"init", []string{"init", "--config", cfg}, "", 0, "wrote " + cfg, ""},
		{"ask without prompt", []string{"ask", "--config", cfg}, "", 2, "", "no prompt given"},
		{"ask unknown agent via stdin", []string{"ask", "--config", cfg, "--agents", "ghost"}, "q\n", 2, "", `unknown agent "ghost"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(c.args, strings.NewReader(c.stdin), &out, &errOut)
			if code != c.wantCode {
				t.Fatalf("exit %d, want %d (stderr %q)", code, c.wantCode, errOut.String())
			}
			if !strings.Contains(out.String(), c.wantOut) {
				t.Errorf("stdout %q does not contain %q", out.String(), c.wantOut)
			}
			if !strings.Contains(errOut.String(), c.wantErrOut) {
				t.Errorf("stderr %q does not contain %q", errOut.String(), c.wantErrOut)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	reg, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	closed, _ := os.Open(reg.Name())
	closed.Close()
	var nilFile *os.File

	cases := []struct {
		name string
		r    io.Reader
		want bool
	}{
		{"in-memory reader", strings.NewReader("x"), false},
		{"nil reader", nil, false},
		{"nil *os.File", nilFile, false},
		{"pipe", pr, false},
		{"regular file", reg, false},
		{"closed file", closed, false},
	}
	if runtime.GOOS != "windows" {
		// /dev/null is a character device, which is what a TTY looks like.
		dn, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer dn.Close()
		cases = append(cases, struct {
			name string
			r    io.Reader
			want bool
		}{"character device", dn, true})
	}
	for _, c := range cases {
		if got := isTerminal(c.r); got != c.want {
			t.Errorf("%s: isTerminal = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMainExitsWithRunStatus covers main itself: it must pass os.Args[1:] to run and exit
// with run's status.
func TestMainExitsWithRunStatus(t *testing.T) {
	oldArgs, oldExit, oldStdout, oldStderr := os.Args, exit, os.Stdout, os.Stderr
	t.Cleanup(func() { os.Args, exit, os.Stdout, os.Stderr = oldArgs, oldExit, oldStdout, oldStderr })

	sink, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	os.Stdout, os.Stderr = sink, sink

	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"council", "version"}, 0},
		{[]string{"council", "bogus"}, 2},
	} {
		got := -1
		exit = func(code int) { got = code }
		os.Args = c.args
		main()
		if got != c.want {
			t.Errorf("main %q exited %d, want %d", c.args[1:], got, c.want)
		}
	}
	b, _ := os.ReadFile(sink.Name())
	if !strings.Contains(string(b), "council dev") || !strings.Contains(string(b), `unknown command "bogus"`) {
		t.Errorf("unexpected output %q", b)
	}
}
