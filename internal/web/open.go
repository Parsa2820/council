package web

import (
	"os/exec"
	"runtime"
)

// OpenCommand returns the argv that opens url in the default browser on goos.
func OpenCommand(goos, url string) []string {
	switch goos {
	case "darwin":
		return []string{"open", url}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default: // linux, *bsd
		return []string{"xdg-open", url}
	}
}

// OpenBrowser opens url in the default browser without waiting for it.
func OpenBrowser(url string) error {
	argv := OpenCommand(runtime.GOOS, url)
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
